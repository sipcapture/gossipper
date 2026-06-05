// Package safepath provides helpers to ensure resolved file paths stay within
// an expected base directory (path-traversal hardening for CodeQL go/path-injection).
package safepath

import (
	"os"
	"path/filepath"
	"strings"
)

// Within reports whether target resolves inside root after Abs+Clean.
func Within(root, target string) bool {
	rootAbs, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return false
	}
	targetAbs, err := filepath.Abs(filepath.Clean(target))
	if err != nil {
		return false
	}
	if targetAbs == rootAbs {
		return true
	}
	sep := string(os.PathSeparator)
	return strings.HasPrefix(targetAbs, rootAbs+sep)
}

// Join joins base with elems and returns the result only when it stays within base.
func Join(base string, elems ...string) (string, error) {
	base = filepath.Clean(base)
	all := append([]string{base}, elems...)
	candidate := filepath.Clean(filepath.Join(all...))
	if !Within(base, candidate) {
		return "", os.ErrInvalid
	}
	return candidate, nil
}
