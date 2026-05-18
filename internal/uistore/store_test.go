package uistore

import (
	"bytes"
	"errors"
	"fmt"
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

func TestScenarioHistorySnapshotsAndRetrieval(t *testing.T) {
	s := newTestStore(t)
	v1 := `<?xml version="1.0"?><scenario name="v1"/>`
	v2 := `<?xml version="1.0"?><scenario name="v2"/>`
	v3 := `<?xml version="1.0"?><scenario name="v3"/>`
	if _, err := s.PutScenario(ScenarioMeta{ID: "demo", Name: "v1"}, v1, true); err != nil {
		t.Fatalf("put v1: %v", err)
	}
	// First update — should produce one snapshot of v1.
	if _, err := s.PutScenario(ScenarioMeta{ID: "demo", Name: "v2"}, v2, false); err != nil {
		t.Fatalf("put v2: %v", err)
	}
	h, err := s.ListScenarioHistory("demo")
	if err != nil {
		t.Fatalf("history after v2: %v", err)
	}
	if len(h) != 1 {
		t.Fatalf("expected 1 history entry, got %d", len(h))
	}
	if h[0].Meta.Name != "v1" {
		t.Fatalf("history meta should reflect v1, got %q", h[0].Meta.Name)
	}
	got, err := s.GetScenarioHistory("demo", h[0].TS)
	if err != nil {
		t.Fatalf("get history: %v", err)
	}
	if got.XML != v1 {
		t.Fatalf("history XML mismatch: %q", got.XML)
	}
	// Second update — second snapshot, newest first.
	if _, err := s.PutScenario(ScenarioMeta{ID: "demo", Name: "v3"}, v3, false); err != nil {
		t.Fatalf("put v3: %v", err)
	}
	h, err = s.ListScenarioHistory("demo")
	if err != nil {
		t.Fatalf("history after v3: %v", err)
	}
	if len(h) != 2 {
		t.Fatalf("expected 2 history entries, got %d", len(h))
	}
	if h[0].Meta.Name != "v2" || h[1].Meta.Name != "v1" {
		t.Fatalf("history order wrong: %q / %q", h[0].Meta.Name, h[1].Meta.Name)
	}
}

func TestScenarioHistorySkipsIdenticalXML(t *testing.T) {
	s := newTestStore(t)
	xml := `<?xml version="1.0"?><scenario name="x"/>`
	if _, err := s.PutScenario(ScenarioMeta{ID: "noop", Name: "first"}, xml, true); err != nil {
		t.Fatalf("put: %v", err)
	}
	// Same XML, only meta changes → no snapshot.
	if _, err := s.PutScenario(ScenarioMeta{ID: "noop", Name: "renamed"}, xml, false); err != nil {
		t.Fatalf("put renamed: %v", err)
	}
	h, err := s.ListScenarioHistory("noop")
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(h) != 0 {
		t.Fatalf("expected no history when XML unchanged, got %d", len(h))
	}
}

func TestScenarioHistoryDeleteCleansUp(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.PutScenario(ScenarioMeta{ID: "gone", Name: "v1"}, `<a/>`, true); err != nil {
		t.Fatalf("put v1: %v", err)
	}
	if _, err := s.PutScenario(ScenarioMeta{ID: "gone", Name: "v2"}, `<b/>`, false); err != nil {
		t.Fatalf("put v2: %v", err)
	}
	if err := s.DeleteScenario("gone"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.ListScenarioHistory("gone"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestScenarioHistoryPruneKeepsNewest(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenWithOptions(dir, StoreOptions{ScenarioHistoryKeep: 2})
	if err != nil {
		t.Fatalf("OpenWithOptions: %v", err)
	}
	xml := func(v int) string { return fmt.Sprintf(`<?xml version="1.0"?><scenario v="%d"/>`, v) }
	if _, err := s.PutScenario(ScenarioMeta{ID: "cap", Name: "v0"}, xml(0), true); err != nil {
		t.Fatalf("put v0: %v", err)
	}
	for i := 1; i <= 4; i++ {
		if _, err := s.PutScenario(ScenarioMeta{ID: "cap", Name: fmt.Sprintf("v%d", i)}, xml(i), false); err != nil {
			t.Fatalf("put v%d: %v", i, err)
		}
	}
	hist, err := s.ListScenarioHistory("cap")
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(hist) != 2 {
		t.Fatalf("expected 2 snapshots after prune, got %d", len(hist))
	}
	// Newest should be v3 then v2 (v0 was overwritten before snapshots; v1 pruned).
	got, err := s.GetScenarioHistory("cap", hist[0].TS)
	if err != nil {
		t.Fatalf("get newest: %v", err)
	}
	if !strings.Contains(got.XML, `v="3"`) {
		t.Fatalf("newest snapshot should be v3, got %q", got.XML)
	}
}

func TestForkScenarioFromHistory(t *testing.T) {
	s := newTestStore(t)
	v1 := `<?xml version="1.0"?><scenario name="orig"/>`
	if _, err := s.PutScenario(ScenarioMeta{ID: "src", Name: "Source", Role: "server", Description: "desc"}, v1, true); err != nil {
		t.Fatalf("put: %v", err)
	}
	if _, err := s.PutScenario(ScenarioMeta{ID: "src", Name: "Source v2"}, `<?xml version="1.0"?><scenario name="v2"/>`, false); err != nil {
		t.Fatalf("update: %v", err)
	}
	hist, err := s.ListScenarioHistory("src")
	if err != nil || len(hist) != 1 {
		t.Fatalf("history len=%d err=%v", len(hist), err)
	}
	forked, err := s.ForkScenarioFromHistory("src", hist[0].TS, ScenarioMeta{ID: "forked", Name: "Forked copy"})
	if err != nil {
		t.Fatalf("fork: %v", err)
	}
	if forked.XML != v1 {
		t.Fatalf("fork XML mismatch: %q", forked.XML)
	}
	if forked.Meta.Role != "server" || forked.Meta.Description != "desc" {
		t.Fatalf("fork meta not inherited: %+v", forked.Meta)
	}
	if _, err := s.ForkScenarioFromHistory("src", hist[0].TS, ScenarioMeta{ID: "src"}); err == nil {
		t.Fatal("expected error when fork id equals source")
	}
}

func TestDeleteScenarioHistoryEntry(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.PutScenario(ScenarioMeta{ID: "h", Name: "v1"}, `<a/>`, true); err != nil {
		t.Fatalf("put v1: %v", err)
	}
	if _, err := s.PutScenario(ScenarioMeta{ID: "h", Name: "v2"}, `<b/>`, false); err != nil {
		t.Fatalf("put v2: %v", err)
	}
	if _, err := s.PutScenario(ScenarioMeta{ID: "h", Name: "v3"}, `<c/>`, false); err != nil {
		t.Fatalf("put v3: %v", err)
	}
	hist, err := s.ListScenarioHistory("h")
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(hist) != 2 {
		t.Fatalf("expected 2 snapshots, got %d", len(hist))
	}
	// Remove the oldest snapshot (v1) and re-list.
	if err := s.DeleteScenarioHistory("h", hist[1].TS); err != nil {
		t.Fatalf("delete oldest: %v", err)
	}
	hist2, err := s.ListScenarioHistory("h")
	if err != nil {
		t.Fatalf("history after delete: %v", err)
	}
	if len(hist2) != 1 || hist2[0].TS != hist[0].TS {
		t.Fatalf("expected only v2 snapshot to remain, got %+v", hist2)
	}
	if err := s.DeleteScenarioHistory("h", hist[1].TS); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound on second delete, got %v", err)
	}
	if err := s.DeleteScenarioHistory("h", "../etc/passwd"); !errors.Is(err, ErrInvalidHistoryTS) {
		t.Fatalf("expected ErrInvalidHistoryTS, got %v", err)
	}
}

func TestScenarioHistoryRejectsBadTS(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.PutScenario(ScenarioMeta{ID: "ok", Name: "x"}, `<x/>`, true); err != nil {
		t.Fatalf("put: %v", err)
	}
	for _, bad := range []string{"../etc/passwd", "20260518T170230Z/../x", "not a ts", ""} {
		if _, err := s.GetScenarioHistory("ok", bad); err == nil {
			t.Fatalf("expected error for ts %q, got nil", bad)
		}
	}
}

func TestInvalidIDs(t *testing.T) {
	s := newTestStore(t)
	for _, bad := range []string{"", "../etc", "with space", "nested/deep", strings.Repeat("x", 200)} {
		if _, err := s.PutServerProfile(ServerProfile{ID: bad}, true); !errors.Is(err, ErrInvalidID) {
			t.Fatalf("expected ErrInvalidID for %q, got %v", bad, err)
		}
	}
}
