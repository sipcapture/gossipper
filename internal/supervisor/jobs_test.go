package supervisor

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/sipcapture/gossipper/internal/settingsauth"
)

func newStore(t *testing.T) *JobsStore {
	t.Helper()
	db, err := settingsauth.OpenStore(filepath.Join(t.TempDir(), "settings.sqlite"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewJobsStore(db)
}

func TestJobsCRUD(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()
	created, err := store.Create(ctx, Job{ID: "job-1", ProfileID: "primary", ProfileKind: "server", ScenarioID: "uas_basic", ArtifactsDir: "/tmp/jobs/job-1"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Status != StatusPending {
		t.Fatalf("expected pending, got %q", created.Status)
	}
	if err := store.UpdateStatus(ctx, "job-1", StatusRunning, nil, ""); err != nil {
		t.Fatalf("UpdateStatus running: %v", err)
	}
	if err := store.SetPID(ctx, "job-1", 4242); err != nil {
		t.Fatalf("SetPID: %v", err)
	}
	got, err := store.Get(ctx, "job-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.PID == nil || *got.PID != 4242 {
		t.Fatalf("pid mismatch: %+v", got)
	}
	if got.Status != StatusRunning {
		t.Fatalf("status mismatch: %q", got.Status)
	}
	if got.StartedAt == nil {
		t.Fatalf("StartedAt should be set after StatusRunning")
	}
	exit := 0
	if err := store.UpdateStatus(ctx, "job-1", StatusSucceeded, &exit, ""); err != nil {
		t.Fatalf("UpdateStatus succeeded: %v", err)
	}
	final, err := store.Get(ctx, "job-1")
	if err != nil {
		t.Fatalf("Get after success: %v", err)
	}
	if final.Status != StatusSucceeded || final.FinishedAt == nil || final.ExitCode == nil || *final.ExitCode != 0 {
		t.Fatalf("final mismatch: %+v", final)
	}
	if err := store.AddArtifact(ctx, "job-1", "wav", "/tmp/jobs/job-1/call.wav", 1024); err != nil {
		t.Fatalf("AddArtifact: %v", err)
	}
	arts, err := store.ListArtifacts(ctx, "job-1")
	if err != nil || len(arts) != 1 {
		t.Fatalf("ListArtifacts len=%d err=%v", len(arts), err)
	}
	list, err := store.List(ctx, 0)
	if err != nil || len(list) != 1 {
		t.Fatalf("List len=%d err=%v", len(list), err)
	}
	if err := store.Delete(ctx, "job-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.Get(ctx, "job-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestRegistryStartStopWithStub(t *testing.T) {
	store := newStore(t)
	reg := NewRegistry(store, NewStubRunner())
	ctx := context.Background()
	job, err := reg.StartJob(ctx, Job{ID: "abc", ProfileID: "stress", ProfileKind: "client"},
		Spec{JobID: "abc", ProfileID: "stress", ProfileKind: "client"})
	if err != nil {
		t.Fatalf("StartJob: %v", err)
	}
	if job.Status != StatusRunning {
		t.Fatalf("expected running, got %q", job.Status)
	}
	if job.PID == nil {
		t.Fatalf("PID should be set after start")
	}
	stopped, err := reg.StopJob(ctx, "abc")
	if err != nil {
		t.Fatalf("StopJob: %v", err)
	}
	if stopped.Status != StatusStopped {
		t.Fatalf("expected stopped, got %q", stopped.Status)
	}
}

func TestRegistryStartFailureMarksFailed(t *testing.T) {
	store := newStore(t)
	runner := NewStubRunner()
	runner.AlwaysError = errors.New("boom")
	reg := NewRegistry(store, runner)
	job, err := reg.StartJob(context.Background(), Job{ID: "x"}, Spec{JobID: "x"})
	if err != nil {
		t.Fatalf("StartJob: %v", err)
	}
	if job.Status != StatusFailed {
		t.Fatalf("expected failed, got %q", job.Status)
	}
	if job.Error == "" {
		t.Fatalf("Error message should be populated")
	}
}
