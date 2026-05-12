package media

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseRTPStreamSpecRecordWAVOptions(t *testing.T) {
	t.Parallel()

	cmd, cfg, err := ParseRTPStreamSpec("audio.raw,1,0,PCMU/8000,record_recv=recv.wav,record_send=send.wav", "/scen")
	if err != nil {
		t.Fatalf("ParseRTPStreamSpec: %v", err)
	}
	if cmd != "start" {
		t.Fatalf("cmd=%q", cmd)
	}
	if want := filepath.Join("/scen", "recv.wav"); cfg.RecordRecvWAV != want {
		t.Fatalf("RecordRecvWAV=%q want %q", cfg.RecordRecvWAV, want)
	}
	if want := filepath.Join("/scen", "send.wav"); cfg.RecordSendWAV != want {
		t.Fatalf("RecordSendWAV=%q want %q", cfg.RecordSendWAV, want)
	}
}

func TestPCMuPayloadDecodeSilence(t *testing.T) {
	t.Parallel()

	payload := make([]byte, 160)
	for i := range payload {
		payload[i] = 0xff
	}
	samples, err := rtpPayloadToPCM16Samples(0, payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 160 {
		t.Fatalf("len=%d", len(samples))
	}
	for i, s := range samples {
		if s != 0 {
			t.Fatalf("sample %d: %d want 0", i, s)
		}
	}
}

func TestPCMaPayloadDecodeSilence(t *testing.T) {
	t.Parallel()

	payload := make([]byte, 160)
	for i := range payload {
		payload[i] = 0xD5
	}
	samples, err := rtpPayloadToPCM16Samples(8, payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 160 {
		t.Fatalf("len=%d", len(samples))
	}
	for i, s := range samples {
		if s != 0 {
			t.Fatalf("sample %d: %d want 0", i, s)
		}
	}
}

func TestWavPCMRecorderWriteAndClose(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "out.wav")
	rec, err := newWavPCMRecorder(path, 8000)
	if err != nil {
		t.Fatal(err)
	}
	rec.appendRTPPayload(0, []byte{0xff, 0xff})
	rec.appendRTPPayload(0, []byte{0xff, 0xff})
	if err := rec.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 44 {
		t.Fatalf("file too short: %d", len(data))
	}
	if string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		t.Fatalf("bad wav header")
	}
}
