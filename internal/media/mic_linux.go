//go:build linux && !audio

package media

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// StartMicrophone captures mono PCM at 8 kHz and sends PCMU RTP.
// micInput: empty = default ALSA device; non-empty = arecord -D <micInput>;
// prefix "ffmpeg:" = use ffmpeg; remainder is space-separated args inserted before "-ac 1 -ar 8000 -f s16le -".
func (s *Session) StartMicrophone(ctx context.Context, endpoint Endpoint, localIP string, localPort int, micInput string) error {
	s.Stop()

	in := strings.TrimSpace(micInput)
	if rest, ok := strings.CutPrefix(in, "ffmpeg:"); ok {
		ffmpeg, err := exec.LookPath("ffmpeg")
		if err != nil {
			return fmt.Errorf("rtp_stream mic: ffmpeg not in PATH: %w", err)
		}
		prefix := strings.Fields(strings.TrimSpace(rest))
		args := append(prefix, "-ac", "1", "-ar", "8000", "-f", "s16le", "-")
		return s.startMicrophonePCMReader(ctx, endpoint, localIP, localPort, ffmpeg, args)
	}

	arec, err := exec.LookPath("arecord")
	if err != nil {
		return fmt.Errorf("rtp_stream mic: arecord not found in PATH (install alsa-utils): %w", err)
	}
	args := []string{"-q", "-t", "raw", "-f", "S16_LE", "-c", "1", "-r", "8000"}
	if in != "" {
		args = append([]string{"-D", in}, args...)
	}
	args = append(args, "-")
	return s.startMicrophonePCMReader(ctx, endpoint, localIP, localPort, arec, args)
}
