package webrtc

import (
	"fmt"
	"strings"
	"time"

	pion "github.com/pion/webrtc/v4"

	"github.com/sipcapture/gossipper/internal/media"
)

const trickleFirstCandidateWait = 400 * time.Millisecond

// AddRemoteICECandidatesFromBody ingests trickle SDP fragments or JSON trickle
// bodies and forwards candidates to pion. Full session descriptions (v=0 with
// m=audio) should be applied via SetRemoteDescription instead.
func (b *Bridge) AddRemoteICECandidatesFromBody(body string) (int, error) {
	if b == nil || b.pc == nil {
		return 0, nil
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return 0, nil
	}
	if strings.HasPrefix(body, "{") || strings.HasPrefix(body, "[") {
		frag, err := media.ParseTrickleICEJSONToSDPFragment(body)
		if err != nil {
			return 0, fmt.Errorf("webrtc: parse trickle json: %w", err)
		}
		body = frag
	}
	if body == "" {
		return 0, nil
	}
	return b.addRemoteCandidateInits(extractCandidateInits(body))
}

func (b *Bridge) addRemoteCandidateInits(inits []pion.ICECandidateInit) (int, error) {
	added := 0
	for _, init := range inits {
		if err := b.pc.AddICECandidate(init); err != nil {
			return added, fmt.Errorf("webrtc: add ice candidate: %w", err)
		}
		added++
	}
	if added > 0 {
		b.stateMu.Lock()
		b.remoteTrickleAdded += added
		b.stateMu.Unlock()
	}
	return added, nil
}

func extractCandidateInits(body string) []pion.ICECandidateInit {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	var out []pion.ICECandidateInit
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(strings.ToLower(line), "a=candidate:") {
			continue
		}
		cand := strings.TrimSpace(line[len("a=candidate:"):])
		if cand == "" {
			continue
		}
		out = append(out, pion.ICECandidateInit{Candidate: "candidate:" + cand})
	}
	return out
}

func mergeCandidatesIntoSDP(sdp string, candidates []*pion.ICECandidate) string {
	if len(candidates) == 0 {
		return sdp
	}
	existing := map[string]struct{}{}
	for _, init := range extractCandidateInits(sdp) {
		existing[strings.TrimSpace(init.Candidate)] = struct{}{}
	}
	var extra []string
	for _, c := range candidates {
		if c == nil {
			continue
		}
		init := c.ToJSON()
		key := strings.TrimSpace(init.Candidate)
		if key == "" {
			continue
		}
		if _, ok := existing[key]; ok {
			continue
		}
		existing[key] = struct{}{}
		extra = append(extra, "a=candidate:"+strings.TrimPrefix(key, "candidate:"))
	}
	if len(extra) == 0 {
		return sdp
	}
	sdp = strings.TrimRight(sdp, "\r\n") + "\r\n"
	if !strings.Contains(strings.ToLower(sdp), "a=ice-options:") {
		sdp += "a=ice-options:trickle\r\n"
	}
	return sdp + strings.Join(extra, "\r\n") + "\r\n"
}

// IsTrickleICEFragment reports whether the body looks like a trickle-only ICE
// update (candidates / JSON trickle) rather than a full session description.
func IsTrickleICEFragment(body string) bool {
	return isTrickleFragment(body)
}

func isTrickleFragment(body string) bool {
	body = strings.TrimSpace(body)
	if body == "" {
		return false
	}
	if strings.HasPrefix(body, "{") || strings.HasPrefix(body, "[") {
		return true
	}
	if !media.SDPHasIceMediaAttributes(body) {
		return false
	}
	lower := strings.ToLower(body)
	return !strings.Contains(lower, "m=audio")
}
