//go:build !linux

package media

import (
	"context"
	"fmt"
)

// StartMicrophone captures microphone audio (Linux: arecord) and sends RTP PCMU/8000.
func (s *Session) StartMicrophone(ctx context.Context, endpoint Endpoint, localIP string, localPort int) error {
	return fmt.Errorf("rtp_stream mic: only supported on Linux (requires arecord from alsa-utils)")
}
