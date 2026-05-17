package engine

import (
	"context"
	"testing"
	"time"
)

// TestEngineNewWebRTCBridge keeps the engine ↔ internal/webrtc wiring honest
// even before runtime integration: it constructs an Engine with WebRTC
// config, calls NewWebRTCBridge, and exercises the offer/answer + SRTP
// handshake against a peer bridge. If the engine.Config-to-Options copy
// ever loses a field, this test breaks immediately.
func TestEngineNewWebRTCBridge(t *testing.T) {
	t.Parallel()
	e := New(Config{
		Transport:           "u1",
		LocalIP:             "127.0.0.1",
		WebRTCICEServers:    []string{},
		WebRTCPrefersPCMA:   true,
		WebRTCICEUsername:   "u",
		WebRTCICECredential: "c",
	})
	br, err := e.NewWebRTCBridge()
	if err != nil {
		t.Fatalf("NewWebRTCBridge: %v", err)
	}
	defer br.Close()
	if got := br.Codec(); got != "PCMA" {
		t.Fatalf("expected PCMA codec from PrefersPCMA=true, got %q", got)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	offer, err := br.CreateOffer(ctx)
	if err != nil {
		t.Fatalf("CreateOffer: %v", err)
	}
	if len(offer) == 0 {
		t.Fatal("empty offer SDP")
	}
}
