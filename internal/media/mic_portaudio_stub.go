//go:build audio && !cgo

package media

import (
	"context"
	"fmt"
)

// StartMicrophone with -tags audio requires CGO and libportaudio (pkg-config portaudio-2.0).
func (s *Session) StartMicrophone(ctx context.Context, endpoint Endpoint, localIP string, localPort int, micInput string) error {
	_ = endpoint
	_ = localIP
	_ = localPort
	_ = micInput
	return fmt.Errorf("rtp_stream mic: PortAudio build needs CGO_ENABLED=1 and PortAudio dev headers (portaudio-2.0)")
}
