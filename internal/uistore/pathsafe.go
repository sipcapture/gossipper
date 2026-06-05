package uistore

import (
	"errors"
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
	data, err := safepath.ReadFile(baseDir, path)
	if errors.Is(err, os.ErrInvalid) {
		return nil, ErrInvalidID
	}
	return data, err
}

func statWithin(baseDir, path string) (os.FileInfo, error) {
	info, err := safepath.Stat(baseDir, path)
	if errors.Is(err, os.ErrInvalid) {
		return nil, ErrInvalidID
	}
	return info, err
}
