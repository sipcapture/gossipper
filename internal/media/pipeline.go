package media

import (
	"errors"
	"strings"
)

// ErrUnsupportedOverWebRTC is returned when a scenario action requires classic
// UDP/RTP session features that are not implemented on the WebRTC bridge path.
var ErrUnsupportedOverWebRTC = errors.New("media: action is not supported over webrtc bridge")

// SDPBodyFromRawMessage extracts the SDP body from a full SIP message string.
func SDPBodyFromRawMessage(raw string) string {
	if i := strings.Index(raw, "\r\n\r\n"); i >= 0 {
		return raw[i+4:]
	}
	if i := strings.Index(raw, "\n\n"); i >= 0 {
		return raw[i+2:]
	}
	return ""
}
