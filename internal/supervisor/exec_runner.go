package supervisor

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/sipcapture/gossipper/internal/safepath"
)

// ExecRunner spawns `gossipper worker --spec=<path>` as a child process for
// every started job, drains stdout (JSON-lines stats) and stderr (free-form
// log), and updates the JobsStore when the child exits.
type ExecRunner struct {
	// Binary is the absolute path of the gossipper executable. When empty,
	// os.Executable() is used.
	Binary string
	// DataDir is the master's uistore data dir; written into Spec.DataDir.
	DataDir string
	// Store receives lifecycle updates when the child exits.
	Store *JobsStore
	// Logger receives free-form messages.
	Logger *slog.Logger
	// StopTimeout is how long Stop waits between SIGTERM and SIGKILL.
	StopTimeout time.Duration

	mu      sync.Mutex
	running map[string]*runningWorker
}

type runningWorker struct {
	jobID    string
	cmd      *exec.Cmd
	spec     Spec
	specPath string
	cancel   context.CancelFunc
	doneCh   chan struct{}
	// stopRequested is set by Stop() so waitWorker does not overwrite the
	// status with succeeded/failed when the worker exits after SIGTERM.
	stopRequested bool
}

// NewExecRunner wires an ExecRunner with the master's job store.
func NewExecRunner(dataDir string, store *JobsStore, logger *slog.Logger) *ExecRunner {
	if logger == nil {
		logger = slog.Default()
	}
	return &ExecRunner{
		DataDir:     dataDir,
		Store:       store,
		Logger:      logger,
		StopTimeout: 5 * time.Second,
		running:     map[string]*runningWorker{},
	}
}

func (r *ExecRunner) binary() (string, error) {
	if r.Binary != "" {
		return r.Binary, nil
	}
	return os.Executable()
}

// Start serialises spec, forks `gossipper worker --spec=<path>`, drains output
// in background goroutines and returns the PID. The caller (Registry) flips
// the job to running on success; on exit the runner flips to succeeded /
// failed / stopped depending on exit code and a Stop flag.
func (r *ExecRunner) Start(ctx context.Context, spec Spec) (int, error) {
	if spec.JobID == "" {
		return 0, errors.New("supervisor: spec.JobID is required")
	}
	if spec.DataDir == "" {
		spec.DataDir = r.DataDir
	}
	bin, err := r.binary()
	if err != nil {
		return 0, fmt.Errorf("locate gossipper binary: %w", err)
	}
	specPath, err := writeSpecFile(spec)
	if err != nil {
		return 0, err
	}

	runCtx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(runCtx, bin, "worker", "--spec", specPath)
	// Make the child its own process group so SIGTERM can target it without
	// affecting the master.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		_ = os.Remove(specPath)
		return 0, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		_ = os.Remove(specPath)
		return 0, err
	}
	if err := cmd.Start(); err != nil {
		cancel()
		_ = os.Remove(specPath)
		return 0, err
	}
	pid := cmd.Process.Pid

	rw := &runningWorker{
		jobID:    spec.JobID,
		cmd:      cmd,
		spec:     spec,
		specPath: specPath,
		cancel:   cancel,
		doneCh:   make(chan struct{}),
	}
	r.mu.Lock()
	r.running[spec.JobID] = rw
	r.mu.Unlock()

	go r.drainStdout(spec.JobID, stdout)
	go r.drainStderr(spec.JobID, stderr)
	go r.waitWorker(rw)

	r.Logger.Info("supervisor: worker started", "job_id", spec.JobID, "pid", pid)
	_ = ctx
	return pid, nil
}

// Stop sends SIGTERM to the worker and waits up to StopTimeout before SIGKILL.
func (r *ExecRunner) Stop(_ context.Context, jobID string) error {
	r.mu.Lock()
	rw, ok := r.running[jobID]
	if ok {
		rw.stopRequested = true
	}
	r.mu.Unlock()
	if !ok {
		return ErrNotFound
	}
	if rw.cmd.Process != nil {
		_ = syscall.Kill(-rw.cmd.Process.Pid, syscall.SIGTERM)
	}
	select {
	case <-rw.doneCh:
		return nil
	case <-time.After(r.StopTimeout):
		if rw.cmd.Process != nil {
			_ = syscall.Kill(-rw.cmd.Process.Pid, syscall.SIGKILL)
		}
		<-rw.doneCh
		return nil
	}
}

// IsRunning reports whether the worker for the given job id is currently alive.
func (r *ExecRunner) IsRunning(jobID string) bool {
	r.mu.Lock()
	_, ok := r.running[jobID]
	r.mu.Unlock()
	return ok
}

func (r *ExecRunner) jobArtifactsDir(jobID string) (string, error) {
	root := strings.TrimSpace(r.DataDir)
	if root == "" {
		return "", errors.New("supervisor: DataDir is required")
	}
	return safepath.JobArtifactsDir(root, jobID)
}

func (r *ExecRunner) drainStdout(jobID string, rc io.ReadCloser) {
	defer rc.Close()
	var statsFile *os.File
	if f, ferr := safepath.OpenJobArtifact(r.DataDir, jobID, "stats.jsonl",
		os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640); ferr == nil {
		statsFile = f
		defer func() {
			if err := f.Close(); err != nil {
				r.Logger.Warn("supervisor: close stats file", "job_id", jobID, "error", err)
			}
		}()
	}
	scanner := bufio.NewScanner(rc)
	scanner.Buffer(make([]byte, 64*1024), 1<<20)
	for scanner.Scan() {
		line := scanner.Bytes()
		if statsFile != nil {
			_, _ = statsFile.Write(append(line, '\n'))
		}
		// Phase 3 surfaces per-job snapshots via /api/v2/live; for now we only
		// peek so the line is not dropped silently.
		var ev map[string]any
		if json.Unmarshal(line, &ev) == nil {
			r.Logger.Debug("supervisor: worker event", "job_id", jobID, "kind", ev["kind"])
		}
	}
}

func (r *ExecRunner) drainStderr(jobID string, rc io.ReadCloser) {
	defer rc.Close()
	var logFile *os.File
	if f, ferr := safepath.OpenJobArtifact(r.DataDir, jobID, "worker.log",
		os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640); ferr == nil {
		logFile = f
		defer func() {
			if err := f.Close(); err != nil {
				r.Logger.Warn("supervisor: close worker log", "job_id", jobID, "error", err)
			}
		}()
	}
	scanner := bufio.NewScanner(rc)
	scanner.Buffer(make([]byte, 64*1024), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if logFile != nil {
			_, _ = logFile.WriteString(line + "\n")
		}
		r.Logger.Info("worker", "job_id", jobID, "msg", line)
	}
}

func (r *ExecRunner) waitWorker(rw *runningWorker) {
	defer close(rw.doneCh)
	defer rw.cancel()
	err := rw.cmd.Wait()
	_ = os.Remove(rw.specPath)

	r.mu.Lock()
	delete(r.running, rw.jobID)
	r.mu.Unlock()

	exit := 0
	failMsg := ""
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			exit = ee.ExitCode()
		} else {
			failMsg = err.Error()
		}
	}
	status := StatusSucceeded
	if exit != 0 || failMsg != "" {
		status = StatusFailed
	}
	r.mu.Lock()
	stopRequested := rw.stopRequested
	r.mu.Unlock()
	if stopRequested {
		status = StatusStopped
	}
	if r.Store != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		exitPtr := exit
		_ = r.Store.UpdateStatus(ctx, rw.jobID, status, &exitPtr, failMsg)
		if dir, err := r.jobArtifactsDir(rw.jobID); err == nil {
			if rw.spec.IsToolJob() {
				RememberToolArtifacts(ctx, r.Store, rw.jobID, dir, rw.spec.ToolID())
			} else {
				r.rememberJobArtifacts(ctx, rw.jobID, dir)
			}
		}
	}
	r.Logger.Info("supervisor: worker exited", "job_id", rw.jobID, "exit", exit, "status", status)
}

func (r *ExecRunner) rememberJobArtifacts(ctx context.Context, jobID, artifactsDir string) {
	for _, item := range []struct {
		kind string
		name string
	}{
		{"stats", "stats.jsonl"},
		{"log", "worker.log"},
		{"summary", "summary.json"},
		{"call_records", "call_records.jsonl"},
	} {
		rememberArtifact(ctx, r.Store, artifactsDir, jobID, item.kind, item.name)
	}
	rememberRecordings(ctx, r.Store, artifactsDir, jobID)
}

func rememberArtifact(ctx context.Context, store *JobsStore, artifactsDir, jobID, kind string, nameParts ...string) {
	if len(nameParts) == 0 {
		return
	}
	path, err := safepath.Join(artifactsDir, nameParts...)
	if err != nil {
		return
	}
	info, err := safepath.Stat(artifactsDir, path)
	if err != nil || info.IsDir() || info.Size() == 0 {
		return
	}
	_ = store.AddArtifact(ctx, jobID, kind, path, info.Size())
}

// rememberRecordings walks <artifacts>/recordings/ and registers every
// non-empty file as a "recording" artifact (kind matches MediaWav for the UI).
func rememberRecordings(ctx context.Context, store *JobsStore, artifactsDir, jobID string) {
	recDir, err := safepath.Join(artifactsDir, "recordings")
	if err != nil {
		return
	}
	entries, err := safepath.ReadDir(artifactsDir, recDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		path, perr := safepath.Join(recDir, e.Name())
		if perr != nil {
			continue
		}
		info, err := e.Info()
		if err != nil || info.Size() == 0 {
			continue
		}
		_ = store.AddArtifact(ctx, jobID, "recording", path, info.Size())
	}
}

func writeSpecFile(spec Spec) (string, error) {
	dir := filepath.Join(spec.DataDir, "tmp")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(dir, "spec-*.json")
	if err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return "", err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// SnapshotRunning returns a copy of the current job→pid map (useful for
// the master to display a live aggregate).
func (r *ExecRunner) SnapshotRunning() map[string]int {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]int, len(r.running))
	for id, rw := range r.running {
		if rw.cmd != nil && rw.cmd.Process != nil {
			out[id] = rw.cmd.Process.Pid
		}
	}
	return out
}
