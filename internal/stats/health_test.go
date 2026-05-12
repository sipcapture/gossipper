package stats

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteJSONIncludesSchemaAndFindings(t *testing.T) {
	t.Parallel()
	collector := New()
	collector.StartCall()
	collector.FinishCall(true, 5*time.Millisecond)

	path := filepath.Join(t.TempDir(), "summary.json")
	if err := collector.WriteJSON(path, nil); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var s Summary
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	if s.SchemaVersion != SummarySchemaVersion {
		t.Fatalf("schema_version: got %q", s.SchemaVersion)
	}
	if len(s.Findings) == 0 {
		t.Fatalf("expected findings, got %+v", s.Findings)
	}
}

func TestHealthMaxFailedCallsZero(t *testing.T) {
	t.Parallel()
	cfg := HealthConfig{MaxFailedCalls: 0}
	if !cfg.Active() {
		t.Fatal("expected health active")
	}
	s := Summary{SuccessCalls: 0, FailedCalls: 1, TotalCalls: 1, SuccessRatio: 0}
	h, reasons := EvaluateHealth(cfg, s)
	if h == nil || h.Pass {
		t.Fatalf("expected fail, got %+v reasons=%v", h, reasons)
	}
	if len(reasons) == 0 {
		t.Fatal("expected reasons")
	}
}

func TestHealthMinSuccessRatio(t *testing.T) {
	t.Parallel()
	cfg := HealthConfig{MinSuccessRatio: 0.99}
	s := Summary{SuccessCalls: 8, FailedCalls: 2, TotalCalls: 10, SuccessRatio: 0.8}
	h, _ := EvaluateHealth(cfg, s)
	if h.Pass {
		t.Fatal("expected fail")
	}
}

func TestWriteJSONHealthPass(t *testing.T) {
	t.Parallel()
	collector := New()
	collector.StartCall()
	collector.FinishCall(true, 1)
	path := filepath.Join(t.TempDir(), "out.json")
	opts := &SummaryWriteOptions{
		ToolVersion: "test",
		Health:      HealthConfig{MinSuccessRatio: 0.5},
	}
	if err := collector.WriteJSON(path, opts); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	var s Summary
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	if s.Health == nil || !s.Health.Active || !s.Health.Pass {
		t.Fatalf("health: %+v", s.Health)
	}
}
