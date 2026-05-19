package pcap2scenario

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOutDirFromArgsDefaults(t *testing.T) {
	dir, err := OutDirFromArgs("/data", "/data/artifacts/jobs/j1", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if dir != filepath.Join("/data/artifacts/jobs/j1", "scenarios") {
		t.Fatalf("got %q", dir)
	}
}

func TestOutDirFromArgsRelativeOut(t *testing.T) {
	root := t.TempDir()
	rel := "artifacts/jobs/x/scenarios"
	dir, err := OutDirFromArgs(root, "", map[string]any{"out": rel})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, rel)
	if dir != want {
		t.Fatalf("got %q want %q", dir, want)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}
