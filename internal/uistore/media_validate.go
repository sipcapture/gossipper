package uistore

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
)

// ErrInvalidMedia is returned by PutMedia when the uploaded bytes fail the
// per-kind magic-number check. Wrap-friendly: callers can use errors.Is.
var ErrInvalidMedia = errors.New("invalid media payload")

// validateMediaContent inspects the first bytes of the temp file written by
// PutMedia and rejects files that do not match the declared kind:
//
//   - MediaWav: must start with the canonical RIFF/WAVE header and contain a
//     "fmt " chunk in the first 4 KiB. We do NOT enforce sample rate or
//     channels — the worker may downsample/resample at playback.
//   - MediaPcap: must start with the libpcap magic (a1b2c3d4 / d4c3b2a1, both
//     endians, μs + ns variants) or the PCAPNG section header block magic
//     (0a0d0d0a).
//   - All other kinds: no validation (forward compat).
func validateMediaContent(kind MediaKind, path string) error {
	f, err := os.Open(path) //nolint:gosec // path comes from os.CreateTemp
	if err != nil {
		return err
	}
	defer f.Close()
	switch kind {
	case MediaWav:
		return validateWAV(f)
	case MediaPcap:
		return validatePCAP(f)
	default:
		return nil
	}
}

func validateWAV(f *os.File) error {
	hdr := make([]byte, 12)
	if _, err := f.ReadAt(hdr, 0); err != nil {
		return fmt.Errorf("%w: short WAV header", ErrInvalidMedia)
	}
	if string(hdr[0:4]) != "RIFF" {
		return fmt.Errorf("%w: missing RIFF magic", ErrInvalidMedia)
	}
	if string(hdr[8:12]) != "WAVE" {
		return fmt.Errorf("%w: missing WAVE marker", ErrInvalidMedia)
	}
	// Look for a "fmt " chunk inside the first 4KiB to confirm this is a real
	// WAV and not just a random file that happens to begin with RIFF/WAVE.
	probe := make([]byte, 4096)
	n, _ := f.ReadAt(probe, 12)
	probe = probe[:n]
	if !containsBytes(probe, []byte("fmt ")) {
		return fmt.Errorf("%w: missing fmt chunk", ErrInvalidMedia)
	}
	return nil
}

func validatePCAP(f *os.File) error {
	hdr := make([]byte, 4)
	if _, err := f.ReadAt(hdr, 0); err != nil {
		return fmt.Errorf("%w: short PCAP header", ErrInvalidMedia)
	}
	magic := binary.BigEndian.Uint32(hdr)
	switch magic {
	case 0xa1b2c3d4, 0xd4c3b2a1, 0xa1b23c4d, 0x4d3cb2a1: // libpcap (μs + ns variants)
		return nil
	case 0x0a0d0d0a: // PCAPNG section header block
		return nil
	}
	return fmt.Errorf("%w: unknown PCAP/PCAPNG magic %08x", ErrInvalidMedia, magic)
}

func containsBytes(haystack, needle []byte) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		match := true
		for j := range needle {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
