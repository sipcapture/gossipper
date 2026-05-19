package pcap2scenario

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// OutDirFromArgs resolves the output directory for a pcap2scenario tool job.
func OutDirFromArgs(dataDir, artifactsDir string, args map[string]any) (string, error) {
	outDir := argString(args, "out_dir")
	if outDir == "" {
		if rel := argString(args, "out"); rel != "" {
			return resolveDataPath(dataDir, rel)
		}
		if artifactsDir != "" {
			return filepath.Join(artifactsDir, "scenarios"), nil
		}
		return "", errors.New("out_dir or artifacts dir is required")
	}
	return resolveDataPath(dataDir, outDir)
}

func argString(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	v, ok := args[key]
	if !ok || v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	default:
		return strings.TrimSpace(fmt.Sprint(x))
	}
}

func resolveDataPath(dataDir, rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return "", errors.New("path is required")
	}
	root, err := filepath.Abs(dataDir)
	if err != nil {
		return "", err
	}
	var abs string
	if filepath.IsAbs(rel) {
		abs = filepath.Clean(rel)
	} else {
		abs = filepath.Clean(filepath.Join(root, rel))
	}
	if !pathWithinRoot(root, abs) {
		return "", fmt.Errorf("path %q escapes data-dir", rel)
	}
	return abs, nil
}

func pathWithinRoot(root, abs string) bool {
	if abs == root {
		return true
	}
	return strings.HasPrefix(abs, root+string(os.PathSeparator))
}
