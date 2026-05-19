package engine

// WebRTC bridge factory plumbing.
//
// Phase 4.2 plan (see docs/webrtc.md): when a SIP scenario advertises a
// WebRTC-style media block, the engine spins up an internal/webrtc.Bridge
// per call instead of an internal/media.Session. The Bridge then:
//   - generates the SDP answer (or offer) that gets inlined into the SIP
//     dialog,
//   - terminates DTLS-SRTP locally,
//   - bridges decoded G.711 frames between the SIP scenario's RTP pipeline
//     and pion's TrackLocal / TrackRemote.
//
// The factory below lives in the engine package because it needs the engine's
// resolved cfg (Transport-specific ICE settings, codec prefs). It is not
// invoked by Engine.Run yet — the engine continues to use UDP RTP today.
// Wiring it into the executeCall path is the next concrete step; the unit
// test that asserts a Bridge can be created from engine.Config keeps the
// plumbing honest in the meantime.

import (
	"time"

	"github.com/sipcapture/gossipper/internal/webrtc"
)

// NewWebRTCBridge constructs a webrtc.Bridge configured from this engine's
// runtime settings (ICE servers / credentials / codec preference). Returns
// the same error the underlying constructor reports; callers that need to
// pre-flight a Bridge before SIP-call time can use this without holding any
// engine locks.
func (e *Engine) NewWebRTCBridge() (*webrtc.Bridge, error) {
	return webrtc.NewBridge(webrtc.Options{
		ICEServers:    e.cfg.WebRTCICEServers,
		ICEUsername:   e.cfg.WebRTCICEUsername,
		ICECredential: e.cfg.WebRTCICECredential,
		ICEAuthSecret: e.cfg.WebRTCICEAuthSecret,
		ICEAuthTTL:    time.Duration(e.cfg.WebRTCICEAuthTTLSec) * time.Second,
		PrefersPCMA:   e.cfg.WebRTCPrefersPCMA,
	})
}
