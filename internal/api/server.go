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

	apiv2 "github.com/sipcapture/gossipper/internal/api/v2"
	"github.com/sipcapture/gossipper/internal/cli"
	"github.com/sipcapture/gossipper/internal/engine"
	"github.com/sipcapture/gossipper/internal/scenario"
	"github.com/sipcapture/gossipper/internal/settingsauth"
	"github.com/sipcapture/gossipper/internal/stats"
	"github.com/sipcapture/gossipper/internal/supervisor"
	"github.com/sipcapture/gossipper/internal/uistore"
)

const maxScenarioBodyBytes = 16 << 20
const maxClientsPostBody = 1 << 20

// ServerConfig configures the gossipper management HTTP API.
type ServerConfig struct {
	Engine *engine.Engine
	// ExtraEngines are additional SIP engines (composite server -config clients). Stats aggregate includes them;
	// scenario apply still targets Engine only; GET/POST /transports includes listener slots on Engine and client rows for all client engines.
	ExtraEngines []*engine.Engine
	ExtraIDs     []string // same length as ExtraEngines; stable profile ids for stats/control
	// LiveExtras returns dynamically added load engines (POST /api/v1/clients), appended after ExtraEngines for stats/control.
	LiveExtras func() ([]*engine.Engine, []string)
	// AddLoadClient starts a new UAC engine from a JSON snippet; nil disables POST /api/v1/clients.
	AddLoadClient func(ctx context.Context, wantID string, body []byte) (assignedID string, err error)
	// RemoveLoadClient stops a dynamic client started via AddLoadClient; nil disables DELETE /api/v1/clients.
	RemoveLoadClient func(ctx context.Context, id string) error
	// StatsPrimaryID is the JSON "id" of the server profile (composite mode); empty => "primary" in API responses.
	StatsPrimaryID string
	CLI            cli.Config
	Token          string // optional; if set, require Authorization: Bearer <Token>
	// ValidateScenario checks a parsed scenario against current CLI flags (transport, 3PCC, etc.).
	// If nil, PUT/apply skip this check (tests only; production should always set it).
	ValidateScenario func(scenario.Scenario) error
	Logger           *slog.Logger
	// SettingsAuth enables internal SQLite user auth + JWT for the API (when non-nil and Enabled).
	SettingsAuth *settingsauth.Auth
	// UIStore + JobsRegistry, when both non-nil, expose the admin-console
	// REST surface (/api/v2/*) on the same mux as legacy /api/v1/*.
	// Wired by launcher when cli.Config.UIDataDir is set; nil keeps the
	// management API legacy-only.
	UIStore      *uistore.Store
	JobsRegistry *supervisor.Registry
	// Version is reported by /api/v2/health when v2 is enabled.
	Version string
	// EnableLegacyV1 controls whether the legacy /api/v1/* routes are
	// registered. Defaults to true for back-compat; admin-console-only
	// deployments should set this to false to keep only /api/v2/* on the
	// management surface.
	EnableLegacyV1 bool
}

// Server exposes REST-style endpoints under /api/v1/.
type Server struct {
	cfg    ServerConfig
	logger *slog.Logger
}

// New returns an API server wrapper (handlers are registered on construction).
//
// Back-compat: callers that build a ServerConfig with zero value for
// EnableLegacyV1 *and* no admin-console wiring (UIStore + JobsRegistry both
// nil) opt into the historical behaviour where /api/v1/* is served. The
// launcher always sets EnableLegacyV1 explicitly so this only affects ad-hoc
// constructors (tests, external embedders).
func New(cfg ServerConfig) *Server {
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	if !cfg.EnableLegacyV1 && cfg.UIStore == nil && cfg.JobsRegistry == nil {
		cfg.EnableLegacyV1 = true
	}
	return &Server{cfg: cfg, logger: log}
}

// Handler returns the root HTTP handler (mux with routes).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	if s.cfg.EnableLegacyV1 {
		mux.HandleFunc("GET /api/v1/auth/status", s.handleAuthStatus)
		mux.HandleFunc("POST /api/v1/auth/login", s.handleAuthLogin)
		mux.HandleFunc("GET /api/v1/health", s.wrap(s.handleHealth))
		mux.HandleFunc("GET /api/v1/stats", s.wrap(s.handleStats))
		mux.HandleFunc("GET /api/v1/scenario", s.wrap(s.handleScenarioGet))
		mux.HandleFunc("PUT /api/v1/scenario", s.wrap(s.handleScenarioPut))
		mux.HandleFunc("POST /api/v1/scenario/apply", s.wrap(s.handleScenarioApply))
		mux.HandleFunc("GET /api/v1/control", s.wrap(s.handleControlGet))
		mux.HandleFunc("POST /api/v1/control", s.wrap(s.handleControlPost))
		mux.HandleFunc("GET /api/v1/transports", s.wrap(s.handleTransportsGet))
		mux.HandleFunc("POST /api/v1/transports", s.wrap(s.handleTransportsPost))
		mux.HandleFunc("GET /api/v1/clients", s.wrap(s.handleClientsGet))
		mux.HandleFunc("POST /api/v1/clients", s.wrap(s.handleClientsPost))
		mux.HandleFunc("DELETE /api/v1/clients", s.wrap(s.handleClientsDelete))
		mux.HandleFunc("GET /api/v1/live", s.handleLiveWS)
	}
	if s.cfg.UIStore != nil && s.cfg.JobsRegistry != nil {
		apiv2.New(apiv2.Config{
			Store:    s.cfg.UIStore,
			Registry: s.cfg.JobsRegistry,
			Auth:     s.cfg.SettingsAuth,
			Version:  s.cfg.Version,
		}).Register(mux)
	}
	registerEmbeddedControlUI(mux)
	return mux
}

// V2Enabled reports whether the /api/v2/* admin-console surface is mounted on
// this server (i.e. ServerConfig.UIStore and JobsRegistry are both set).
func (s *Server) V2Enabled() bool {
	return s.cfg.UIStore != nil && s.cfg.JobsRegistry != nil
}

// V1Enabled reports whether the legacy /api/v1/* routes are registered.
func (s *Server) V1Enabled() bool { return s.cfg.EnableLegacyV1 }

func (s *Server) checkLegacyAPIToken(r *http.Request) bool {
	if s.cfg.Token == "" {
		return true
	}
	want := "Bearer " + s.cfg.Token
	got := r.Header.Get("Authorization")
	if len(got) == len(want) && subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1 {
		return true
	}
	q := strings.TrimSpace(r.URL.Query().Get("token"))
	if q != "" && len(q) == len(s.cfg.Token) && subtle.ConstantTimeCompare([]byte(q), []byte(s.cfg.Token)) == 1 {
		return true
	}
	return false
}

func (s *Server) authorizeRequest(r *http.Request) bool {
	if s.cfg.SettingsAuth != nil && s.cfg.SettingsAuth.Enabled() {
		return s.cfg.SettingsAuth.ValidRequest(r)
	}
	return s.checkLegacyAPIToken(r)
}

func (s *Server) wrap(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.authorizeRequest(r) {
			s.jsonErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next(w, r)
	}
}

func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if s.cfg.SettingsAuth != nil && s.cfg.SettingsAuth.Enabled() {
		_ = json.NewEncoder(w).Encode(map[string]any{"auth": "internal"})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"auth": "none"})
}

type authLoginBody struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if s.cfg.SettingsAuth == nil || !s.cfg.SettingsAuth.Enabled() {
		s.jsonErr(w, http.StatusNotFound, "internal auth is not enabled")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	if err != nil {
		s.jsonErr(w, http.StatusBadRequest, fmt.Sprintf("read body: %v", err))
		return
	}
	var in authLoginBody
	if err := json.Unmarshal(body, &in); err != nil {
		s.jsonErr(w, http.StatusBadRequest, fmt.Sprintf("invalid json: %v", err))
		return
	}
	tok, exp, err := s.cfg.SettingsAuth.Login(r.Context(), in.Username, in.Password)
	if err != nil {
		s.jsonErr(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"token":      tok,
		"expires_at": exp,
		"token_type": "Bearer",
	})
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

func (s *Server) mergedExtras() ([]*engine.Engine, []string) {
	var engines []*engine.Engine
	var ids []string
	for i, e := range s.cfg.ExtraEngines {
		if e == nil {
			continue
		}
		engines = append(engines, e)
		id := ""
		if i < len(s.cfg.ExtraIDs) {
			id = strings.TrimSpace(s.cfg.ExtraIDs[i])
		}
		if id == "" {
			id = fmt.Sprintf("extra-%d", len(ids))
		}
		ids = append(ids, id)
	}
	if s.cfg.LiveExtras != nil {
		e2, i2 := s.cfg.LiveExtras()
		for i, e := range e2 {
			if e == nil {
				continue
			}
			engines = append(engines, e)
			id := ""
			if i < len(i2) {
				id = strings.TrimSpace(i2[i])
			}
			if id == "" {
				id = fmt.Sprintf("dyn-%d", len(ids))
			}
			ids = append(ids, id)
		}
	}
	for len(ids) < len(engines) {
		ids = append(ids, fmt.Sprintf("extra-%d", len(ids)))
	}
	return engines, ids
}

func (s *Server) statsPrimaryID() string {
	if pid := strings.TrimSpace(s.cfg.StatsPrimaryID); pid != "" {
		return pid
	}
	return "primary"
}

func (s *Server) collectClientTransportSummaries() []engine.ClientTransportSummary {
	out := make([]engine.ClientTransportSummary, 0, 1+len(s.cfg.ExtraEngines))
	pid := s.statsPrimaryID()
	if sum, ok := s.cfg.Engine.ClientTransportSummary(pid); ok {
		out = append(out, sum)
	}
	extras, ids := s.mergedExtras()
	for i, e := range extras {
		if e == nil {
			continue
		}
		if sum, ok := e.ClientTransportSummary(ids[i]); ok {
			out = append(out, sum)
		}
	}
	return out
}

// markDynamicClientRows sets Dynamic=true for engines returned by LiveExtras (POST /clients).
func (s *Server) markDynamicClientRows(rows []engine.ClientTransportSummary) {
	if s.cfg.LiveExtras == nil {
		return
	}
	_, dynIDs := s.cfg.LiveExtras()
	dyn := make(map[string]struct{}, len(dynIDs))
	for _, id := range dynIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			dyn[id] = struct{}{}
		}
	}
	for i := range rows {
		if _, ok := dyn[rows[i].ID]; ok {
			rows[i].Dynamic = true
		}
	}
}

func (s *Server) buildTransportsGetResponse() transportsGetResponse {
	clients := s.collectClientTransportSummaries()
	s.markDynamicClientRows(clients)
	out := transportsGetResponse{
		Listeners: s.cfg.Engine.TransportListenerStates(),
		Clients:   clients,
	}
	out.DynamicClientAPI.CanPost = s.cfg.AddLoadClient != nil
	out.DynamicClientAPI.CanDelete = s.cfg.RemoveLoadClient != nil
	return out
}

func (s *Server) engineForClientTransportID(id string) (*engine.Engine, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, errors.New("empty client id")
	}
	pid := s.statsPrimaryID()
	if id == pid {
		return s.cfg.Engine, nil
	}
	extras, ids := s.mergedExtras()
	for i, e := range extras {
		if e == nil {
			continue
		}
		if ids[i] == id {
			return e, nil
		}
	}
	return nil, fmt.Errorf("unknown client id %q", id)
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
	extras, extraIDs := s.mergedExtras()
	if len(extras) > 0 {
		type row struct {
			ID    string        `json:"id"`
			Stats stats.Summary `json:"stats"`
		}
		rows := []row{{ID: "primary", Stats: s.cfg.Engine.Stats().Snapshot()}}
		if pid := strings.TrimSpace(s.cfg.StatsPrimaryID); pid != "" {
			rows[0].ID = pid
		}
		for i, e := range extras {
			if e == nil {
				continue
			}
			id := extraIDs[i]
			rows = append(rows, row{ID: id, Stats: e.Stats().Snapshot()})
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		out := map[string]any{"multi": true, "engines": rows}
		if s.cfg.LiveExtras != nil {
			_, dynIDs := s.cfg.LiveExtras()
			out["dynamic_client_ids"] = dynIDs
		}
		_ = json.NewEncoder(w).Encode(out)
		return
	}
	snap := s.cfg.Engine.Stats().Snapshot()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if s.cfg.LiveExtras != nil {
		_, dynIDs := s.cfg.LiveExtras()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"multi":              false,
			"stats":              snap,
			"dynamic_client_ids": dynIDs,
		})
		return
	}
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
			s.jsonErr(w, http.StatusBadRequest, "POST /scenario/apply without XML body: set Content-Type: application/xml and send scenario XML, or start gossipper with -sf so the on-disk file can be re-read (built-in -sn scenarios have no file path)")
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
	extras, extraIDs := s.mergedExtras()
	if len(extras) > 0 {
		type row struct {
			ID     string  `json:"id"`
			Rate   float64 `json:"rate"`
			Paused bool    `json:"paused"`
		}
		rows := []row{{ID: "primary", Rate: s.cfg.Engine.Rate(), Paused: s.cfg.Engine.Paused()}}
		if pid := strings.TrimSpace(s.cfg.StatsPrimaryID); pid != "" {
			rows[0].ID = pid
		}
		for i, e := range extras {
			if e == nil {
				continue
			}
			id := extraIDs[i]
			rows = append(rows, row{ID: id, Rate: e.Rate(), Paused: e.Paused()})
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{"multi": true, "engines": rows})
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
	apply := func(e *engine.Engine) {
		if e == nil {
			return
		}
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
	}
	apply(s.cfg.Engine)
	extras, _ := s.mergedExtras()
	for _, e := range extras {
		apply(e)
	}
	s.handleControlGet(w, r)
}

func (s *Server) handleClientsGet(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Engine == nil {
		s.jsonErr(w, http.StatusServiceUnavailable, "engine unavailable")
		return
	}
	clients := s.collectClientTransportSummaries()
	s.markDynamicClientRows(clients)
	out := map[string]any{
		"engines": clients,
		"dynamic_client_api": map[string]bool{
			"can_post":   s.cfg.AddLoadClient != nil,
			"can_delete": s.cfg.RemoveLoadClient != nil,
		},
	}
	if s.cfg.LiveExtras != nil {
		_, ids := s.cfg.LiveExtras()
		out["dynamic"] = ids
	} else {
		out["dynamic"] = []string{}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) handleClientsDelete(w http.ResponseWriter, r *http.Request) {
	if s.cfg.RemoveLoadClient == nil {
		s.jsonErr(w, http.StatusNotFound, "dynamic client removal is not enabled for this process")
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		s.jsonErr(w, http.StatusBadRequest, "query parameter id is required")
		return
	}
	if err := s.cfg.RemoveLoadClient(r.Context(), id); err != nil {
		s.jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleClientsPost(w http.ResponseWriter, r *http.Request) {
	if s.cfg.AddLoadClient == nil {
		s.jsonErr(w, http.StatusNotFound, "dynamic clients are not enabled for this process")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxClientsPostBody+1))
	if err != nil {
		s.jsonErr(w, http.StatusBadRequest, fmt.Sprintf("read body: %v", err))
		return
	}
	if len(body) > maxClientsPostBody {
		s.jsonErr(w, http.StatusRequestEntityTooLarge, "client snippet too large")
		return
	}
	wantID := strings.TrimSpace(r.URL.Query().Get("id"))
	if wantID == "" {
		var meta struct {
			ID *string `json:"id"`
		}
		if err := json.Unmarshal(body, &meta); err == nil && meta.ID != nil {
			wantID = strings.TrimSpace(*meta.ID)
		}
	}
	id, err := s.cfg.AddLoadClient(r.Context(), wantID, body)
	if err != nil {
		s.jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "started": true})
}

type transportsGetResponse struct {
	Listeners        []engine.TransportListenerState `json:"listeners"`
	Clients          []engine.ClientTransportSummary `json:"clients"`
	DynamicClientAPI struct {
		CanPost   bool `json:"can_post"`
		CanDelete bool `json:"can_delete"`
	} `json:"dynamic_client_api"`
}

func (s *Server) handleTransportsGet(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Engine == nil {
		s.jsonErr(w, http.StatusServiceUnavailable, "engine unavailable")
		return
	}
	out := s.buildTransportsGetResponse()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(out); err != nil {
		s.logger.Error("api: encode transports", "error", err)
	}
}

type transportToggle struct {
	Index   int  `json:"index"`
	Enabled bool `json:"enabled"`
}

type clientTransportToggle struct {
	ID        string `json:"id"`
	Accepting bool   `json:"accepting"`
}

type transportsPostBody struct {
	Listeners []transportToggle `json:"listeners,omitempty"`
	Clients   []clientTransportToggle `json:"clients,omitempty"`
	// Single-toggle shorthand when listeners is omitted: {"index":0,"enabled":false}
	Index   *int  `json:"index,omitempty"`
	Enabled *bool `json:"enabled,omitempty"`
}

func (s *Server) handleTransportsPost(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Engine == nil {
		s.jsonErr(w, http.StatusServiceUnavailable, "engine unavailable")
		return
	}
	var body transportsPostBody
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, fmt.Sprintf("invalid json: %v", err))
		return
	}
	var listenerOps []transportToggle
	if len(body.Listeners) > 0 {
		listenerOps = append(listenerOps, body.Listeners...)
	} else if body.Index != nil && body.Enabled != nil {
		listenerOps = append(listenerOps, transportToggle{Index: *body.Index, Enabled: *body.Enabled})
	}
	if len(listenerOps) == 0 && len(body.Clients) == 0 {
		s.jsonErr(w, http.StatusBadRequest, "provide listeners[] or index+enabled, and/or clients[{id,accepting}]")
		return
	}
	e := s.cfg.Engine
	for _, op := range listenerOps {
		if err := e.SetTransportListenerEnabled(op.Index, op.Enabled); err != nil {
			s.jsonErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	for _, c := range body.Clients {
		eng, err := s.engineForClientTransportID(c.ID)
		if err != nil {
			s.jsonErr(w, http.StatusBadRequest, err.Error())
			return
		}
		if _, ok := eng.ClientTransportSummary(c.ID); !ok {
			s.jsonErr(w, http.StatusBadRequest, fmt.Sprintf("engine %q is not a SIP client (UAC)", strings.TrimSpace(c.ID)))
			return
		}
		if c.Accepting {
			eng.Resume()
		} else {
			eng.Pause()
		}
	}
	s.handleTransportsGet(w, r)
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
