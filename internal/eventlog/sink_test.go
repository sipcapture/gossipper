package eventlog

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFormatTextOrdersAttrs(t *testing.T) {
	ev := Event{
		Time:  time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC),
		Level: LevelInfo,
		Kind:  KindSIPSend,
		Msg:   "INVITE",
		Attrs: map[string]any{
			"src_ip":     "10.0.0.2",
			"call_id":    "abc",
			"role":       "client",
			"sip.method": "INVITE",
		},
	}
	got := formatText(ev)
	want := `2026-04-24T12:00:00Z info sip.send INVITE call_id=abc role=client sip.method=INVITE src_ip=10.0.0.2`
	if got != want {
		t.Fatalf("formatText mismatch:\n got: %s\nwant: %s", got, want)
	}
}

func TestFormatJSONRoundTrip(t *testing.T) {
	ev := Event{
		Time:  time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC),
		Level: LevelInfo,
		Kind:  KindSIPRecv,
		Msg:   "200 OK",
		Attrs: map[string]any{"call_id": "abc", "src_port": 5060},
	}
	line := formatJSON(ev)
	var got struct {
		Time  string         `json:"time"`
		Level string         `json:"level"`
		Kind  string         `json:"kind"`
		Msg   string         `json:"msg"`
		Attrs map[string]any `json:"attrs"`
	}
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, line)
	}
	if got.Kind != KindSIPRecv || got.Msg != "200 OK" || got.Level != "info" {
		t.Fatalf("unexpected JSON: %+v", got)
	}
	if got.Attrs["call_id"] != "abc" {
		t.Fatalf("expected call_id attr, got %+v", got.Attrs)
	}
}

func TestFileSinkWritesJSONL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	sink, err := NewFileSink(path, map[string]any{"role": "server"})
	if err != nil {
		t.Fatalf("NewFileSink: %v", err)
	}
	if err := sink.Write([]Event{
		{Kind: KindCallStart, Msg: "started"},
		{Kind: KindCallEnd, Msg: "ended"},
	}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := sink.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if len(lines) != 2 {
		t.Fatalf("expected 2 JSONL lines, got %d", len(lines))
	}
	for _, line := range lines {
		var got map[string]any
		if err := json.Unmarshal([]byte(line), &got); err != nil {
			t.Fatalf("invalid JSON line %q: %v", line, err)
		}
		attrs, ok := got["attrs"].(map[string]any)
		if !ok || attrs["role"] != "server" {
			t.Fatalf("expected resource role=server in attrs, got %+v", got)
		}
	}
	if !strings.Contains(lines[0], KindCallStart) || !strings.Contains(lines[1], KindCallEnd) {
		t.Fatalf("unexpected line ordering: %v", lines)
	}
}

func TestParseLevel(t *testing.T) {
	cases := map[string]Level{
		"":      LevelInfo,
		"info":  LevelInfo,
		"INFO":  LevelInfo,
		"debug": LevelDebug,
		"warn":  LevelWarn,
		"error": LevelError,
		"err":   LevelError,
	}
	for name, want := range cases {
		got, ok := ParseLevel(name)
		if !ok || got != want {
			t.Fatalf("ParseLevel(%q)=%v ok=%v, want %v", name, got, ok, want)
		}
	}
	if _, ok := ParseLevel("verbose"); ok {
		t.Fatalf("ParseLevel(verbose) should report not-ok")
	}
}
