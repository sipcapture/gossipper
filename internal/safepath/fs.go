package safepath

import (
	"os"
	"path/filepath"
	"strings"
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

// EnsureJobArtifactsDir returns the per-job artifacts directory, creating it when missing.
func EnsureJobArtifactsDir(dataRoot, jobID string, perm os.FileMode) (string, error) {
	dir, err := JobArtifactsDir(dataRoot, jobID)
	if err != nil {
		return "", err
	}
	dirAbs, err := resolveUnderRoot(dataRoot, dir)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dirAbs, perm); err != nil {
		return "", err
	}
	return dirAbs, nil
}

// OpenJobArtifact opens a file with a constant basename under a job artifacts directory.
func OpenJobArtifact(dataRoot, jobID, name string, flag int, perm os.FileMode) (*os.File, error) {
	dir, err := EnsureJobArtifactsDir(dataRoot, jobID, 0o750)
	if err != nil {
		return nil, err
	}
	path, err := Join(dir, name)
	if err != nil {
		return nil, err
	}
	pathAbs, err := resolveUnderRoot(dir, path)
	if err != nil {
		return nil, err
	}
	return os.OpenFile(pathAbs, flag, perm)
}

// ReadFile reads target after verifying it resolves inside root.
func ReadFile(root, target string) ([]byte, error) {
	abs, err := resolveUnderRoot(root, target)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(abs)
}

// Stat stats target after verifying it resolves inside root.
func Stat(root, target string) (os.FileInfo, error) {
	abs, err := resolveUnderRoot(root, target)
	if err != nil {
		return nil, err
	}
	return os.Stat(abs)
}

// ReadDir lists entries under dir after verifying dir resolves inside baseRoot.
func ReadDir(baseRoot, dir string) ([]os.DirEntry, error) {
	abs, err := resolveUnderRoot(baseRoot, dir)
	if err != nil {
		return nil, err
	}
	return os.ReadDir(abs)
}

// EnsureDirUnder creates dir when it resolves inside root.
func EnsureDirUnder(root, dir string, perm os.FileMode) error {
	abs, err := resolveUnderRoot(root, dir)
	if err != nil {
		return err
	}
	return os.MkdirAll(abs, perm)
}

// RenameUnder renames src to dst when both paths resolve inside root.
func RenameUnder(root, src, dst string) error {
	srcAbs, err := resolveUnderRoot(root, src)
	if err != nil {
		return err
	}
	dstAbs, err := resolveUnderRoot(root, dst)
	if err != nil {
		return err
	}
	return os.Rename(srcAbs, dstAbs)
}

func resolveUnderRoot(root, target string) (string, error) {
	rootAbs, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return "", err
	}
	targetAbs, err := filepath.Abs(filepath.Clean(target))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", os.ErrInvalid
	}
	if targetAbs != rootAbs && !strings.HasPrefix(targetAbs, rootAbs+string(os.PathSeparator)) {
		return "", os.ErrInvalid
	}
	return targetAbs, nil
}
