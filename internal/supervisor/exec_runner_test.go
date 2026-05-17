package supervisor

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/sipcapture/gossipper/internal/settingsauth"
	"github.com/sipcapture/gossipper/internal/uistore"
)

// TestExecRunnerEndToEnd builds the gossipper binary, drops a sane server
// profile + scenario into a fresh uistore, starts a job via Registry+ExecRunner,
// and verifies the job transitions to a terminal state with artefacts on disk.
func TestExecRunnerEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode: skipping fork/exec test")
	}
	bin := buildGossipperBinary(t)
	dataDir := mkTempDir(t)

	store, err := uistore.Open(dataDir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	// Minimal UAS scenario that completes immediately when -m 1 forces a
	// single-call termination (the BuildConfigFromSpec mapper does not set -m
	// explicitly so we rely on engine.Run returning when ctx is cancelled).
	xml := `<?xml version="1.0"?><scenario name="t"><recv request="INVITE" optional="true" timeout="100"/></scenario>`
	if _, err := store.PutScenario(uistore.ScenarioMeta{ID: "t", Name: "t", Role: "server"}, xml, true); err != nil {
		t.Fatalf("put scenario: %v", err)
	}
	if _, err := store.PutServerProfile(uistore.ServerProfile{
		ID:          "srv",
		Name:        "srv",
		ScenarioRef: "t",
		Transports: []uistore.TransportSpec{{
			Transport: "u1", LocalIP: "127.0.0.1", LocalPort: pickFreePort(t), Enabled: true,
		}},
		MaxConcurrent: 4,
	}, true); err != nil {
		t.Fatalf("put server: %v", err)
	}

	db, err := settingsauth.OpenStore(filepath.Join(dataDir, "settings.sqlite"))
	if err != nil {
		t.Fatalf("settings db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	jobsStore := NewJobsStore(db)
	runner := NewExecRunner(dataDir, jobsStore, nil)
	runner.Binary = bin
	runner.StopTimeout = 2 * time.Second
	reg := NewRegistry(jobsStore, runner)

	artifactsDir := filepath.Join(dataDir, "artifacts", "jobs", "j1")
	_ = os.MkdirAll(artifactsDir, 0o750)

	job, err := reg.StartJob(context.Background(),
		Job{ID: "j1", ProfileID: "srv", ProfileKind: "server", ScenarioID: "t", ArtifactsDir: artifactsDir},
		Spec{
			JobID: "j1", DataDir: dataDir, ProfileID: "srv", ProfileKind: "server",
			ScenarioID: "t", ArtifactsDir: artifactsDir, StatsIntervalMs: 200,
			Engine: map[string]any{"global_timeout_ms": 1500},
		},
	)
	if err != nil {
		t.Fatalf("StartJob: %v", err)
	}
	if job.Status != StatusRunning {
		t.Fatalf("expected running, got %q", job.Status)
	}
	// Wait until the worker exits (engine returns when GlobalTimeout fires).
	deadline := time.Now().Add(10 * time.Second)
	var final Job
	for time.Now().Before(deadline) {
		time.Sleep(150 * time.Millisecond)
		final, err = jobsStore.Get(context.Background(), "j1")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if final.Status == StatusSucceeded || final.Status == StatusFailed {
			break
		}
	}
	if final.Status != StatusSucceeded && final.Status != StatusFailed {
		t.Fatalf("worker never finished: status=%q", final.Status)
	}
	// At least the stats.jsonl / worker.log should exist with size > 0 because
	// the worker emits a "started" event up-front.
	if info, err := os.Stat(filepath.Join(artifactsDir, "stats.jsonl")); err != nil || info.Size() == 0 {
		t.Fatalf("stats.jsonl missing or empty: %v", err)
	}
	arts, err := jobsStore.ListArtifacts(context.Background(), "j1")
	if err != nil {
		t.Fatalf("ListArtifacts: %v", err)
	}
	if len(arts) == 0 {
		t.Fatalf("expected at least one artifact, got 0")
	}
}

func TestExecRunnerStopKillsWorker(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	bin := buildGossipperBinary(t)
	dataDir := mkTempDir(t)
	store, err := uistore.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	xml := `<?xml version="1.0"?><scenario name="t"><recv request="INVITE" optional="true" timeout="60000"/></scenario>`
	if _, err := store.PutScenario(uistore.ScenarioMeta{ID: "t", Name: "t", Role: "server"}, xml, true); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PutServerProfile(uistore.ServerProfile{
		ID: "srv", Name: "srv", ScenarioRef: "t",
		Transports: []uistore.TransportSpec{{
			Transport: "u1", LocalIP: "127.0.0.1", LocalPort: pickFreePort(t), Enabled: true,
		}},
	}, true); err != nil {
		t.Fatal(err)
	}
	db, err := settingsauth.OpenStore(filepath.Join(dataDir, "settings.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	store2 := NewJobsStore(db)
	runner := NewExecRunner(dataDir, store2, nil)
	runner.Binary = bin
	runner.StopTimeout = 1 * time.Second
	reg := NewRegistry(store2, runner)

	artifactsDir := filepath.Join(dataDir, "artifacts", "jobs", "jstop")
	_ = os.MkdirAll(artifactsDir, 0o750)
	if _, err := reg.StartJob(context.Background(),
		Job{ID: "jstop", ProfileID: "srv", ProfileKind: "server", ScenarioID: "t", ArtifactsDir: artifactsDir},
		Spec{JobID: "jstop", DataDir: dataDir, ProfileID: "srv", ProfileKind: "server", ScenarioID: "t", ArtifactsDir: artifactsDir},
	); err != nil {
		t.Fatalf("StartJob: %v", err)
	}
	// Wait briefly so worker actually starts the engine.
	time.Sleep(400 * time.Millisecond)
	if !runner.IsRunning("jstop") {
		t.Fatalf("worker should be running")
	}
	if _, err := reg.StopJob(context.Background(), "jstop"); err != nil {
		t.Fatalf("StopJob: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !runner.IsRunning("jstop") {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("worker still running after Stop")
}

// mkTempDir is t.TempDir() without the failing RemoveAll — SQLite WAL files
// occasionally linger after Close so we swallow ENOTEMPTY at the end of the
// test run.
func mkTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "supervisor-test-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() {
		// best-effort cleanup; we deliberately ignore errors
		_ = os.RemoveAll(dir)
	})
	return dir
}

func buildGossipperBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "gossipper-test")
	cmd := exec.Command("go", "build", "-o", bin, "github.com/sipcapture/gossipper/cmd/gossip")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, string(out))
	}
	return bin
}

// pickFreePort returns a TCP port we expect to be free for the duration of
// the test. The same port number is reused for the UAS UDP listener — on
// short test runs port collisions are very unlikely.
func pickFreePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}
