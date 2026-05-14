package media

import (
	"strings"
)

// SDPHintsSRTP returns true if the SDP body suggests SRTP (SAVP profile, crypto, or DTLS fingerprint).
func SDPHintsSRTP(sdpBody string) bool {
	body := strings.ReplaceAll(sdpBody, "\r\n", "\n")
	low := strings.ToLower(body)
	if strings.Contains(low, "rtp/savp") || strings.Contains(low, "rtp/savpf") {
		return true
	}
	for _, line := range strings.Split(low, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "a=crypto:") || strings.HasPrefix(line, "a=fingerprint:") {
			return true
		}
	}
	return false
}
