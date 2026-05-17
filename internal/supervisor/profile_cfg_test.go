package supervisor

import (
	"path/filepath"
	"testing"

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
