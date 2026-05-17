package uistore

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s
}

func TestServerProfileCRUD(t *testing.T) {
	s := newTestStore(t)
	p := ServerProfile{
		ID:   "primary",
		Name: "Primary",
		Transports: []TransportSpec{
			{Transport: "u1", LocalIP: "0.0.0.0", LocalPort: 5060, Enabled: true},
		},
	}
	got, err := s.PutServerProfile(p, true)
	if err != nil {
		t.Fatalf("PutServerProfile create: %v", err)
	}
	if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Fatalf("expected timestamps to be set: %+v", got)
	}
	if _, err := s.PutServerProfile(p, true); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict on second create, got %v", err)
	}
	got.Name = "Renamed"
	updated, err := s.PutServerProfile(got, false)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Name != "Renamed" {
		t.Fatalf("expected Name=Renamed, got %q", updated.Name)
	}
	if !updated.UpdatedAt.After(got.CreatedAt) && !updated.UpdatedAt.Equal(got.CreatedAt) {
		t.Fatalf("UpdatedAt should be >= CreatedAt, got %v / %v", updated.UpdatedAt, got.CreatedAt)
	}
	list, err := s.ListServerProfiles()
	if err != nil || len(list) != 1 {
		t.Fatalf("ListServerProfiles len=%d err=%v", len(list), err)
	}
	if err := s.DeleteServerProfile("primary"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.GetServerProfile("primary"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestClientProfileCRUD(t *testing.T) {
	s := newTestStore(t)
	p := ClientProfile{
		ID:        "stress",
		Name:      "Stress UAC",
		RemoteIP:  "127.0.0.1",
		RemotePort: 5060,
		Rate:      10,
	}
	if _, err := s.PutClientProfile(p, true); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := s.GetClientProfile("stress")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Rate != 10 {
		t.Fatalf("Rate mismatch: %v", got.Rate)
	}
	if err := s.DeleteClientProfile("stress"); err != nil {
		t.Fatalf("delete: %v", err)
	}
}

func TestScenarioCRUD(t *testing.T) {
	s := newTestStore(t)
	body := `<?xml version="1.0"?><scenario name="x"/>`
	out, err := s.PutScenario(ScenarioMeta{ID: "uas_basic", Name: "Basic UAS"}, body, true)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if out.XML != body {
		t.Fatalf("body mismatch")
	}
	got, err := s.GetScenario("uas_basic")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Meta.Name != "Basic UAS" {
		t.Fatalf("name mismatch: %q", got.Meta.Name)
	}
	list, err := s.ListScenarios()
	if err != nil || len(list) != 1 {
		t.Fatalf("list len=%d err=%v", len(list), err)
	}
	if err := s.DeleteScenario("uas_basic"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.GetScenario("uas_basic"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestMediaUploadAndList(t *testing.T) {
	s := newTestStore(t)
	wav := minimalWAV()
	a, err := s.PutMedia(MediaWav, "ringback.wav", bytes.NewReader(wav))
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if a.SizeBytes != int64(len(wav)) {
		t.Fatalf("size mismatch: %d", a.SizeBytes)
	}
	list, err := s.ListMedia(MediaWav)
	if err != nil || len(list) != 1 {
		t.Fatalf("list len=%d err=%v", len(list), err)
	}
	if err := s.DeleteMedia(MediaWav, "ringback.wav"); err != nil {
		t.Fatalf("delete: %v", err)
	}
}

func TestMediaUploadRejectsInvalidWAV(t *testing.T) {
	s := newTestStore(t)
	_, err := s.PutMedia(MediaWav, "bogus.wav", strings.NewReader("RIFFnoFMT"))
	if !errors.Is(err, ErrInvalidMedia) {
		t.Fatalf("expected ErrInvalidMedia, got %v", err)
	}
}

func TestMediaUploadRejectsInvalidPCAP(t *testing.T) {
	s := newTestStore(t)
	_, err := s.PutMedia(MediaPcap, "bogus.pcap", strings.NewReader("\x00\x00\x00\x00rest"))
	if !errors.Is(err, ErrInvalidMedia) {
		t.Fatalf("expected ErrInvalidMedia, got %v", err)
	}
}

func TestMediaUploadAcceptsValidPCAP(t *testing.T) {
	s := newTestStore(t)
	pcap := append([]byte{0xa1, 0xb2, 0xc3, 0xd4}, make([]byte, 20)...)
	if _, err := s.PutMedia(MediaPcap, "capture.pcap", bytes.NewReader(pcap)); err != nil {
		t.Fatalf("put valid PCAP: %v", err)
	}
}

// minimalWAV returns the smallest byte sequence that satisfies validateWAV:
// canonical 44-byte PCM mono 8kHz 16-bit header with no payload.
func minimalWAV() []byte {
	out := []byte("RIFF")
	out = append(out, 0x24, 0x00, 0x00, 0x00) // chunk size
	out = append(out, []byte("WAVE")...)
	out = append(out, []byte("fmt ")...)
	out = append(out, 0x10, 0x00, 0x00, 0x00) // subchunk1 size
	out = append(out, 0x01, 0x00)             // audio format = 1 (PCM)
	out = append(out, 0x01, 0x00)             // channels = 1
	out = append(out, 0x40, 0x1f, 0x00, 0x00) // sample rate = 8000
	out = append(out, 0x80, 0x3e, 0x00, 0x00) // byte rate
	out = append(out, 0x02, 0x00)             // block align
	out = append(out, 0x10, 0x00)             // bits per sample
	out = append(out, []byte("data")...)
	out = append(out, 0x00, 0x00, 0x00, 0x00) // data size = 0
	return out
}

func TestInvalidIDs(t *testing.T) {
	s := newTestStore(t)
	for _, bad := range []string{"", "../etc", "with space", "слишком/глубоко", strings.Repeat("x", 200)} {
		if _, err := s.PutServerProfile(ServerProfile{ID: bad}, true); !errors.Is(err, ErrInvalidID) {
			t.Fatalf("expected ErrInvalidID for %q, got %v", bad, err)
		}
	}
}
