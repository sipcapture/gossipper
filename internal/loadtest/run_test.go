package loadtest

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/sipcapture/gossipper/internal/settingsauth"
	"github.com/sipcapture/gossipper/internal/supervisor"
	"github.com/sipcapture/gossipper/internal/uistore"
)

func TestParseDirector(t *testing.T) {
	d, err := ParseDirector("10.0.0.5:5060")
	if err != nil {
		t.Fatal(err)
	}
	if d.Host != "10.0.0.5" || d.Port != 5060 {
		t.Fatalf("got %+v", d)
	}
	d, err = ParseDirector("sbc.lab")
	if err != nil || d.Port != 5060 {
		t.Fatalf("default port: %+v err=%v", d, err)
	}
}

func TestRequestValidate(t *testing.T) {
	req := Request{
		Director:      "127.0.0.1:5060",
		ScenarioID:    "invite_media",
		TotalCalls:    5,
		Rate:          2,
		MaxConcurrent: 1,
	}
	if err := req.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestStartBackgroundJob(t *testing.T) {
	dir := t.TempDir()
	store, err := uistore.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	db, err := settingsauth.OpenStore(filepath.Join(dir, "settings.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	jobsStore := supervisor.NewJobsStore(db)
	reg := supervisor.NewRegistry(jobsStore, supervisor.NewStubRunner())

	job, err := Start(context.Background(), store, reg, Request{
		Director:      "127.0.0.1:5060",
		ScenarioID:    "invite_media",
		TotalCalls:    3,
		Rate:          1,
		MaxConcurrent: 1,
		HealthEnabled: true,
		HealthMinSuccessRatio: 0.9,
		HealthMaxFailedCalls:  0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != supervisor.StatusRunning {
		t.Fatalf("status=%q want running", job.Status)
	}
	if job.ProfileKind != string(uistore.KindClient) || job.ProfileID != WizardProfileID {
		t.Fatalf("profile=%q/%q", job.ProfileKind, job.ProfileID)
	}
	got, err := reg.Store.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != supervisor.StatusRunning {
		t.Fatalf("persisted status=%q", got.Status)
	}
}
