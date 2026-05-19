package supervisor

import (
	"path/filepath"
	"testing"

	"github.com/sipcapture/gossipper/internal/cli"
	"github.com/sipcapture/gossipper/internal/uistore"
)

// TestBuildConfigFromSpecPropagatesWebRTC asserts that ICE / codec fields on
// a profile's transport row land on cli.Config so the worker can pass them
// to engine.NewWebRTCBridge later.
func TestBuildConfigFromSpecPropagatesWebRTC(t *testing.T) {
	dir := t.TempDir()
	store, err := uistore.Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	p := uistore.ServerProfile{
		ID:   "primary",
		Name: "Primary",
		Transports: []uistore.TransportSpec{
			{
				Transport: "u1",
				LocalIP:   "127.0.0.1",
				LocalPort: 5061,
				Enabled:   true,
			},
			{
				Transport:     "webrtc",
				Enabled:       true,
				ICEServers:    []string{"stun:stun.l.google.com:19302", "turn:turn.example.com:3478"},
				ICEUsername:   "alice",
				ICECredential: "s3cret",
				PrefersPCMA:   true,
			},
		},
	}
	if _, err := store.PutServerProfile(p, true); err != nil {
		t.Fatal(err)
	}

	spec := Spec{
		JobID:        "j1",
		DataDir:      dir,
		ProfileID:    "primary",
		ProfileKind:  string(uistore.KindServer),
		ArtifactsDir: filepath.Join(dir, "artifacts", "j1"),
	}
	cfg, cleanup, err := BuildConfigFromSpec(spec)
	if cleanup != nil {
		t.Cleanup(cleanup)
	}
	if err != nil {
		t.Fatalf("BuildConfigFromSpec: %v", err)
	}
	if len(cfg.WebRTCICEServers) != 2 {
		t.Fatalf("expected 2 ICE servers, got %v", cfg.WebRTCICEServers)
	}
	if cfg.WebRTCICEUsername != "alice" || cfg.WebRTCICECredential != "s3cret" {
		t.Fatalf("ICE creds mismatch: %q / %q", cfg.WebRTCICEUsername, cfg.WebRTCICECredential)
	}
	if !cfg.WebRTCPrefersPCMA {
		t.Fatalf("PrefersPCMA not propagated")
	}
	if cfg.Transport != "u1" {
		t.Fatalf("expected SIP transport to win (u1), got %q (webrtc must not become cfg.Transport)", cfg.Transport)
	}
	if cfg.LocalPort != 5061 {
		t.Fatalf("expected LocalPort from SIP transport, got %d", cfg.LocalPort)
	}
}

// TestBuildConfigFromSpecWebRTCOnlyDoesNotBecomeSIPTransport guards against
// regression: a profile with only a webrtc row should not leak "webrtc" into
// cfg.Transport (which would crash the SIP engine on Run).
func TestBuildConfigFromSpecWebRTCOnlyDoesNotBecomeSIPTransport(t *testing.T) {
	dir := t.TempDir()
	store, err := uistore.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	p := uistore.ClientProfile{
		ID:   "rtc-only",
		Name: "RTC only",
		Transports: []uistore.TransportSpec{
			{Transport: "webrtc", Enabled: true, ICEServers: []string{"stun:stun.example.com:3478"}},
		},
	}
	if _, err := store.PutClientProfile(p, true); err != nil {
		t.Fatal(err)
	}
	spec := Spec{
		JobID:        "j2",
		DataDir:      dir,
		ProfileID:    "rtc-only",
		ProfileKind:  string(uistore.KindClient),
		ArtifactsDir: filepath.Join(dir, "artifacts", "j2"),
	}
	cfg, cleanup, err := BuildConfigFromSpec(spec)
	if cleanup != nil {
		t.Cleanup(cleanup)
	}
	if err != nil {
		t.Fatalf("BuildConfigFromSpec: %v", err)
	}
	if cfg.Transport == "webrtc" {
		t.Fatalf("webrtc must not become a SIP transport; got cfg.Transport=%q", cfg.Transport)
	}
	if len(cfg.WebRTCICEServers) != 1 {
		t.Fatalf("expected ICE servers preserved, got %v", cfg.WebRTCICEServers)
	}
}

// TestBuildConfigFromSpecFallsBackToBuiltinScenario asserts that a profile
// referencing an engine-baked scenario name (e.g. "management") works even
// when no scenario XML lives in uistore — the worker uses cfg.ScenarioName
// and lets scenario.LoadNamed resolve it.
func TestBuildConfigFromSpecFallsBackToBuiltinScenario(t *testing.T) {
	dir := t.TempDir()
	store, err := uistore.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	p := uistore.ServerProfile{
		ID:          "management",
		Name:        "management",
		ScenarioRef: "management",
		Transports: []uistore.TransportSpec{
			{Transport: "u1", LocalIP: "127.0.0.1", LocalPort: 5070, Enabled: true},
		},
	}
	if _, err := store.PutServerProfile(p, true); err != nil {
		t.Fatal(err)
	}
	spec := Spec{
		JobID:        "j3",
		DataDir:      dir,
		ProfileID:    "management",
		ProfileKind:  string(uistore.KindServer),
		ArtifactsDir: filepath.Join(dir, "artifacts", "j3"),
	}
	cfg, cleanup, err := BuildConfigFromSpec(spec)
	if cleanup != nil {
		t.Cleanup(cleanup)
	}
	if err != nil {
		t.Fatalf("BuildConfigFromSpec: %v", err)
	}
	if cfg.ScenarioFile != "" {
		t.Fatalf("expected no scenario file for built-in scenario, got %q", cfg.ScenarioFile)
	}
	if cfg.ScenarioName != "management" {
		t.Fatalf("expected ScenarioName=management, got %q", cfg.ScenarioName)
	}
}

// TestBuildConfigFromSpecUnknownScenarioStillErrors guards the fallback so
// that profiles referencing a typo or missing custom scenario fail loud
// rather than silently running the default UAC.
func TestBuildConfigFromSpecUnknownScenarioStillErrors(t *testing.T) {
	dir := t.TempDir()
	store, err := uistore.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	p := uistore.ServerProfile{
		ID:          "bogus",
		Name:        "bogus",
		ScenarioRef: "does-not-exist-anywhere",
		Transports: []uistore.TransportSpec{
			{Transport: "u1", LocalIP: "127.0.0.1", LocalPort: 5071, Enabled: true},
		},
	}
	if _, err := store.PutServerProfile(p, true); err != nil {
		t.Fatal(err)
	}
	spec := Spec{
		JobID:        "j4",
		DataDir:      dir,
		ProfileID:    "bogus",
		ProfileKind:  string(uistore.KindServer),
		ArtifactsDir: filepath.Join(dir, "artifacts", "j4"),
	}
	_, cleanup, err := BuildConfigFromSpec(spec)
	if cleanup != nil {
		t.Cleanup(cleanup)
	}
	if err == nil {
		t.Fatalf("expected error for unknown scenario, got nil")
	}
}

func TestOverlayEngineJSONSipAndHealth(t *testing.T) {
	cfg := cli.DefaultConfig()
	raw := []byte(`{
		"sip_from": "sip:alice@lab",
		"sip_pai": "alice@lab",
		"sip_provider": "prov1",
		"health_min_success_ratio": 0.95,
		"health_max_failed_calls": 0
	}`)
	if err := overlayEngineJSON(&cfg, raw); err != nil {
		t.Fatal(err)
	}
	if cfg.SipFrom != "sip:alice@lab" || cfg.SipPAI != "alice@lab" || cfg.SipProvider != "prov1" {
		t.Fatalf("sip identity: from=%q pai=%q provider=%q", cfg.SipFrom, cfg.SipPAI, cfg.SipProvider)
	}
	if cfg.HealthMinSuccessRatio != 0.95 || cfg.HealthMaxFailedCalls != 0 {
		t.Fatalf("health: ratio=%v max_failed=%d", cfg.HealthMinSuccessRatio, cfg.HealthMaxFailedCalls)
	}
}

func TestBuildConfigFromSpecSummaryHTML(t *testing.T) {
	dir := t.TempDir()
	store, err := uistore.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	p := uistore.ClientProfile{
		ID:          "c1",
		Name:        "c1",
		ScenarioRef: "invite_media",
		RemoteIP:    "127.0.0.1",
		RemotePort:  5060,
		Transports:  []uistore.TransportSpec{{Transport: "u1", LocalIP: "0.0.0.0", LocalPort: 0, Enabled: true}},
	}
	if _, err := store.PutClientProfile(p, true); err != nil {
		t.Fatal(err)
	}
	art := filepath.Join(dir, "artifacts", "j5")
	spec := Spec{
		JobID: "j5", DataDir: dir, ProfileID: "c1", ProfileKind: string(uistore.KindClient),
		ArtifactsDir: art,
	}
	cfg, cleanup, err := BuildConfigFromSpec(spec)
	if cleanup != nil {
		t.Cleanup(cleanup)
	}
	if err != nil {
		t.Fatal(err)
	}
	wantHTML := filepath.Join(art, "report.html")
	if cfg.SummaryHTML != wantHTML {
		t.Fatalf("SummaryHTML=%q want %q", cfg.SummaryHTML, wantHTML)
	}
}
