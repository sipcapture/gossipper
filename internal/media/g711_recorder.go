package media

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// G711WAVRecorder buffers decoded G.711 payloads for WAV export (WebRTC bridge path).
type G711WAVRecorder struct {
	mu     sync.Mutex
	on     bool
	path   string
	duplex bool
	recv   []int16
	sent   []int16
}

// StartRecording begins capture to path (resolved against basePath when relative).
func (r *G711WAVRecorder) StartRecording(path string, duplex bool, basePath string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.on {
		_ = r.stopLocked()
	}
	path = ResolvePath(basePath, path)
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("g711 recorder mkdir: %w", err)
		}
	}
	r.path = path
	r.duplex = duplex
	r.recv = nil
	r.sent = nil
	r.on = true
	return nil
}

// StopRecording flushes buffered PCM to WAV and disables capture.
func (r *G711WAVRecorder) StopRecording() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stopLocked()
}

func (r *G711WAVRecorder) stopLocked() error {
	if !r.on && r.path == "" {
		return nil
	}
	path := r.path
	duplex := r.duplex
	recv := r.recv
	sent := r.sent
	r.on = false
	r.path = ""
	r.recv = nil
	r.sent = nil
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

func (r *G711WAVRecorder) Recording() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.on
}

// AppendReceived appends inbound G.711 payload (PCMA or PCMU codec name).
func (r *G711WAVRecorder) AppendReceived(payload []byte, codec string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.on || len(payload) == 0 {
		return
	}
	pcm := pcmFromCodecPayload(codec, payload)
	if len(pcm) > 0 {
		r.recv = append(r.recv, pcm...)
	}
}

// AppendSent appends outbound G.711 payload when duplex capture is enabled.
func (r *G711WAVRecorder) AppendSent(payload []byte, codec string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.on || !r.duplex || len(payload) == 0 {
		return
	}
	pcm := pcmFromCodecPayload(codec, payload)
	if len(pcm) > 0 {
		r.sent = append(r.sent, pcm...)
	}
}

func pcmFromCodecPayload(codec string, payload []byte) []int16 {
	pt := uint8(0)
	if strings.EqualFold(codec, "PCMA") {
		pt = 8
	}
	samples, err := decodeG711PayloadToPCM16(pt, payload)
	if err != nil || len(samples) == 0 {
		return nil
	}
	return samples
}
