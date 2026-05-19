package media

import (
	"os"
	"path/filepath"
	"testing"
)

func TestG711WAVRecorderMono(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mono.wav")
	r := &G711WAVRecorder{}
	if err := r.StartRecording(path, false, ""); err != nil {
		t.Fatal(err)
	}
	// μ-law silence-ish byte
	payload := make([]byte, 160)
	for i := range payload {
		payload[i] = 0xff
	}
	r.AppendReceived(payload, "PCMU")
	if err := r.StopRecording(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() < 44 {
		t.Fatalf("expected WAV file, stat err=%v size=%d", err, info.Size())
	}
}

func TestG711WAVRecorderDuplexEmptyRemoved(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.wav")
	r := &G711WAVRecorder{}
	if err := r.StartRecording(path, true, ""); err != nil {
		t.Fatal(err)
	}
	if err := r.StopRecording(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected empty recording removed, err=%v", err)
	}
}
