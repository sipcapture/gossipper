package launcher

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/sipcapture/gossipper/internal/api"
	"github.com/sipcapture/gossipper/internal/cli"
	"github.com/sipcapture/gossipper/internal/settingsauth"
	"github.com/sipcapture/gossipper/internal/supervisor"
	"github.com/sipcapture/gossipper/internal/uistore"
)

// warnIfNoV2 prints a friendly hint when management API is up but the admin
// console (/api/v2/*) is not — this is the #1 reason new operators hit 404
// on /api/v2/health or /api/v2/servers after enabling api_addr.
func warnIfNoV2(cfg cli.Config, s *api.Server) {
	if s == nil || s.V2Enabled() {
		return
	}
	if strings.TrimSpace(cfg.ApiAddr) == "" {
		return
	}
	fmt.Fprintln(os.Stderr,
		`api: admin console (/api/v2/*) NOT mounted — set "ui_data_dir" in the management config (or pass -ui_data_dir) to enable it. /api/v1/* is the only API on this listener.`)
}

// apiMountSummary describes which API surfaces the given server has mounted.
// Returned strings are intentionally compact so they fit in a single startup
// log line ("/api/v1/* + /api/v2/*").
func apiMountSummary(s *api.Server) string {
	parts := []string{}
	if s.V1Enabled() {
		parts = append(parts, "/api/v1/*")
	}
	if s.V2Enabled() {
		parts = append(parts, "/api/v2/* (admin console)")
	}
	if len(parts) == 0 {
		return "(none — only embedded UI)"
	}
	return strings.Join(parts, " + ")
}

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

// LegacyV1Enabled returns the effective on/off state of the /api/v1/*
// surface for cfg. Honours an explicit LegacyAPIV1Set flag, otherwise
// auto-disables v1 whenever the admin console (UIDataDir) is mounted —
// the assumption being that a deployment opting into v2 doesn't want the
// older client API quietly tagging along.
func LegacyV1Enabled(cfg cli.Config) bool {
	if cfg.LegacyAPIV1Set {
		return cfg.LegacyAPIV1
	}
	if strings.TrimSpace(cfg.UIDataDir) != "" {
		return false
	}
	return true
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
	if err := seedProfilesFromConfig(store, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "api: warn: seed profiles from management config failed: %v\n", err)
	}
	return &UIBundle{
		Store:    store,
		Registry: reg,
		closer:   db.Close,
	}, nil
}

// seedProfilesFromConfig populates the empty UI store with the server bind
// and any joined-clients defined in the management JSON, so freshly-installed
// operators see what they configured already in the admin console instead of
// an empty Servers / Clients page. Seeding is one-shot: any existing profile
// suppresses it (so manual edits / additions never get clobbered on restart).
func seedProfilesFromConfig(store *uistore.Store, cfg cli.Config) error {
	if store == nil {
		return nil
	}
	servers, err := store.ListServerProfiles()
	if err != nil {
		return fmt.Errorf("list servers: %w", err)
	}
	clients, err := store.ListClientProfiles()
	if err != nil {
		return fmt.Errorf("list clients: %w", err)
	}
	if len(servers) > 0 || len(clients) > 0 {
		return nil
	}

	var seeded int

	if srv := serverProfileFromConfig(cfg); srv != nil {
		if _, err := store.PutServerProfile(*srv, true); err == nil {
			seeded++
		} else if !errors.Is(err, uistore.ErrConflict) {
			fmt.Fprintf(os.Stderr, "api: warn: seed server profile %q: %v\n", srv.ID, err)
		}
	}

	used := map[string]struct{}{}
	for i, jc := range cfg.JoinedClients {
		id := strings.TrimSpace(jc.ID)
		if id == "" {
			id = fmt.Sprintf("client-%d", i+1)
		}
		if _, dup := used[id]; dup {
			id = fmt.Sprintf("%s-%d", id, i+1)
		}
		used[id] = struct{}{}
		cp := clientProfileFromConfig(id, jc.Config)
		if cp == nil {
			continue
		}
		if _, err := store.PutClientProfile(*cp, true); err == nil {
			seeded++
		} else if !errors.Is(err, uistore.ErrConflict) {
			fmt.Fprintf(os.Stderr, "api: warn: seed client profile %q: %v\n", cp.ID, err)
		}
	}

	if seeded > 0 {
		fmt.Fprintf(os.Stderr, "api: seeded %d profile(s) from management config into %s\n", seeded, store.Layout().Root)
	}
	return nil
}

func serverProfileFromConfig(cfg cli.Config) *uistore.ServerProfile {
	transports := serverTransportsFromConfig(cfg)
	if len(transports) == 0 {
		return nil
	}
	id := strings.TrimSpace(cfg.ServerProfileID)
	if id == "" {
		id = "management"
	}
	return &uistore.ServerProfile{
		ID:            id,
		Name:          id,
		Description:   "Imported from management config on first start.",
		ScenarioRef:   strings.TrimSpace(cfg.ScenarioName),
		Transports:    transports,
		MaxConcurrent: cfg.MaxConcurrent,
		Notes:         "built-in: owned by the management process (cfg.Server). Edit the management JSON and restart the service to change bindings; Start/Stop from the UI does not apply.",
	}
}

func serverTransportsFromConfig(cfg cli.Config) []uistore.TransportSpec {
	var out []uistore.TransportSpec
	for _, l := range cfg.ServerListeners {
		if strings.TrimSpace(l.Transport) == "" {
			continue
		}
		out = append(out, uistore.TransportSpec{
			Transport: l.Transport,
			LocalIP:   l.LocalIP,
			LocalPort: l.LocalPort,
			Enabled:   true,
		})
	}
	if len(out) == 0 && strings.TrimSpace(cfg.Transport) != "" && cfg.LocalPort > 0 {
		out = append(out, uistore.TransportSpec{
			Transport: cfg.Transport,
			LocalIP:   cfg.LocalIP,
			LocalPort: cfg.LocalPort,
			Enabled:   true,
		})
	}
	return out
}

func clientProfileFromConfig(id string, cfg cli.Config) *uistore.ClientProfile {
	transport := strings.TrimSpace(cfg.Transport)
	if transport == "" {
		return nil
	}
	cp := &uistore.ClientProfile{
		ID:          id,
		Name:        id,
		Description: "Imported from management config on first start.",
		ScenarioRef: strings.TrimSpace(cfg.ScenarioName),
		Transports: []uistore.TransportSpec{{
			Transport: transport,
			LocalIP:   cfg.LocalIP,
			LocalPort: cfg.LocalPort,
			Enabled:   true,
		}},
		RemoteIP:      cfg.RemoteHost,
		RemotePort:    cfg.RemotePort,
		Rate:          cfg.Rate,
		MaxConcurrent: cfg.MaxConcurrent,
		Notes:         "built-in: owned by the management process (cfg.JoinedClients). Edit the management JSON and restart the service to change settings; Start/Stop from the UI does not apply.",
	}
	return cp
}
