package media

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net"
	"time"

	"github.com/pion/stun/v3"
)

const iceRandBytesUfrag = 6
const iceRandBytesPwd = 18

func randomICEFragment(nBytes int) string {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		// Extremely unlikely; fall back to deterministic padding (still unique enough for tests).
		for i := range b {
			b[i] = byte(i)
		}
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// EnsureLocalIceCredentials generates a=ice-ufrag / a=ice-pwd material for our SDP offer (WebRTC/browser).
func (s *Session) EnsureLocalIceCredentials() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.iceLocalUfrag != "" && s.iceLocalPwd != "" {
		return
	}
	s.iceLocalUfrag = randomICEFragment(iceRandBytesUfrag)
	s.iceLocalPwd = randomICEFragment(iceRandBytesPwd)
}

// ICELocalUfrag returns the local ICE username fragment for template substitution (SDP offer).
func (s *Session) ICELocalUfrag() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.iceLocalUfrag
}

// ICELocalPwd returns the local ICE password for template substitution (SDP offer).
func (s *Session) ICELocalPwd() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.iceLocalPwd
}

// SetRemoteIceFromSDP stores remote ICE credentials from m=audio, or from a trickle fragment
// (no m=audio) when a=ice-ufrag / a=ice-pwd appear there. If the body has no usable credentials,
// existing remote ICE is left unchanged (so candidate-only trickle does not clear ufrag/pwd).
func (s *Session) SetRemoteIceFromSDP(sdpBody string) {
	u, p, ok := ParseAudioIceCredentials(sdpBody)
	if !ok {
		u, p, ok = ParseAudioIceCredentialsTrickleFragment(sdpBody)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !ok {
		return
	}
	s.iceRemoteUfrag = u
	s.iceRemotePwd = p
}

func (s *Session) hasRemoteICE() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.iceRemoteUfrag != "" && s.iceRemotePwd != ""
}

// tryICEStunBindingResponse handles an inbound STUN Binding Request (ICE connectivity check toward us).
// Returns true if the packet was consumed and a response was sent (or attempted).
func (s *Session) tryICEStunBindingResponse(udp net.PacketConn, pkt []byte, from *net.UDPAddr) bool {
	if !stun.IsMessage(pkt) {
		return false
	}
	var m stun.Message
	if err := stun.Decode(pkt, &m); err != nil {
		return false
	}
	if m.Type.Method != stun.MethodBinding || m.Type.Class != stun.ClassRequest {
		return false
	}

	s.mu.Lock()
	locU, locP := s.iceLocalUfrag, s.iceLocalPwd
	remU, remP := s.iceRemoteUfrag, s.iceRemotePwd
	s.mu.Unlock()
	if locU == "" || locP == "" || remU == "" || remP == "" {
		return false
	}

	wantUser := locU + ":" + remU
	var user stun.Username
	if err := user.GetFrom(&m); err != nil || string(user) != wantUser {
		return false
	}
	if err := stun.MessageIntegrity([]byte(locP)).Check(&m); err != nil {
		return false
	}

	ip := from.IP
	if ip == nil || ip.IsUnspecified() {
		ip = net.IPv4zero
	}
	if ip4 := ip.To4(); ip4 != nil {
		ip = ip4
	}
	out, err := stun.Build(&m, stun.BindingSuccess,
		&stun.XORMappedAddress{IP: ip, Port: from.Port},
		stun.NewShortTermIntegrity(locP),
		stun.Fingerprint,
	)
	if err != nil {
		return false
	}
	_, _ = udp.WriteTo(out.Raw, from)
	return true
}

// iceConnectivityPingLoop sends STUN Binding requests toward remote (controlling / ICE-lite peer role).
func (s *Session) iceConnectivityPingLoop(ctx context.Context, udp net.PacketConn, remote *net.UDPAddr) {
	if !s.hasRemoteICE() {
		return
	}
	ticker := time.NewTicker(40 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-deadline.C:
			return
		case <-ticker.C:
			s.mu.Lock()
			lu, ru, rp := s.iceLocalUfrag, s.iceRemoteUfrag, s.iceRemotePwd
			s.mu.Unlock()
			if ru == "" || lu == "" || rp == "" {
				return
			}
			req, err := stun.Build(stun.BindingRequest, stun.TransactionID,
				stun.NewUsername(ru+":"+lu),
				stun.NewShortTermIntegrity(rp),
				stun.Fingerprint,
			)
			if err != nil {
				continue
			}
			_, _ = udp.WriteTo(req.Raw, remote)
		}
	}
}
