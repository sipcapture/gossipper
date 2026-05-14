package media

import (
	"os"
	"path/filepath"
	"testing"
)

func TestG711MuLawRoundTrip(t *testing.T) {
	t.Parallel()
	const b = byte(0xab)
	s := muLawSampleToLinear(b)
	out := linearToMuLaw(s)
	if out != b {
		t.Fatalf("round trip: got %02x want %02x", out, b)
	}
}

func TestWriteWAVStereoAndReadHeader(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "st.wav")
	st := []int16{100, -200, 300, -400}
	if err := writeWAVPCM16LE(path, 8000, 2, st); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 44 {
		t.Fatalf("wav too short: %d", len(data))
	}
	if string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		t.Fatalf("bad wav header")
	}
}

func TestParseRTPRecordSpec(t *testing.T) {
	t.Parallel()
	cmd, path, duplex, err := ParseRTPRecordSpec("start,./a.wav,Duplex")
	if err != nil || cmd != "start" || path != "./a.wav" || !duplex {
		t.Fatalf("got cmd=%q path=%q duplex=%v err=%v", cmd, path, duplex, err)
	}
	cmd2, _, _, err := ParseRTPRecordSpec("stop")
	if err != nil || cmd2 != "stop" {
		t.Fatalf("stop: %v", err)
	}
}
