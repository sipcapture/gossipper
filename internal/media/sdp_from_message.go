package media

import (
	"strings"

	"github.com/sipcapture/gossipper/internal/sip"
)

// EffectiveMediaSDPBody returns SDP text suitable for media parsing: for
// application/trickle-ice+json bodies it converts JSON to a fragment of SDP lines.
func EffectiveMediaSDPBody(msg sip.Message) string {
	ct, ok := sip.Header(msg.Headers, "Content-Type")
	body := msg.Body
	if !ok || strings.TrimSpace(body) == "" {
		return body
	}
	ctLower := strings.ToLower(ct)
	if strings.Contains(ctLower, "application/trickle-ice+json") || strings.Contains(ctLower, "trickle-ice+json") {
		if frag, err := ParseTrickleICEJSONToSDPFragment(body); err == nil && strings.TrimSpace(frag) != "" {
			return frag
		}
	}
	return body
}
