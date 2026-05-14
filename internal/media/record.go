package media

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/pion/rtp"
)

// ParseRTPRecordSpec parses exec rtp_record="..." values.
// Forms:
//
//	stop
//	start,<path.wav>[,duplex]
//
// Optional third token "duplex" records stereo WAV: left channel = sent (local),
// right channel = received (remote), padded to equal length with silence.
func ParseRTPRecordSpec(spec string) (cmd string, path string, duplex bool, err error) {
	spec = strings.TrimSpace(strings.ToLower(spec))
	if spec == "" {
		return "", "", false, fmt.Errorf("rtp_record: empty spec")
	}
	if spec == "stop" {
		return "stop", "", false, nil
	}
	if !strings.HasPrefix(spec, "start,") {
		return "", "", false, fmt.Errorf("rtp_record: expected stop or start,<path>[,duplex], got %q", spec)
	}
	rest := strings.TrimSpace(spec[len("start,"):])
	if rest == "" {
		return "", "", false, fmt.Errorf("rtp_record: start requires a path")
	}
	parts := strings.Split(rest, ",")
	path = strings.TrimSpace(parts[0])
	if path == "" {
		return "", "", false, fmt.Errorf("rtp_record: empty path")
	}
	if len(parts) >= 2 && strings.TrimSpace(strings.ToLower(parts[1])) == "duplex" {
		duplex = true
	}
	return "start", path, duplex, nil
}

func sanitizeCallIDForFilename(callID string) string {
	var b strings.Builder
	for _, r := range callID {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	s := strings.Trim(b.String(), "_")
	if s == "" {
		return "call"
	}
	if len(s) > 120 {
		s = s[:120]
	}
	return s
}

// SetAutoRecord enables automatic WAV capture when a media session starts.
// dir is the output directory (empty disables). When duplex is true, uses stereo duplex layout.
func (s *Session) SetAutoRecord(dir string, duplex bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.autoRecordDir = strings.TrimSpace(dir)
	s.autoRecordDuplex = duplex
}

// StartRecording begins buffering decoded G.711 (PT 0 / 8) RTP payloads for WAV export.
// For duplex, sent samples are captured from outbound PCMU/PCMA frames in parallel.
func (s *Session) StartRecording(path string, duplex bool, basePath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return fmt.Errorf("rtp_record: no active media session (start RTP first)")
	}
	// Replace any in-progress capture (including auto-record).
	if s.recordOn {
		_ = s.stopRecordingLocked()
	}
	path = ResolvePath(basePath, path)
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("rtp_record mkdir: %w", err)
		}
	}
	s.recordPath = path
	s.recordDuplex = duplex
	s.recordRecv = nil
	s.recordSent = nil
	s.recordOn = true
	return nil
}

// StopRecording flushes buffered PCM to the configured WAV path and disables recording.
func (s *Session) StopRecording() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopRecordingLocked()
}

func (s *Session) stopRecordingLocked() error {
	if !s.recordOn && s.recordPath == "" {
		return nil
	}
	path := s.recordPath
	duplex := s.recordDuplex
	recv := s.recordRecv
	sent := s.recordSent
	s.recordOn = false
	s.recordPath = ""
	s.recordRecv = nil
	s.recordSent = nil
	if path == "" {
		return nil
	}
	if len(recv) == 0 && (!duplex || len(sent) == 0) {
		_ = os.Remove(path)
		return nil
	}
	var err error
	if duplex {
		n := len(recv)
		if len(sent) > n {
			n = len(sent)
		}
		stereo := make([]int16, 2*n)
		for i := 0; i < n; i++ {
			if i < len(sent) {
				stereo[2*i] = sent[i]
			}
			if i < len(recv) {
				stereo[2*i+1] = recv[i]
			}
		}
		err = writeWAVPCM16LE(path, 8000, 2, stereo)
	} else {
		err = writeWAVPCM16LE(path, 8000, 1, recv)
	}
	return err
}

func (s *Session) flushRecording() {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.stopRecordingLocked()
}

func (s *Session) maybeStartAutoRecord() {
	s.mu.Lock()
	dir := s.autoRecordDir
	dup := s.autoRecordDuplex
	callID := s.callID
	running := s.running
	on := s.recordOn
	s.mu.Unlock()
	if dir == "" || !running || on || callID == "" {
		return
	}
	base := filepath.Join(dir, sanitizeCallIDForFilename(callID)+".wav")
	// StartRecording locks and checks running again
	_ = s.StartRecording(base, dup, "")
}

func (s *Session) appendRecordInbound(pkt *rtp.Packet) {
	if !s.recordOn {
		return
	}
	samples, err := decodeG711PayloadToPCM16(pkt.PayloadType, pkt.Payload)
	if err != nil || samples == nil {
		return
	}
	s.recordRecv = append(s.recordRecv, samples...)
}

func (s *Session) appendRecordOutbound(pt uint8, payload []byte) {
	if !s.recordOn || !s.recordDuplex {
		return
	}
	samples, err := decodeG711PayloadToPCM16(pt, payload)
	if err != nil || samples == nil {
		return
	}
	s.recordSent = append(s.recordSent, samples...)
}
