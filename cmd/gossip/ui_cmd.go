package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	apiv2 "github.com/sipcapture/gossipper/internal/api/v2"
	"github.com/sipcapture/gossipper/internal/api"
	"github.com/sipcapture/gossipper/internal/settingsauth"
	"github.com/sipcapture/gossipper/internal/supervisor"
	"github.com/sipcapture/gossipper/internal/uistore"
)

// runUICommand implements `gossipper ui` — the supervisor / UI control plane.
// It does NOT start any SIP engine itself: workers are launched on demand via
// the /api/v2/jobs endpoint. Phase 0 wires the StubRunner; Phase 2 replaces it
// with a real fork/exec implementation.
func runUICommand(args []string) error {
	fs := flag.NewFlagSet("ui", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	dataDir := fs.String("data-dir", "./data", "data directory for profiles, scenarios, media, jobs")
	historyKeep := fs.Int("scenario-history-keep", 0, "max archived scenario versions per id (0 = unlimited)")
	listen := fs.String("listen", ":8080", "HTTP listen address (Control UI + /api/v2)")
	authSqlite := fs.String("auth-sqlite", "", "path to SQLite settings DB (defaults to <data-dir>/settings.sqlite)")
	jwtSecret := fs.String("jwt-secret", "", "optional JWT signing secret (>=16 chars); auto-generated and persisted when empty")
	noAuth := fs.Bool("no-auth", false, "disable internal auth (all /api/v2 endpoints public; default is auth enabled)")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `gossipper ui — supervisor + Control UI

Phase 0 wires a stub worker runner: /api/v2/jobs records lifecycle in the
settings DB but does not yet fork real workers (that lands in Phase 2).

Usage:
  gossipper ui [flags]

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	store, err := uistore.OpenWithOptions(*dataDir, uistore.StoreOptions{
		ScenarioHistoryKeep: *historyKeep,
	})
	if err != nil {
		return fmt.Errorf("ui: open data dir: %w", err)
	}

	dbPath := strings.TrimSpace(*authSqlite)
	if dbPath == "" {
		dbPath = store.Layout().SettingsDBPath()
	}
	db, err := settingsauth.OpenStore(dbPath)
	if err != nil {
		return fmt.Errorf("ui: open settings DB: %w", err)
	}
	defer db.Close()

	jobsStore := supervisor.NewJobsStore(db)
	runner := supervisor.NewExecRunner(store.Layout().Root, jobsStore, nil)
	reg := supervisor.NewRegistry(jobsStore, runner)

	var auth *settingsauth.Auth
	if !*noAuth {
		a, err := settingsauth.OpenBootstrap(dbPath, *jwtSecret)
		if err != nil {
			return fmt.Errorf("ui: enable auth: %w", err)
		}
		defer a.Close()
		auth = a
	}

	mux := http.NewServeMux()
	apiv2.New(apiv2.Config{
		Store:    store,
		Registry: reg,
		Auth:     auth,
		Version:  GetShortVersionString(),
	}).Register(mux)
	api.RegisterEmbeddedControlUI(mux)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})

	srv := &http.Server{Addr: *listen, Handler: mux}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		uiNote := "embedded Control UI at /"
		if !api.HasEmbeddedControlUI() {
			uiNote = "Control UI NOT embedded (rebuild with: make frontend && go build)"
		}
		fmt.Fprintf(os.Stderr, "gossipper ui: data-dir=%s listen=%s auth=%s %s\n",
			store.Layout().Root, *listen, authLabel(auth), uiNote)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithCancel(context.Background())
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		<-errCh
		return nil
	case err := <-errCh:
		return err
	}
}

func authLabel(a *settingsauth.Auth) string {
	if a != nil && a.Enabled() {
		return "internal(jwt+sqlite)"
	}
	return "none"
}
