// Package loadtest starts sipstress-style invite_media runs as background supervisor jobs.
package loadtest

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/sipcapture/gossipper/internal/scenario"
	"github.com/sipcapture/gossipper/internal/supervisor"
	"github.com/sipcapture/gossipper/internal/uistore"
)

// WizardProfileID is the ephemeral client profile upserted before each API/UI run.
const WizardProfileID = "_load_wizard"

// InviteMediaScenarios lists built-in UAC scenarios accepted by the load-test API.
var InviteMediaScenarios = []string{
	"invite_media",
	"invite_media_early",
	"invite_media_early_180",
	"invite_media_savpf",
	"invite_media_scale",
}

// Request describes a sipstress-style load run submitted via POST /api/v2/load-test/run.
type Request struct {
	ID                    string  `json:"id,omitempty"`
	Director              string  `json:"director"`
	ScenarioID            string  `json:"scenario_id,omitempty"`
	TotalCalls            int     `json:"total_calls"`
	Rate                  float64 `json:"rate"`
	MaxConcurrent         int     `json:"max_concurrent"`
	RunTimeoutMs          int     `json:"run_timeout_ms,omitempty"`
	SipFrom               string  `json:"sip_from,omitempty"`
	SipPAI                string  `json:"sip_pai,omitempty"`
	SipProvider           string  `json:"sip_provider,omitempty"`
	RecordWAV             bool    `json:"record_wav,omitempty"`
	RecordWAVDuplex       bool    `json:"record_wav_duplex,omitempty"`
	HealthEnabled         bool    `json:"health_enabled,omitempty"`
	HealthMinSuccessRatio float64 `json:"health_min_success_ratio,omitempty"`
	HealthMaxFailedCalls  int     `json:"health_max_failed_calls,omitempty"`
}

// Director is a parsed SBC/director endpoint.
type Director struct {
	Host string
	Port int
}

// ParseDirector parses sipstress-style director strings (host:port, sip:host:port, URL).
func ParseDirector(input string) (Director, error) {
	raw := strings.TrimSpace(input)
	if raw == "" {
		return Director{}, errors.New("director is required")
	}
	if strings.Contains(raw, "://") {
		// URL form handled by caller if needed; keep simple split for sip: without //
	}
	if strings.HasPrefix(raw, "sip:") {
		rest := strings.TrimPrefix(raw, "sip:")
		if at := strings.Index(rest, "@"); at >= 0 {
			rest = rest[at+1:]
		}
		host, port, err := splitHostPort(rest, 5060)
		if err != nil {
			return Director{}, err
		}
		return Director{Host: host, Port: port}, nil
	}
	host, port, err := splitHostPort(raw, 5060)
	if err != nil {
		return Director{}, err
	}
	return Director{Host: host, Port: port}, nil
}

func splitHostPort(raw string, defaultPort int) (string, int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", 0, errors.New("director host is empty")
	}
	if i := strings.LastIndex(raw, ":"); i > 0 && strings.Index(raw, ":") == i {
		host := strings.TrimSpace(raw[:i])
		var port int
		if _, err := fmt.Sscanf(raw[i+1:], "%d", &port); err != nil || port <= 0 {
			return "", 0, fmt.Errorf("invalid director port in %q", raw)
		}
		if host == "" {
			return "", 0, errors.New("director host is empty")
		}
		return host, port, nil
	}
	return raw, defaultPort, nil
}

// Validate checks request fields before starting a background job.
func (req Request) Validate() error {
	if _, err := ParseDirector(req.Director); err != nil {
		return fmt.Errorf("director: %w", err)
	}
	scID := strings.TrimSpace(req.ScenarioID)
	if scID == "" {
		scID = "invite_media"
	}
	if !validScenario(scID) {
		return fmt.Errorf("scenario_id: unknown or unsupported %q", scID)
	}
	if req.TotalCalls < 0 {
		return errors.New("total_calls must be >= 0 (0 = soak until stop)")
	}
	if req.Rate <= 0 {
		return errors.New("rate must be > 0")
	}
	if req.MaxConcurrent <= 0 {
		return errors.New("max_concurrent must be > 0")
	}
	if req.RunTimeoutMs < 0 {
		return errors.New("run_timeout_ms must be >= 0")
	}
	if req.HealthEnabled && req.HealthMinSuccessRatio < 0 {
		return errors.New("health_min_success_ratio must be >= 0")
	}
	if req.HealthMaxFailedCalls < 0 {
		return errors.New("health_max_failed_calls must be >= 0")
	}
	return nil
}

func validScenario(id string) bool {
	for _, s := range InviteMediaScenarios {
		if s == id {
			return true
		}
	}
	_, err := scenario.LoadNamed(id)
	return err == nil
}

// BuildEngine returns worker engine overrides for the request.
func (req Request) BuildEngine(dir Director) map[string]any {
	engine := map[string]any{
		"total_calls":     req.TotalCalls,
		"rate":            req.Rate,
		"max_concurrent":  req.MaxConcurrent,
		"remote_host":     dir.Host,
		"remote_port":     dir.Port,
	}
	if req.RunTimeoutMs > 0 {
		engine["global_timeout_ms"] = req.RunTimeoutMs
	}
	if s := strings.TrimSpace(req.SipFrom); s != "" {
		engine["sip_from"] = s
	}
	if s := strings.TrimSpace(req.SipPAI); s != "" {
		engine["sip_pai"] = s
	}
	if s := strings.TrimSpace(req.SipProvider); s != "" {
		engine["sip_provider"] = s
	}
	if req.HealthEnabled {
		if req.HealthMinSuccessRatio > 0 {
			engine["health_min_success_ratio"] = req.HealthMinSuccessRatio
		}
		engine["health_max_failed_calls"] = req.HealthMaxFailedCalls
	}
	return engine
}

// UpsertWizardProfile writes the shared wizard client profile used by load-test jobs.
func UpsertWizardProfile(store *uistore.Store, req Request, dir Director) error {
	if store == nil {
		return errors.New("loadtest: store is nil")
	}
	scID := strings.TrimSpace(req.ScenarioID)
	if scID == "" {
		scID = "invite_media"
	}
	p := uistore.ClientProfile{
		ID:            WizardProfileID,
		Name:          "Load test (wizard)",
		Description:   "Auto-updated by POST /api/v2/load-test/run before each background job.",
		ScenarioRef:   scID,
		RemoteIP:      dir.Host,
		RemotePort:    dir.Port,
		Rate:          req.Rate,
		MaxConcurrent: req.MaxConcurrent,
		Transports: []uistore.TransportSpec{
			{Transport: "u1", LocalIP: "0.0.0.0", LocalPort: 0, Enabled: true},
		},
	}
	if req.RunTimeoutMs > 0 {
		p.DurationMs = req.RunTimeoutMs
	}
	_, err := store.PutClientProfile(p, true)
	if errors.Is(err, uistore.ErrConflict) {
		_, err = store.PutClientProfile(p, false)
	}
	return err
}

// Start validates the request, upserts the wizard profile, and forks a background worker job.
// The returned job is already status=running (or failed if the worker could not start).
func Start(ctx context.Context, store *uistore.Store, reg *supervisor.Registry, req Request) (supervisor.Job, error) {
	if reg == nil {
		return supervisor.Job{}, errors.New("loadtest: registry is not configured")
	}
	if err := req.Validate(); err != nil {
		return supervisor.Job{}, err
	}
	dir, err := ParseDirector(req.Director)
	if err != nil {
		return supervisor.Job{}, err
	}
	if err := UpsertWizardProfile(store, req, dir); err != nil {
		return supervisor.Job{}, fmt.Errorf("profile: %w", err)
	}
	jobID := strings.TrimSpace(req.ID)
	if jobID == "" {
		jobID = uuid.NewString()
	}
	scID := strings.TrimSpace(req.ScenarioID)
	if scID == "" {
		scID = "invite_media"
	}
	artifactsDir, err := store.Layout().JobArtifactDir(jobID)
	if err != nil {
		return supervisor.Job{}, err
	}
	engine := req.BuildEngine(dir)
	argsJSON, err := supervisor.EncodeArgs(engine)
	if err != nil {
		return supervisor.Job{}, err
	}
	job := supervisor.Job{
		ID:           jobID,
		ProfileID:    WizardProfileID,
		ProfileKind:  string(uistore.KindClient),
		ScenarioID:   scID,
		Status:       supervisor.StatusPending,
		ArgsJSON:     argsJSON,
		ArtifactsDir: artifactsDir,
		CreatedAt:    time.Now().UTC(),
	}
	spec := supervisor.Spec{
		JobID:           jobID,
		DataDir:         store.Layout().Root,
		ProfileID:       WizardProfileID,
		ProfileKind:     string(uistore.KindClient),
		ScenarioID:      scID,
		ArtifactsDir:    artifactsDir,
		RecordWAV:       req.RecordWAV,
		RecordWAVDuplex: req.RecordWAV && req.RecordWAVDuplex,
		Engine:          engine,
	}
	return reg.StartJob(ctx, job, spec)
}
