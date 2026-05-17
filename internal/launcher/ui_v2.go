package launcher

import (
	"fmt"
	"strings"

	"github.com/sipcapture/gossipper/internal/cli"
	"github.com/sipcapture/gossipper/internal/settingsauth"
	"github.com/sipcapture/gossipper/internal/supervisor"
	"github.com/sipcapture/gossipper/internal/uistore"
)

// UIBundle groups the optional admin-console resources mounted under
// /api/v2/* on the management HTTP server. All three fields are populated
// by openUIBundle when cli.Config.UIDataDir is non-empty; Close releases
// the SQLite handle.
type UIBundle struct {
	Store    *uistore.Store
	Registry *supervisor.Registry
	closer   func() error
}

// Close releases the underlying SQLite settings DB used by the JobsStore.
// uistore is filesystem-only and does not require an explicit close.
func (b *UIBundle) Close() error {
	if b == nil || b.closer == nil {
		return nil
	}
	return b.closer()
}

// openUIBundle opens the on-disk admin console state (profiles / scenarios /
// media + the SQLite jobs store) for cfg.UIDataDir. Returns (nil, nil) when
// UIDataDir is empty so callers can treat "no UI" as a normal mode.
func openUIBundle(cfg cli.Config) (*UIBundle, error) {
	dir := strings.TrimSpace(cfg.UIDataDir)
	if dir == "" {
		return nil, nil
	}
	store, err := uistore.Open(dir)
	if err != nil {
		return nil, fmt.Errorf("ui v2: open data dir %q: %w", dir, err)
	}
	db, err := settingsauth.OpenStore(store.Layout().SettingsDBPath())
	if err != nil {
		return nil, fmt.Errorf("ui v2: open jobs DB: %w", err)
	}
	jobsStore := supervisor.NewJobsStore(db)
	runner := supervisor.NewExecRunner(store.Layout().Root, jobsStore, nil)
	reg := supervisor.NewRegistry(jobsStore, runner)
	return &UIBundle{
		Store:    store,
		Registry: reg,
		closer:   db.Close,
	}, nil
}
