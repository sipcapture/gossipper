package media

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// μ-law / A-law decode tables for G.711 when recording RTP to WAV.
var muLawDecodeTable [256]int16
var aLawDecodeTable [256]int16

func init() {
	for i := 0; i < 256; i++ {
		muLawDecodeTable[i] = muLawToLinear(byte(i))
	}
	// A-law: many linear samples map to the same wire code; pick the one with
	// smallest magnitude so silence (0xD5) decodes to 0 like linearToALaw(0).
	for bi := 0; bi < 256; bi++ {
		b := byte(bi)
		var best int16
		var bestAbs int32 = 1 << 30
		for s := -32768; s <= 32767; s++ {
			v := int16(s)
			if linearToALaw(v) != b {
				continue
			}
			abs := int32(v)
			if abs < 0 {
				abs = -abs
			}
			if abs < bestAbs {
				bestAbs = abs
				best = v
			}
		}
		aLawDecodeTable[bi] = best
	}
}

// muLawToLinear expands one μ-law byte to 16-bit linear PCM (spandsp-style).
func muLawToLinear(u byte) int16 {
	u = ^u
	sign := u & 0x80
	exponent := int32((u >> 4) & 0x07)
	mantissa := int32(u & 0x0F)
	t := (mantissa << 3) + 0x84
	t <<= uint(exponent)
	if sign != 0 {
		return int16(0x84 - t)
	}
	return int16(t - 0x84)
}

func rtpPayloadToPCM16Samples(pt uint8, payload []byte) ([]int16, error) {
	if len(payload) == 0 {
		return nil, nil
	}
	switch pt {
	case 0:
		out := make([]int16, len(payload))
		for i, b := range payload {
			out[i] = muLawDecodeTable[b]
		}
		return out, nil
	case 8:
		out := make([]int16, len(payload))
		for i, b := range payload {
			out[i] = aLawDecodeTable[b]
		}
		return out, nil
	default:
		return nil, fmt.Errorf("wav record: unsupported RTP payload type %d (only PCMU=0 and PCMA=8)", pt)
	}
}

type wavPCMRecorder struct {
	mu        sync.Mutex
	path      string
	rate      int
	samples   []int16
	closed    bool
	closeOnce sync.Once
}

func newWavPCMRecorder(path string, sampleRate int) (*wavPCMRecorder, error) {
	if path == "" {
		return nil, fmt.Errorf("wav record path is empty")
	}
	if sampleRate <= 0 {
		return nil, fmt.Errorf("wav record sample rate %d", sampleRate)
	}
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	return &wavPCMRecorder{path: path, rate: sampleRate}, nil
}

func (w *wavPCMRecorder) appendRTPPayload(pt uint8, payload []byte) {
	samples, err := rtpPayloadToPCM16Samples(pt, payload)
	if err != nil || len(samples) == 0 {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	w.samples = append(w.samples, samples...)
}

func (w *wavPCMRecorder) Close() error {
	var err error
	w.closeOnce.Do(func() {
		w.mu.Lock()
		w.closed = true
		data := append([]int16(nil), w.samples...)
		w.samples = nil
		rate := w.rate
		path := w.path
		w.mu.Unlock()
		err = writeWAVPCM16Mono(path, rate, data)
	})
	return err
}

// writeWAVPCM16Mono writes a PCM 16-bit mono little-endian WAV file.
func writeWAVPCM16Mono(path string, sampleRate int, samples []int16) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	dataSize := len(samples) * 2
	riffSize := uint32(36 + dataSize)

	hdr := make([]byte, 44)
	copy(hdr[0:4], []byte("RIFF"))
	binary.LittleEndian.PutUint32(hdr[4:8], riffSize)
	copy(hdr[8:12], []byte("WAVE"))
	copy(hdr[12:16], []byte("fmt "))
	binary.LittleEndian.PutUint32(hdr[16:20], 16)
	binary.LittleEndian.PutUint16(hdr[20:22], 1) // PCM
	binary.LittleEndian.PutUint16(hdr[22:24], 1) // mono
	binary.LittleEndian.PutUint32(hdr[24:28], uint32(sampleRate))
	byteRate := sampleRate * 2
	binary.LittleEndian.PutUint32(hdr[28:32], uint32(byteRate))
	binary.LittleEndian.PutUint16(hdr[32:34], 2) // block align
	binary.LittleEndian.PutUint16(hdr[34:36], 16)
	copy(hdr[36:40], []byte("data"))
	binary.LittleEndian.PutUint32(hdr[40:44], uint32(dataSize))

	if _, err := f.Write(hdr); err != nil {
		return err
	}
	if len(samples) > 0 {
		body := make([]byte, len(samples)*2)
		for i, sample := range samples {
			binary.LittleEndian.PutUint16(body[i*2:], uint16(sample))
		}
		if _, err := f.Write(body); err != nil {
			return err
		}
	}
	return nil
}
