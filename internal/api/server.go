package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sipcapture/gossipper/internal/cli"
	"github.com/sipcapture/gossipper/internal/engine"
	"github.com/sipcapture/gossipper/internal/scenario"
)

const maxScenarioBodyBytes = 16 << 20

// ServerConfig configures the gossipper management HTTP API.
type ServerConfig struct {
	Engine *engine.Engine
	CLI    cli.Config
	Token  string // optional; if set, require Authorization: Bearer <Token>
	// ValidateScenario checks a parsed scenario against current CLI flags (transport, 3PCC, etc.).
	// If nil, PUT/apply skip this check (tests only; production should always set it).
	ValidateScenario func(scenario.Scenario) error
	Logger           *slog.Logger
}

// Server exposes REST-style endpoints under /api/v1/.
type Server struct {
	cfg    ServerConfig
	logger *slog.Logger
}

// New returns an API server wrapper (handlers are registered on construction).
func New(cfg ServerConfig) *Server {
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Server{cfg: cfg, logger: log}
}

// Handler returns the root HTTP handler (mux with routes).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", s.wrap(s.handleHealth))
	mux.HandleFunc("GET /api/v1/stats", s.wrap(s.handleStats))
	mux.HandleFunc("GET /api/v1/scenario", s.wrap(s.handleScenarioGet))
	mux.HandleFunc("PUT /api/v1/scenario", s.wrap(s.handleScenarioPut))
	mux.HandleFunc("POST /api/v1/scenario/apply", s.wrap(s.handleScenarioApply))
	mux.HandleFunc("GET /api/v1/control", s.wrap(s.handleControlGet))
	mux.HandleFunc("POST /api/v1/control", s.wrap(s.handleControlPost))
	registerEmbeddedControlUI(mux)
	return mux
}

func (s *Server) wrap(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.Token != "" {
			want := "Bearer " + s.cfg.Token
			got := r.Header.Get("Authorization")
			if len(got) != len(want) || subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
				s.jsonErr(w, http.StatusUnauthorized, "unauthorized")
				return
			}
		}
		next(w, r)
	}
}

func (s *Server) validateScenario(sc scenario.Scenario) error {
	if s.cfg.ValidateScenario == nil {
		return nil
	}
	return s.cfg.ValidateScenario(sc)
}

func (s *Server) jsonErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Engine == nil {
		s.jsonErr(w, http.StatusServiceUnavailable, "engine unavailable")
		return
	}
	snap := s.cfg.Engine.Stats().Snapshot()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(snap); err != nil {
		s.logger.Error("api: encode stats", "error", err)
	}
}

type scenarioGetResponse struct {
	ScenarioFile string `json:"scenario_file"`
	ScenarioName string `json:"scenario_name"`
	XML          string `json:"xml"`
	Builtin      bool   `json:"builtin"`
}

func (s *Server) handleScenarioGet(w http.ResponseWriter, r *http.Request) {
	cfg := s.cfg.CLI
	out := scenarioGetResponse{
		ScenarioFile: cfg.ScenarioFile,
		ScenarioName: cfg.ScenarioName,
	}
	if cfg.ScenarioFile != "" {
		data, err := os.ReadFile(cfg.ScenarioFile)
		if err != nil {
			s.jsonErr(w, http.StatusInternalServerError, fmt.Sprintf("read scenario file: %v", err))
			return
		}
		out.XML = string(data)
	} else {
		out.Builtin = true
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(out); err != nil {
		s.logger.Error("api: encode scenario", "error", err)
	}
}

func (s *Server) handleScenarioPut(w http.ResponseWriter, r *http.Request) {
	cfg := s.cfg.CLI
	if cfg.ScenarioFile == "" {
		s.jsonErr(w, http.StatusBadRequest, "scenario file path is required (-sf); built-in scenarios cannot be updated via API")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxScenarioBodyBytes+1))
	if err != nil {
		s.jsonErr(w, http.StatusBadRequest, fmt.Sprintf("read body: %v", err))
		return
	}
	if len(body) > maxScenarioBodyBytes {
		s.jsonErr(w, http.StatusRequestEntityTooLarge, "scenario body too large")
		return
	}
	sc, err := scenario.ParseString(string(body))
	if err != nil {
		s.jsonErr(w, http.StatusBadRequest, fmt.Sprintf("parse scenario: %v", err))
		return
	}
	sc.BasePath = filepath.Dir(cfg.ScenarioFile)
	if err := s.validateScenario(sc); err != nil {
		s.jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := atomicWriteFile(cfg.ScenarioFile, body); err != nil {
		s.jsonErr(w, http.StatusInternalServerError, fmt.Sprintf("write scenario file: %v", err))
		return
	}
	resp := map[string]any{"written": cfg.ScenarioFile, "applied": false}
	if r.URL.Query().Get("apply") == "1" || r.URL.Query().Get("apply") == "true" {
		if s.cfg.Engine == nil {
			s.jsonErr(w, http.StatusServiceUnavailable, "engine unavailable for apply")
			return
		}
		if err := s.cfg.Engine.TryReplaceLiveScenario(sc); err != nil {
			s.jsonErr(w, http.StatusConflict, err.Error())
			return
		}
		resp["applied"] = true
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.logger.Error("api: encode put response", "error", err)
	}
}

func (s *Server) handleScenarioApply(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Engine == nil {
		s.jsonErr(w, http.StatusServiceUnavailable, "engine unavailable")
		return
	}
	cfg := s.cfg.CLI
	ct := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type")))
	ct = strings.TrimSuffix(strings.Split(ct, ";")[0], " ")

	var sc scenario.Scenario
	var err error
	switch {
	case ct == "application/xml" || ct == "text/xml":
		body, err := io.ReadAll(io.LimitReader(r.Body, maxScenarioBodyBytes+1))
		if err != nil {
			s.jsonErr(w, http.StatusBadRequest, fmt.Sprintf("read body: %v", err))
			return
		}
		if len(body) > maxScenarioBodyBytes {
			s.jsonErr(w, http.StatusRequestEntityTooLarge, "scenario body too large")
			return
		}
		sc, err = scenario.ParseString(string(body))
		if err != nil {
			s.jsonErr(w, http.StatusBadRequest, fmt.Sprintf("parse scenario: %v", err))
			return
		}
		if cfg.ScenarioFile != "" {
			sc.BasePath = filepath.Dir(cfg.ScenarioFile)
		}
	default:
		if cfg.ScenarioFile == "" {
			s.jsonErr(w, http.StatusBadRequest, "apply without body requires -sf (scenario file) or Content-Type: application/xml")
			return
		}
		sc, err = scenario.ParseFile(cfg.ScenarioFile)
		if err != nil {
			s.jsonErr(w, http.StatusBadRequest, fmt.Sprintf("parse scenario file: %v", err))
			return
		}
	}

	if err := s.validateScenario(sc); err != nil {
		s.jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.cfg.Engine.TryReplaceLiveScenario(sc); err != nil {
		s.jsonErr(w, http.StatusConflict, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"applied": true})
}

type controlState struct {
	Rate   float64 `json:"rate"`
	Paused bool    `json:"paused"`
}

func (s *Server) handleControlGet(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Engine == nil {
		s.jsonErr(w, http.StatusServiceUnavailable, "engine unavailable")
		return
	}
	e := s.cfg.Engine
	st := controlState{
		Rate:   e.Rate(),
		Paused: e.Paused(),
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(st)
}

type controlPatch struct {
	Rate   *float64 `json:"rate"`
	Paused *bool    `json:"paused"`
	Pause  *bool    `json:"pause"` // alias for paused when only stop/start semantics desired
	Resume *bool    `json:"resume"`
}

func (s *Server) handleControlPost(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Engine == nil {
		s.jsonErr(w, http.StatusServiceUnavailable, "engine unavailable")
		return
	}
	var body controlPatch
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, fmt.Sprintf("invalid json: %v", err))
		return
	}
	e := s.cfg.Engine
	if body.Rate != nil {
		e.SetRate(*body.Rate)
	}
	if body.Paused != nil {
		if *body.Paused {
			e.Pause()
		} else {
			e.Resume()
		}
	}
	if body.Pause != nil && *body.Pause {
		e.Pause()
	}
	if body.Resume != nil && *body.Resume {
		e.Resume()
	}
	s.handleControlGet(w, r)
}

func atomicWriteFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".gossipper-scenario-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := f.Name()
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

// StartListenAndServe runs the HTTP server until ctx is cancelled, then shuts down gracefully.
func StartListenAndServe(ctx context.Context, addr string, handler http.Handler) error {
	if strings.TrimSpace(addr) == "" {
		return errors.New("api: empty listen address")
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		return nil
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}
