package safepath

import (
	"path/filepath"
	"testing"
)

func TestWithin(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "child", "file.txt")
	if !Within(root, child) {
		t.Fatalf("expected %q within %q", child, root)
	}
	outside := filepath.Join(filepath.Dir(root), "escape.txt")
	if Within(root, outside) {
		t.Fatalf("expected %q outside %q", outside, root)
	}
}

func TestJobArtifactsDir(t *testing.T) {
	root := t.TempDir()
	dir, err := JobArtifactsDir(root, "job-1")
	if err != nil {
		t.Fatalf("JobArtifactsDir: %v", err)
	}
	if !Within(root, dir) {
		t.Fatalf("job dir %q escaped root %q", dir, root)
	}
	if _, err := JobArtifactsDir(root, "../evil"); err == nil {
		t.Fatal("expected traversal job id to be rejected")
	}
}

func TestJoinRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	if _, err := Join(root, "..", "etc", "passwd"); err == nil {
		t.Fatal("expected traversal to be rejected")
	}
	got, err := Join(root, "jobs", "job-1")
	if err != nil {
		t.Fatalf("Join: %v", err)
	}
	if !Within(root, got) {
		t.Fatalf("joined path %q escaped root %q", got, root)
	}
}
