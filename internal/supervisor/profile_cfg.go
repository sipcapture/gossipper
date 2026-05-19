package supervisor

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sipcapture/gossipper/internal/cli"
	"github.com/sipcapture/gossipper/internal/scenario"
	"github.com/sipcapture/gossipper/internal/uistore"
)

// BuildConfigFromSpec resolves a worker Spec into a runnable cli.Config.
// It opens the uistore at Spec.DataDir, fetches the relevant profile, optional
// scenario override, materialises the scenario XML into a temp file so existing
// launcher code can pick it up via cfg.ScenarioFile, then merges Spec.Engine
// overrides on top.
//
// Returns the assembled cli.Config plus a cleanup func the caller should defer
// (removes the temp scenario file).
func BuildConfigFromSpec(spec Spec) (cli.Config, func(), error) {
	if spec.DataDir == "" {
		return cli.Config{}, func() {}, fmt.Errorf("supervisor: spec.data_dir is required")
	}
	store, err := uistore.Open(spec.DataDir)
	if err != nil {
		return cli.Config{}, func() {}, err
	}
	scenarioID := strings.TrimSpace(spec.ScenarioID)

	cfg := cli.DefaultConfig()
	cleanup := func() {}

	switch strings.ToLower(strings.TrimSpace(spec.ProfileKind)) {
	case string(uistore.KindServer):
		p, err := store.GetServerProfile(spec.ProfileID)
		if err != nil {
			return cli.Config{}, cleanup, fmt.Errorf("server profile %q: %w", spec.ProfileID, err)
		}
		if scenarioID == "" {
			scenarioID = p.ScenarioRef
		}
		applyServerProfileToConfig(&cfg, p)
	case string(uistore.KindClient):
		p, err := store.GetClientProfile(spec.ProfileID)
		if err != nil {
			return cli.Config{}, cleanup, fmt.Errorf("client profile %q: %w", spec.ProfileID, err)
		}
		if scenarioID == "" {
			scenarioID = p.ScenarioRef
		}
		applyClientProfileToConfig(&cfg, p)
	default:
		return cli.Config{}, cleanup, fmt.Errorf("supervisor: unsupported profile_kind %q", spec.ProfileKind)
	}

	if scenarioID != "" {
		body, err := store.GetScenario(scenarioID)
		switch {
		case err == nil:
			// materialise the XML below.
		case errors.Is(err, uistore.ErrNotFound):
			// Fallback to engine-baked scenarios ("uac", "uas", "management",
			// "invite_media*", …). These ship in scenario.LoadNamed and don't
			// need to live on disk in uistore; profiles can reference them by
			// name (typical for built-in seeded profiles).
			if _, lerr := scenario.LoadNamed(scenarioID); lerr != nil {
				return cli.Config{}, cleanup, fmt.Errorf("scenario %q: %w", scenarioID, err)
			}
			cfg.ScenarioName = scenarioID
			body = uistore.ScenarioBody{}
		default:
			return cli.Config{}, cleanup, fmt.Errorf("scenario %q: %w", scenarioID, err)
		}
		if strings.TrimSpace(body.XML) != "" {
			// Expand [[media:wav/<id>]] / [[media:pcap/<id>]] aliases to
			// absolute on-disk paths so SIPp-style play_pcap_audio /
			// play_pcap_image work without scenario authors having to know
			// the data-dir layout.
			processed, mediaErrs := store.PreprocessScenarioXML(body.XML)
			if len(mediaErrs) > 0 {
				return cli.Config{}, cleanup, fmt.Errorf("scenario %q media refs: %v", scenarioID, mediaErrs)
			}
			tmpDir := filepath.Join(spec.DataDir, "tmp")
			if err := os.MkdirAll(tmpDir, 0o750); err != nil {
				return cli.Config{}, cleanup, fmt.Errorf("scenario tmp dir: %w", err)
			}
			f, err := os.CreateTemp(tmpDir, "scenario-*.xml")
			if err != nil {
				return cli.Config{}, cleanup, fmt.Errorf("scenario tmp file: %w", err)
			}
			path := f.Name()
			if _, err := f.WriteString(processed); err != nil {
				_ = f.Close()
				_ = os.Remove(path)
				return cli.Config{}, cleanup, err
			}
			if err := f.Close(); err != nil {
				_ = os.Remove(path)
				return cli.Config{}, cleanup, err
			}
			cfg.ScenarioFile = path
			cleanup = func() { _ = os.Remove(path) }
		}
	}

	if strings.TrimSpace(cfg.SummaryJSON) == "" && strings.TrimSpace(spec.ArtifactsDir) != "" {
		cfg.SummaryJSON = filepath.Join(spec.ArtifactsDir, "summary.json")
	}
	if strings.TrimSpace(cfg.SummaryHTML) == "" && strings.TrimSpace(spec.ArtifactsDir) != "" {
		cfg.SummaryHTML = filepath.Join(spec.ArtifactsDir, "report.html")
	}
	if strings.TrimSpace(cfg.CallRecordsJSONL) == "" && strings.TrimSpace(spec.ArtifactsDir) != "" {
		cfg.CallRecordsJSONL = filepath.Join(spec.ArtifactsDir, "call_records.jsonl")
	}

	if spec.RecordWAV && strings.TrimSpace(spec.ArtifactsDir) != "" {
		recDir := filepath.Join(spec.ArtifactsDir, "recordings")
		if err := os.MkdirAll(recDir, 0o750); err != nil {
			return cli.Config{}, cleanup, fmt.Errorf("recordings dir: %w", err)
		}
		cfg.RecordWAVDir = recDir
		cfg.RecordWAVDuplex = spec.RecordWAVDuplex
	}

	if len(spec.Engine) > 0 {
		raw, err := json.Marshal(spec.Engine)
		if err != nil {
			return cli.Config{}, cleanup, err
		}
		if err := overlayEngineJSON(&cfg, raw); err != nil {
			return cli.Config{}, cleanup, err
		}
	}
	return cfg, cleanup, nil
}

func applyServerProfileToConfig(cfg *cli.Config, p uistore.ServerProfile) {
	cfg.ServerMode = true
	if p.MaxConcurrent > 0 {
		cfg.MaxConcurrent = p.MaxConcurrent
	}
	enabled := enabledTransports(p.Transports)
	// Split: SIP transports (anything except webrtc) drive listener config;
	// webrtc rows only contribute ICE / codec settings.
	sip, webrtcOnly := splitWebRTC(enabled)
	for _, t := range webrtcOnly {
		applyWebRTCToConfig(cfg, t)
	}
	if len(sip) > 0 {
		first := sip[0]
		if first.Transport != "" {
			cfg.Transport = first.Transport
		}
		if first.LocalIP != "" {
			cfg.LocalIP = first.LocalIP
		}
		if first.LocalPort != 0 {
			cfg.LocalPort = first.LocalPort
		}
		if first.TLSCertFile != "" {
			cfg.TLSCertFile = first.TLSCertFile
		}
		if first.TLSKeyFile != "" {
			cfg.TLSKeyFile = first.TLSKeyFile
		}
		if first.WSPath != "" {
			cfg.WSPath = first.WSPath
		}
		applyWebRTCToConfig(cfg, first)
		if len(sip) > 1 {
			cfg.ServerListeners = nil
			for _, t := range sip {
				cfg.ServerListeners = append(cfg.ServerListeners, cli.ServerListener{
					Transport: t.Transport,
					LocalIP:   t.LocalIP,
					LocalPort: t.LocalPort,
				})
			}
		}
	}
}

func applyClientProfileToConfig(cfg *cli.Config, p uistore.ClientProfile) {
	cfg.ServerMode = false
	if p.RemoteIP != "" {
		cfg.RemoteHost = p.RemoteIP
	}
	if p.RemotePort > 0 {
		cfg.RemotePort = p.RemotePort
	}
	if p.Rate > 0 {
		cfg.Rate = p.Rate
	}
	if p.MaxConcurrent > 0 {
		cfg.MaxConcurrent = p.MaxConcurrent
	}
	if p.DurationMs > 0 {
		cfg.GlobalTimeout = time.Duration(p.DurationMs) * time.Millisecond
	}
	enabled := enabledTransports(p.Transports)
	sip, webrtcOnly := splitWebRTC(enabled)
	for _, t := range webrtcOnly {
		applyWebRTCToConfig(cfg, t)
	}
	if len(sip) > 0 {
		first := sip[0]
		if first.Transport != "" {
			cfg.Transport = first.Transport
		}
		if first.LocalIP != "" {
			cfg.LocalIP = first.LocalIP
		}
		if first.LocalPort != 0 {
			cfg.LocalPort = first.LocalPort
		}
		if first.TLSCertFile != "" {
			cfg.TLSCertFile = first.TLSCertFile
		}
		if first.TLSKeyFile != "" {
			cfg.TLSKeyFile = first.TLSKeyFile
		}
		if first.WSPath != "" {
			cfg.WSPath = first.WSPath
		}
		applyWebRTCToConfig(cfg, first)
	}
}

// splitWebRTC returns (sipTransports, webrtcTransports) preserving order.
func splitWebRTC(in []uistore.TransportSpec) ([]uistore.TransportSpec, []uistore.TransportSpec) {
	sip := make([]uistore.TransportSpec, 0, len(in))
	webrtcOnly := make([]uistore.TransportSpec, 0)
	for _, t := range in {
		if t.Transport == "webrtc" {
			webrtcOnly = append(webrtcOnly, t)
			continue
		}
		sip = append(sip, t)
	}
	return sip, webrtcOnly
}

// applyWebRTCToConfig copies ICE / codec preferences from a uistore transport
// row into cli.Config. Idempotent — later calls only fill empty slots, so the
// first enabled transport wins for shared fields.
func applyWebRTCToConfig(cfg *cli.Config, t uistore.TransportSpec) {
	if t.Transport == "webrtc" {
		cfg.WebRTCMedia = true
	}
	if len(t.ICEServers) > 0 && len(cfg.WebRTCICEServers) == 0 {
		cfg.WebRTCICEServers = append([]string(nil), t.ICEServers...)
	}
	if t.ICEUsername != "" && cfg.WebRTCICEUsername == "" {
		cfg.WebRTCICEUsername = t.ICEUsername
	}
	if t.ICECredential != "" && cfg.WebRTCICECredential == "" {
		cfg.WebRTCICECredential = t.ICECredential
	}
	if t.PrefersPCMA {
		cfg.WebRTCPrefersPCMA = true
	}
}

func enabledTransports(in []uistore.TransportSpec) []uistore.TransportSpec {
	out := make([]uistore.TransportSpec, 0, len(in))
	for _, t := range in {
		if t.Enabled {
			out = append(out, t)
		}
	}
	return out
}

// overlayEngineJSON merges raw JSON keys into cfg by unmarshalling into a
// shallow map. Unknown keys are ignored so the UI can extend the schema
// without breaking older workers.
func overlayEngineJSON(cfg *cli.Config, raw []byte) error {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return err
	}
	str := func(k string, dst *string) {
		if v, ok := m[k]; ok {
			var s string
			if json.Unmarshal(v, &s) == nil && s != "" {
				*dst = s
			}
		}
	}
	intg := func(k string, dst *int) {
		if v, ok := m[k]; ok {
			var n int
			if json.Unmarshal(v, &n) == nil {
				*dst = n
			}
		}
	}
	flt := func(k string, dst *float64) {
		if v, ok := m[k]; ok {
			var f float64
			if json.Unmarshal(v, &f) == nil {
				*dst = f
			}
		}
	}
	str("transport", &cfg.Transport)
	str("local_ip", &cfg.LocalIP)
	intg("local_port", &cfg.LocalPort)
	str("remote_host", &cfg.RemoteHost)
	intg("remote_port", &cfg.RemotePort)
	flt("rate", &cfg.Rate)
	intg("max_concurrent", &cfg.MaxConcurrent)
	if v, ok := m["total_calls"]; ok {
		var n int
		if json.Unmarshal(v, &n) == nil {
			cfg.TotalCalls = n
			cfg.TotalCallsSetExplicitly = true
		}
	}
	if v, ok := m["global_timeout_ms"]; ok {
		var n int
		if json.Unmarshal(v, &n) == nil && n > 0 {
			cfg.GlobalTimeout = time.Duration(n) * time.Millisecond
		}
	}
	str("sip_from", &cfg.SipFrom)
	str("sip_pai", &cfg.SipPAI)
	str("sip_provider", &cfg.SipProvider)
	flt("health_min_success_ratio", &cfg.HealthMinSuccessRatio)
	if v, ok := m["health_max_failed_calls"]; ok {
		var n int
		if json.Unmarshal(v, &n) == nil && n >= 0 {
			cfg.HealthMaxFailedCalls = n
		}
	}
	if v, ok := m["health_max_timeouts"]; ok {
		var n int
		if json.Unmarshal(v, &n) == nil && n >= 0 {
			cfg.HealthMaxTimeouts = n
		}
	}
	return nil
}
