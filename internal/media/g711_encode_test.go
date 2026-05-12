package media

import (
	"testing"
)

func TestEncodeG711FramePCMuSilence(t *testing.T) {
	t.Parallel()
	samples := make([]int16, 160)
	payload := EncodeG711Frame(0, samples)
	if len(payload) != 160 {
		t.Fatalf("len=%d", len(payload))
	}
	for i, b := range payload {
		if b != 0xff {
			t.Fatalf("byte %d: %#x want 0xff", i, b)
		}
	}
}

func TestEncodeG711FramePCMaSilence(t *testing.T) {
	t.Parallel()
	samples := make([]int16, 160)
	payload := EncodeG711Frame(8, samples)
	if len(payload) != 160 {
		t.Fatalf("len=%d", len(payload))
	}
	for i, b := range payload {
		if b != 0xD5 {
			t.Fatalf("byte %d: %#x want 0xd5", i, b)
		}
	}
}
