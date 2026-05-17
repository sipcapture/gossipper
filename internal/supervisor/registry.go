package supervisor

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// Spec is the worker contract serialised to disk and consumed by
// `gossipper worker --spec=<file>`. The master writes Spec into the per-job
// temp directory and exec's the gossipper binary with `--spec=<path>`.
type Spec struct {
	// JobID associates spec with a Job row.
	JobID string `json:"job_id"`
	// DataDir is the absolute path to the master's uistore data directory.
	// Workers open it as a uistore.Store to load profile and scenario data.
	DataDir string `json:"data_dir,omitempty"`
	// ProfileID is the originating server / client profile id.
	ProfileID string `json:"profile_id,omitempty"`
	// ProfileKind is one of "server" / "client".
	ProfileKind string `json:"profile_kind,omitempty"`
	// ScenarioID overrides the profile's default scenario when set.
	ScenarioID string `json:"scenario_id,omitempty"`
	// ArtifactsDir is the per-job directory under data-dir/artifacts/jobs/.
	ArtifactsDir string `json:"artifacts_dir,omitempty"`
	// StatsIntervalMs controls how often the worker emits a JSON-lines stats
	// snapshot to stdout (0 → default 1000ms).
	StatsIntervalMs int `json:"stats_interval_ms,omitempty"`
	// RecordWAV enables automatic per-call WAV capture into
	// ArtifactsDir/recordings (the directory the supervisor later scans for
	// job_artifacts of kind "recording").
	RecordWAV bool `json:"record_wav,omitempty"`
	// RecordWAVDuplex controls stereo (L=sent, R=recv) capture; ignored unless
	// RecordWAV is true.
	RecordWAVDuplex bool `json:"record_wav_duplex,omitempty"`
	// Engine carries opaque CLI overrides applied after profile-derived defaults.
	Engine map[string]any `json:"engine,omitempty"`
}

// Runner is the abstraction the API uses to start/stop workers; Phase 0 ships
// the StubRunner, Phase 2 introduces a real fork/exec runner.
type Runner interface {
	Start(ctx context.Context, spec Spec) (pid int, err error)
	Stop(ctx context.Context, jobID string) error
}

// StubRunner records calls but does not spawn any process. Useful for tests
// and bootstrap until the real runner lands.
type StubRunner struct {
	mu      sync.Mutex
	started map[string]Spec
	stopped map[string]struct{}
	nextPID int
	// AlwaysError, when non-nil, is returned from Start.
	AlwaysError error
}

// NewStubRunner creates an empty StubRunner.
func NewStubRunner() *StubRunner {
	return &StubRunner{
		started: map[string]Spec{},
		stopped: map[string]struct{}{},
		nextPID: 100000,
	}
}

// Start records the spec and returns a fake PID.
func (r *StubRunner) Start(_ context.Context, spec Spec) (int, error) {
	if r.AlwaysError != nil {
		return 0, r.AlwaysError
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if spec.JobID == "" {
		return 0, errors.New("supervisor: spec.JobID is required")
	}
	if _, dup := r.started[spec.JobID]; dup {
		return 0, fmt.Errorf("supervisor: job %q already started", spec.JobID)
	}
	r.started[spec.JobID] = spec
	r.nextPID++
	return r.nextPID, nil
}

// Stop records the stop signal for a job (no actual process to kill).
func (r *StubRunner) Stop(_ context.Context, jobID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.started[jobID]; !ok {
		return ErrNotFound
	}
	r.stopped[jobID] = struct{}{}
	return nil
}

// Started returns the list of started job ids (test helper).
func (r *StubRunner) Started() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.started))
	for id := range r.started {
		out = append(out, id)
	}
	return out
}

// Registry composes a JobsStore and a Runner so the HTTP layer has a single
// place to issue start/stop/delete operations.
type Registry struct {
	Store  *JobsStore
	Runner Runner
}

// NewRegistry constructs a Registry. Either dependency may be nil for tests
// (the call returns an error in that case).
func NewRegistry(store *JobsStore, runner Runner) *Registry {
	return &Registry{Store: store, Runner: runner}
}

// StartJob persists a Job row then asks the Runner to start the worker.
// Returns the persisted Job (with status=running or failed).
func (r *Registry) StartJob(ctx context.Context, j Job, spec Spec) (Job, error) {
	if r == nil || r.Store == nil || r.Runner == nil {
		return Job{}, errors.New("supervisor: registry is not configured")
	}
	if spec.JobID == "" {
		spec.JobID = j.ID
	}
	created, err := r.Store.Create(ctx, j)
	if err != nil {
		return Job{}, err
	}
	pid, err := r.Runner.Start(ctx, spec)
	if err != nil {
		_ = r.Store.UpdateStatus(ctx, created.ID, StatusFailed, nil, err.Error())
		return r.Store.Get(ctx, created.ID)
	}
	if err := r.Store.SetPID(ctx, created.ID, pid); err != nil {
		return Job{}, err
	}
	if err := r.Store.UpdateStatus(ctx, created.ID, StatusRunning, nil, ""); err != nil {
		return Job{}, err
	}
	return r.Store.Get(ctx, created.ID)
}

// StopJob asks the Runner to stop a job. ExecRunner overwrites the status to
// StatusStopped from within waitWorker (so we get the real exit code); for
// StubRunner we still need to update the row here, because no exit notification
// is ever delivered.
func (r *Registry) StopJob(ctx context.Context, id string) (Job, error) {
	stopErr := r.Runner.Stop(ctx, id)
	if stopErr != nil && !errors.Is(stopErr, ErrNotFound) {
		return Job{}, stopErr
	}
	current, getErr := r.Store.Get(ctx, id)
	if getErr != nil {
		return Job{}, getErr
	}
	if current.Status == StatusRunning || current.Status == StatusPending {
		if err := r.Store.UpdateStatus(ctx, id, StatusStopped, nil, ""); err != nil {
			return Job{}, err
		}
		return r.Store.Get(ctx, id)
	}
	return current, nil
}
