package uistore

import (
	"os"

	"github.com/sipcapture/gossipper/internal/safepath"
)

func ensurePathWithin(baseDir, targetPath string) error {
	if !safepath.Within(baseDir, targetPath) {
		return ErrInvalidID
	}
	return nil
}

func readFileWithin(baseDir, path string) ([]byte, error) {
	if err := ensurePathWithin(baseDir, path); err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

func statWithin(baseDir, path string) (os.FileInfo, error) {
	if err := ensurePathWithin(baseDir, path); err != nil {
		return nil, err
	}
	return os.Stat(path)
}
