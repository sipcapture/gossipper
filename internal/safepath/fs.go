package safepath

import (
	"os"
	"path/filepath"
)

// JobArtifactsDir returns <dataRoot>/artifacts/jobs/<jobID> when jobID is safe.
func JobArtifactsDir(dataRoot, jobID string) (string, error) {
	if !SafeID(jobID) {
		return "", os.ErrInvalid
	}
	jobsRoot, err := Join(dataRoot, "artifacts", "jobs")
	if err != nil {
		return "", err
	}
	return Join(jobsRoot, jobID)
}

// ReadFile reads target after verifying it resolves inside root.
func ReadFile(root, target string) ([]byte, error) {
	if !Within(root, target) {
		return nil, os.ErrInvalid
	}
	return os.ReadFile(target)
}

// Stat stats target after verifying it resolves inside root.
func Stat(root, target string) (os.FileInfo, error) {
	if !Within(root, target) {
		return nil, os.ErrInvalid
	}
	return os.Stat(target)
}

// MkdirAll creates dir; dir must have been produced by this package (e.g. JobArtifactsDir).
func MkdirAll(dir string, perm os.FileMode) error {
	dir = filepath.Clean(dir)
	if dir == "" || dir == "." {
		return os.ErrInvalid
	}
	return os.MkdirAll(dir, perm)
}

// OpenFile opens root/name after Join validation.
func OpenFile(root, name string, flag int, perm os.FileMode) (*os.File, error) {
	path, err := Join(root, name)
	if err != nil {
		return nil, err
	}
	return os.OpenFile(path, flag, perm)
}

// ReadDir lists root after clean; entries are joined with safepath.Join before use.
func ReadDir(root string) ([]os.DirEntry, error) {
	root = filepath.Clean(root)
	if root == "" || root == "." {
		return nil, os.ErrInvalid
	}
	return os.ReadDir(root)
}
