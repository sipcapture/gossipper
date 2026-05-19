package toolrun

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sipcapture/gossipper/internal/supervisor"
)

func TestResolveDataPath(t *testing.T) {
	dir := t.TempDir()
	inside := filepath.Join(dir, "media", "x.pcap")
	if err := os.MkdirAll(filepath.Dir(inside), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveDataPath(dir, "media/x.pcap")
	if err != nil {
		t.Fatal(err)
	}
	if got != inside {
		t.Fatalf("got %q want %q", got, inside)
	}
	if _, err := ResolveDataPath(dir, "../etc/passwd"); err == nil {
		t.Fatal("expected escape error")
	}
}

func TestRunInfindex(t *testing.T) {
	dir := t.TempDir()
	csv := filepath.Join(dir, "users.csv")
	if err := os.WriteFile(csv, []byte("alice,1\nbob,2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	artifacts := filepath.Join(dir, "artifacts", "job1")
	spec := supervisor.Spec{
		JobID:        "job1",
		DataDir:      dir,
		ProfileKind:  supervisor.ToolProfileKind,
		ProfileID:    supervisor.ToolInfindex,
		ArtifactsDir: artifacts,
		ToolArgs: map[string]any{
			"csv":   "users.csv",
			"field": 0,
		},
	}
	if err := Run(t.Context(), spec); err != nil {
		t.Fatal(err)
	}
}
