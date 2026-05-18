// Package v2 implements the new UI-facing HTTP API for `gossipper ui`.
//
// Unlike internal/api (the live engine control plane), v2 is decoupled from a
// running SIP engine: it exposes CRUD over profiles / scenarios / media stored
// in uistore.Store and start/stop over jobs persisted by supervisor.JobsStore.
//
// All routes live under /api/v2. The handler is mountable on any
// http.ServeMux (e.g. behind the embedded Control UI at /).
package v2

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/sipcapture/gossipper/internal/settingsauth"
	"github.com/sipcapture/gossipper/internal/supervisor"
	"github.com/sipcapture/gossipper/internal/uistore"
)

// Config wires v2 dependencies. Store, Registry, Auth are required to enable
// the corresponding endpoints; nil values yield 503 responses.
type Config struct {
	Store    *uistore.Store
	Registry *supervisor.Registry
	Auth     *settingsauth.Auth
	Logger   *slog.Logger
	// Version is reported in /api/v2/health.
	Version string
}

// Server registers v2 handlers on a given mux.
type Server struct {
	cfg Config
	log *slog.Logger
}

// New constructs a Server.
func New(cfg Config) *Server {
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Server{cfg: cfg, log: log}
}

// Register attaches the v2 routes to mux.
func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v2/health", s.handleHealth)
	mux.HandleFunc("GET /api/v2/auth/status", s.handleAuthStatus)
	mux.HandleFunc("POST /api/v2/auth/login", s.handleAuthLogin)
	mux.HandleFunc("GET /api/v2/me", s.auth(s.handleMe))

	mux.HandleFunc("GET /api/v2/servers", s.auth(s.handleListServerProfiles))
	mux.HandleFunc("POST /api/v2/servers", s.auth(s.handleCreateServerProfile))
	mux.HandleFunc("GET /api/v2/servers/{id}", s.auth(s.handleGetServerProfile))
	mux.HandleFunc("PUT /api/v2/servers/{id}", s.auth(s.handleUpdateServerProfile))
	mux.HandleFunc("DELETE /api/v2/servers/{id}", s.auth(s.handleDeleteServerProfile))

	mux.HandleFunc("GET /api/v2/clients", s.auth(s.handleListClientProfiles))
	mux.HandleFunc("POST /api/v2/clients", s.auth(s.handleCreateClientProfile))
	mux.HandleFunc("GET /api/v2/clients/{id}", s.auth(s.handleGetClientProfile))
	mux.HandleFunc("PUT /api/v2/clients/{id}", s.auth(s.handleUpdateClientProfile))
	mux.HandleFunc("DELETE /api/v2/clients/{id}", s.auth(s.handleDeleteClientProfile))

	mux.HandleFunc("GET /api/v2/scenarios", s.auth(s.handleListScenarios))
	mux.HandleFunc("GET /api/v2/builtin-scenarios", s.auth(s.handleListBuiltinScenarios))
	mux.HandleFunc("GET /api/v2/builtin-scenarios/{id}", s.auth(s.handleGetBuiltinScenario))
	mux.HandleFunc("POST /api/v2/scenarios", s.auth(s.handleCreateScenario))
	mux.HandleFunc("GET /api/v2/scenarios/{id}", s.auth(s.handleGetScenario))
	mux.HandleFunc("PUT /api/v2/scenarios/{id}", s.auth(s.handleUpdateScenario))
	mux.HandleFunc("DELETE /api/v2/scenarios/{id}", s.auth(s.handleDeleteScenario))
	mux.HandleFunc("GET /api/v2/scenarios/{id}/history", s.auth(s.handleListScenarioHistory))
	mux.HandleFunc("GET /api/v2/scenarios/{id}/history/{ts}", s.auth(s.handleGetScenarioHistory))
	mux.HandleFunc("DELETE /api/v2/scenarios/{id}/history/{ts}", s.auth(s.handleDeleteScenarioHistory))
	mux.HandleFunc("POST /api/v2/scenarios/{id}/history/{ts}/fork", s.auth(s.handleForkScenarioHistory))

	mux.HandleFunc("GET /api/v2/media/{kind}", s.auth(s.handleListMedia))
	mux.HandleFunc("POST /api/v2/media/{kind}/{name}", s.auth(s.handleUploadMedia))
	mux.HandleFunc("GET /api/v2/media/{kind}/{name}", s.auth(s.handleDownloadMedia))
	mux.HandleFunc("DELETE /api/v2/media/{kind}/{name}", s.auth(s.handleDeleteMedia))

	mux.HandleFunc("GET /api/v2/jobs", s.auth(s.handleListJobs))
	mux.HandleFunc("GET /api/v2/jobs/{id}", s.auth(s.handleGetJob))
	mux.HandleFunc("DELETE /api/v2/jobs/{id}", s.auth(s.handleDeleteJob))
	mux.HandleFunc("POST /api/v2/jobs/{id}/stop", s.auth(s.handleStopJob))
	mux.HandleFunc("POST /api/v2/jobs", s.auth(s.handleStartJob))
	mux.HandleFunc("GET /api/v2/jobs/{id}/recordings", s.auth(s.handleListRecordings))
	mux.HandleFunc("GET /api/v2/jobs/{id}/recordings/{name}", s.auth(s.handleDownloadRecording))
	mux.HandleFunc("GET /api/v2/jobs/{id}/events", s.auth(s.handleJobEvents))
	mux.HandleFunc("POST /api/v2/servers/{id}/start", s.auth(s.handleStartServerShortcut))
	mux.HandleFunc("POST /api/v2/servers/{id}/stop", s.auth(s.handleStopServerShortcut))
	mux.HandleFunc("POST /api/v2/clients/{id}/start", s.auth(func(w http.ResponseWriter, r *http.Request) {
		s.handleStartProfileShortcut(w, r, "client")
	}))
	mux.HandleFunc("POST /api/v2/clients/{id}/stop", s.auth(func(w http.ResponseWriter, r *http.Request) {
		s.handleStopProfileShortcut(w, r, "client")
	}))

	mux.HandleFunc("GET /api/v2/live", s.handleLive)

	mux.HandleFunc("GET /api/v2/users", s.auth(s.handleListUsers))
	mux.HandleFunc("POST /api/v2/users", s.auth(s.handleCreateUser))
	mux.HandleFunc("PUT /api/v2/users/{id}", s.auth(s.handleUpdateUser))
	mux.HandleFunc("DELETE /api/v2/users/{id}", s.auth(s.handleDeleteUser))
	mux.HandleFunc("GET /api/v2/audit", s.auth(s.handleListAudit))

	mux.HandleFunc("GET /api/v2/settings", s.auth(s.handleGetSettings))
	mux.HandleFunc("POST /api/v2/settings/rotate-jwt-secret", s.auth(s.handleRotateJWTSecret))
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.Auth != nil && s.cfg.Auth.Enabled() {
			if !s.cfg.Auth.ValidRequest(r) {
				s.writeError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
		}
		next(w, r)
	}
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if body == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		s.log.Error("v2: write response", "err", err)
	}
}

func (s *Server) writeError(w http.ResponseWriter, status int, msg string) {
	s.writeJSON(w, status, map[string]string{"error": msg})
}

func (s *Server) decodeJSON(r *http.Request, v any, maxBytes int64) error {
	if maxBytes <= 0 {
		maxBytes = 1 << 20
	}
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	return nil
}

func mapStoreError(err error) (int, string) {
	switch {
	case err == nil:
		return http.StatusOK, ""
	case errors.Is(err, uistore.ErrNotFound), errors.Is(err, supervisor.ErrNotFound):
		return http.StatusNotFound, "not found"
	case errors.Is(err, uistore.ErrConflict):
		return http.StatusConflict, "id already exists"
	case errors.Is(err, uistore.ErrInvalidID),
		errors.Is(err, uistore.ErrInvalidHistoryTS):
		return http.StatusBadRequest, err.Error()
	default:
		return http.StatusInternalServerError, err.Error()
	}
}

func (s *Server) requireStore(w http.ResponseWriter) bool {
	if s.cfg.Store == nil {
		s.writeError(w, http.StatusServiceUnavailable, "uistore not configured")
		return false
	}
	return true
}

func (s *Server) requireRegistry(w http.ResponseWriter) bool {
	if s.cfg.Registry == nil {
		s.writeError(w, http.StatusServiceUnavailable, "supervisor not configured")
		return false
	}
	return true
}

func pathID(r *http.Request) string {
	return strings.TrimSpace(r.PathValue("id"))
}
