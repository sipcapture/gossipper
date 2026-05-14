//go:build !linux && !audio

package media

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// StartMicrophone captures microphone audio via ffmpeg (requires ffmpeg in PATH).
// micInput: empty uses a per-OS default capture device; otherwise passed as ffmpeg -i value
// (e.g. ":1" on macOS for avfoundation, or `audio=Microphone` on Windows dshow).
func (s *Session) StartMicrophone(ctx context.Context, endpoint Endpoint, localIP string, localPort int, micInput string) error {
	s.Stop()

	ff, err := exec.LookPath("ffmpeg")
	if err != nil {
		return fmt.Errorf("rtp_stream mic: ffmpeg not in PATH: %w", err)
	}

	in := strings.TrimSpace(micInput)
	var args []string
	switch runtime.GOOS {
	case "darwin":
		if in == "" {
			in = ":0"
		}
		args = []string{"-nostdin", "-loglevel", "error", "-f", "avfoundation", "-i", in, "-ac", "1", "-ar", "8000", "-f", "s16le", "-"}
	case "windows":
		if in == "" {
			in = "audio=Microphone"
		}
		args = []string{"-nostdin", "-loglevel", "error", "-f", "dshow", "-i", in, "-ac", "1", "-ar", "8000", "-f", "s16le", "-"}
	default:
		ffIn := strings.TrimSpace(in)
		if ffIn == "" {
			return fmt.Errorf("rtp_stream mic: on %s pass device after mic,… (ffmpeg -i / capture args as one token is not supported; use space-free token or Linux)", runtime.GOOS)
		}
		extra := strings.Fields(ffIn)
		args = append([]string{"-nostdin", "-loglevel", "error"}, extra...)
		args = append(args, "-ac", "1", "-ar", "8000", "-f", "s16le", "-")
	}

	return s.startMicrophonePCMReader(ctx, endpoint, localIP, localPort, ff, args)
}
