// Package uistore implements the on-disk layout used by the UI control plane
// of `gossipper ui`. Profiles, scenarios, and media are stored as version-friendly
// JSON / text files under a single data directory. Stateful artifacts (users,
// jobs, audit log) live in the SQLite settings DB owned by the settingsauth
// package.
//
// Layout (relative to Layout.Root):
//
//	settings.sqlite              # auth + jobs + artifacts + audit
//	profiles/servers/<id>.json   # ServerProfile
//	profiles/clients/<id>.json   # ClientProfile
//	scenarios/<id>.xml           # raw SIP XML
//	scenarios/<id>.meta.json     # ScenarioMeta (sidecar)
//	media/wav/<file>.wav         # RFC4733-friendly PCM/WAV uploads
//	media/pcap/<file>.pcap       # PCAP recordings for replay tests
//	artifacts/jobs/<job-id>/...  # per-job recordings, summary, reports
//	tmp/                         # atomic write staging
package uistore

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Layout describes the on-disk roots used by the UI store. Call Ensure to
// create any missing directories with reasonable permissions.
type Layout struct {
	Root string
}

// New normalises and returns a Layout rooted at the given directory.
func New(root string) (Layout, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return Layout{}, fmt.Errorf("uistore: data root is empty")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return Layout{}, fmt.Errorf("uistore: abs %q: %w", root, err)
	}
	return Layout{Root: abs}, nil
}

// Ensure creates the standard sub-directories (idempotent).
func (l Layout) Ensure() error {
	for _, dir := range []string{
		l.Root,
		l.ServersDir(),
		l.ClientsDir(),
		l.ScenariosDir(),
		l.WavDir(),
		l.PcapDir(),
		l.JobsArtifactsDir(),
		l.TempDir(),
	} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("uistore: mkdir %q: %w", dir, err)
		}
	}
	return nil
}

// SettingsDBPath returns the SQLite settings DB path (auth + jobs + audit).
func (l Layout) SettingsDBPath() string { return filepath.Join(l.Root, "settings.sqlite") }

func (l Layout) ServersDir() string       { return filepath.Join(l.Root, "profiles", "servers") }
func (l Layout) ClientsDir() string       { return filepath.Join(l.Root, "profiles", "clients") }
func (l Layout) ScenariosDir() string     { return filepath.Join(l.Root, "scenarios") }
func (l Layout) WavDir() string           { return filepath.Join(l.Root, "media", "wav") }
func (l Layout) PcapDir() string          { return filepath.Join(l.Root, "media", "pcap") }
func (l Layout) JobsArtifactsDir() string { return filepath.Join(l.Root, "artifacts", "jobs") }
func (l Layout) TempDir() string          { return filepath.Join(l.Root, "tmp") }

// JobArtifactDir returns the per-job artifact directory; ensures it exists
// before returning (callers may rely on the path being writable).
func (l Layout) JobArtifactDir(jobID string) (string, error) {
	jobID = strings.TrimSpace(jobID)
	if !isSafeID(jobID) {
		return "", fmt.Errorf("uistore: invalid job id %q", jobID)
	}
	p := filepath.Join(l.JobsArtifactsDir(), jobID)
	if err := os.MkdirAll(p, 0o750); err != nil {
		return "", err
	}
	return p, nil
}
