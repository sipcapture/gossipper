package backfill

import (
	"context"
	"encoding/json"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseDuration(t *testing.T) {
	tests := []struct {
		input string
		want  time.Duration
		err   bool
	}{
		{"30d", 30 * 24 * time.Hour, false},
		{"7d12h", 7*24*time.Hour + 12*time.Hour, false},
		{"2h30m", 2*time.Hour + 30*time.Minute, false},
		{"1d2h3m4s", 1*24*time.Hour + 2*time.Hour + 3*time.Minute + 4*time.Second, false},
		{"60s", 60 * time.Second, false},
		{"1h", 1 * time.Hour, false},
		{"500ms", 500 * time.Millisecond, false}, // stdlib fallback
		{"", 0, true},
		{"xyz", 0, true},
		{"0d", 0, true}, // resolves to zero
	}
	for _, tt := range tests {
		got, err := ParseDuration(tt.input)
		if tt.err {
			if err == nil {
				t.Errorf("ParseDuration(%q) expected error, got %v", tt.input, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseDuration(%q) unexpected error: %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseDuration(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestConfigValidate(t *testing.T) {
	valid := DefaultConfig()
	valid.HEPAddr = "127.0.0.1:9060"
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid config: unexpected error: %v", err)
	}

	// No outputs configured.
	noOutput := DefaultConfig()
	if err := noOutput.Validate(); err == nil {
		t.Fatal("expected error when no output configured")
	}

	// Bad CPS.
	badCPS := DefaultConfig()
	badCPS.HEPAddr = "127.0.0.1:9060"
	badCPS.CPS = -1
	if err := badCPS.Validate(); err == nil {
		t.Fatal("expected error for negative CPS")
	}

	// Bad duration range.
	badDur := DefaultConfig()
	badDur.HEPAddr = "127.0.0.1:9060"
	badDur.CallDurationMin = 60 * time.Second
	badDur.CallDurationMax = 10 * time.Second
	if err := badDur.Validate(); err == nil {
		t.Fatal("expected error for inverted call duration range")
	}
}

func TestSynthesizeCall_SuccessDialog(t *testing.T) {
	cfg := DefaultConfig()
	cfg.FailRatio = 0 // all calls succeed
	rng := rand.New(rand.NewSource(42))
	start := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)

	call := synthesizeCall(cfg, start, 1, rng)

	if call.Failed {
		t.Fatal("expected successful call")
	}
	if call.Status != 200 {
		t.Fatalf("expected status 200, got %d", call.Status)
	}
	if call.CallID == "" {
		t.Fatal("empty call ID")
	}
	if len(call.Messages) != 7 {
		t.Fatalf("expected 7 messages (INVITE,100,180,200,ACK,BYE,200), got %d", len(call.Messages))
	}

	// Verify message ordering and directions.
	expectedDirs := []string{"send", "recv", "recv", "recv", "send", "send", "recv"}
	for i, msg := range call.Messages {
		if msg.Direction != expectedDirs[i] {
			t.Errorf("message %d: direction=%s, want %s", i, msg.Direction, expectedDirs[i])
		}
		if len(msg.Payload) == 0 {
			t.Errorf("message %d: empty payload", i)
		}
	}

	// Verify offsets are monotonically increasing.
	for i := 1; i < len(call.Messages); i++ {
		if call.Messages[i].Offset < call.Messages[i-1].Offset {
			t.Errorf("message %d offset %v < message %d offset %v", i, call.Messages[i].Offset, i-1, call.Messages[i-1].Offset)
		}
	}

	// Verify INVITE payload contains the Call-ID.
	if !strings.Contains(string(call.Messages[0].Payload), call.CallID) {
		t.Error("INVITE payload missing Call-ID")
	}
}

func TestSynthesizeCall_FailedDialog(t *testing.T) {
	cfg := DefaultConfig()
	cfg.FailRatio = 1.0 // all calls fail
	rng := rand.New(rand.NewSource(42))
	start := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)

	call := synthesizeCall(cfg, start, 1, rng)

	if !call.Failed {
		t.Fatal("expected failed call")
	}
	if call.Status == 200 {
		t.Fatal("failed call should not have status 200")
	}
	// Failed calls have 4 messages: INVITE, 100, error response, ACK.
	if len(call.Messages) != 4 {
		t.Fatalf("expected 4 messages for failed call, got %d", len(call.Messages))
	}
}

func TestSynthesizeCall_CallIDCorrelation(t *testing.T) {
	cfg := DefaultConfig()
	cfg.FailRatio = 0
	rng := rand.New(rand.NewSource(99))
	start := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)

	call := synthesizeCall(cfg, start, 1, rng)

	for i, msg := range call.Messages {
		if !strings.Contains(string(msg.Payload), call.CallID) {
			t.Errorf("message %d does not contain Call-ID %q", i, call.CallID)
		}
	}
}

func TestBuildLogEntry(t *testing.T) {
	cfg := DefaultConfig()
	call := syntheticCall{
		CallID:   "test-call-id@backfill",
		Start:    time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC),
		Duration: 30 * time.Second,
		Failed:   false,
		Status:   200,
	}

	entry := buildLogEntry(cfg, call)
	if entry.CallID != call.CallID {
		t.Errorf("call_id mismatch: got %q", entry.CallID)
	}
	if entry.DurationMS != 30000 {
		t.Errorf("duration_ms: got %d, want 30000", entry.DurationMS)
	}
	if entry.DisconnectReason != "caller_bye" {
		t.Errorf("disconnect_reason: got %q, want caller_bye", entry.DisconnectReason)
	}

	// Verify JSON roundtrip.
	data, err := marshalLogEntry(entry)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded SBCLogEntry
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.CallID != call.CallID {
		t.Errorf("roundtrip call_id mismatch: got %q", decoded.CallID)
	}
}

func TestMetricsAccumulator(t *testing.T) {
	cfg := DefaultConfig()
	cfg.FailRatio = 0
	rng := rand.New(rand.NewSource(42))
	start := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	interval := 60 * time.Second

	acc := newMetricsAccumulator(start, interval)
	for i := 0; i < 10; i++ {
		call := synthesizeCall(cfg, start.Add(time.Duration(i)*time.Second), i, rng)
		acc.addCall(call)
	}

	snap := acc.snapshot()
	if snap.CallsStarted != 10 {
		t.Errorf("calls_started: got %d, want 10", snap.CallsStarted)
	}
	if snap.IntervalSec != 60 {
		t.Errorf("interval_sec: got %f, want 60", snap.IntervalSec)
	}
	if snap.CPS <= 0 {
		t.Error("CPS should be positive")
	}
	if snap.ASR <= 0 || snap.ASR > 1 {
		t.Errorf("ASR out of range: %f", snap.ASR)
	}
	if len(snap.Methods) == 0 {
		t.Error("methods should not be empty")
	}

	// Verify JSON serialization.
	data, err := marshalMetrics(snap)
	if err != nil {
		t.Fatalf("marshal metrics: %v", err)
	}
	var decoded MetricsSnapshot
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal metrics: %v", err)
	}
	if decoded.CallsStarted != 10 {
		t.Errorf("roundtrip calls_started: got %d", decoded.CallsStarted)
	}
}

func TestRunBackfill_FilesOnly(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "calls.jsonl")
	metricsPath := filepath.Join(dir, "metrics.jsonl")

	cfg := DefaultConfig()
	cfg.Duration = 10 * time.Second
	cfg.CPS = 5
	cfg.SBCLogFile = logPath
	cfg.SBCMetricsFile = metricsPath
	cfg.MetricsInterval = 5 * time.Second
	cfg.Progress = false

	ctx := context.Background()
	result, err := Run(ctx, cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if result.TotalCalls == 0 {
		t.Fatal("expected some calls")
	}
	if result.TotalLogs == 0 {
		t.Fatal("expected some log entries")
	}
	if result.TotalMetrics == 0 {
		t.Fatal("expected some metric snapshots")
	}
	if result.TotalHEP != 0 {
		t.Fatalf("expected no HEP packets (no hep_addr), got %d", result.TotalHEP)
	}

	// Verify log file is valid JSON-Lines.
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(logData)), "\n")
	if len(lines) != result.TotalLogs {
		t.Errorf("log lines: got %d, want %d", len(lines), result.TotalLogs)
	}
	for i, line := range lines {
		var entry SBCLogEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Errorf("log line %d: invalid JSON: %v", i, err)
		}
		if entry.CallID == "" {
			t.Errorf("log line %d: empty call_id", i)
		}
	}

	// Verify metrics file is valid JSON-Lines.
	metricsData, err := os.ReadFile(metricsPath)
	if err != nil {
		t.Fatalf("read metrics file: %v", err)
	}
	mlines := strings.Split(strings.TrimSpace(string(metricsData)), "\n")
	if len(mlines) != result.TotalMetrics {
		t.Errorf("metrics lines: got %d, want %d", len(mlines), result.TotalMetrics)
	}
	for i, line := range mlines {
		var snap MetricsSnapshot
		if err := json.Unmarshal([]byte(line), &snap); err != nil {
			t.Errorf("metrics line %d: invalid JSON: %v", i, err)
		}
	}
}

func TestRunBackfill_Cancellation(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Duration = 1 * time.Hour
	cfg.CPS = 100
	cfg.SBCLogFile = filepath.Join(t.TempDir(), "calls.jsonl")
	cfg.Progress = false

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := Run(ctx, cfg)
	if err == nil {
		t.Fatal("expected context error")
	}
}

func TestRunBackfill_TimestampsAreInThePast(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "calls.jsonl")

	now := time.Now()
	cfg := DefaultConfig()
	cfg.Duration = 1 * time.Minute
	cfg.CPS = 10
	cfg.SBCLogFile = logPath
	cfg.Progress = false

	ctx := context.Background()
	_, err := Run(ctx, cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	for i, line := range strings.Split(strings.TrimSpace(string(logData)), "\n") {
		var entry SBCLogEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("line %d: %v", i, err)
		}
		if entry.Timestamp.After(now) {
			t.Errorf("line %d: timestamp %v is in the future (now=%v)", i, entry.Timestamp, now)
		}
		expected := now.Add(-cfg.Duration)
		if entry.Timestamp.Before(expected.Add(-1 * time.Second)) {
			t.Errorf("line %d: timestamp %v is too far in the past (expected >= %v)", i, entry.Timestamp, expected)
		}
	}
}

func TestRunBackfill_HEPPacketEncoding(t *testing.T) {
	cfg := DefaultConfig()
	cfg.FailRatio = 0
	rng := rand.New(rand.NewSource(42))
	start := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)

	call := synthesizeCall(cfg, start, 0, rng)

	// Verify every message produces valid HEP.
	for i, msg := range call.Messages {
		msgTime := call.Start.Add(msg.Offset)
		if msgTime.Before(start) || msgTime.After(start.Add(cfg.CallDurationMax+5*time.Second)) {
			t.Errorf("message %d: timestamp %v outside expected range", i, msgTime)
		}
		if len(msg.Payload) == 0 {
			t.Errorf("message %d: empty payload", i)
		}
		// Verify it contains SIP/2.0 (request or response).
		payload := string(msg.Payload)
		if !strings.Contains(payload, "SIP/2.0") {
			t.Errorf("message %d: payload does not look like SIP: %.60s", i, payload)
		}
	}
}

func TestMetricsAccumulator_Reset(t *testing.T) {
	start := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	acc := newMetricsAccumulator(start, 60*time.Second)

	cfg := DefaultConfig()
	cfg.FailRatio = 0
	rng := rand.New(rand.NewSource(1))
	call := synthesizeCall(cfg, start, 0, rng)
	acc.addCall(call)

	if acc.started != 1 {
		t.Fatalf("started: got %d, want 1", acc.started)
	}

	newStart := start.Add(60 * time.Second)
	acc.reset(newStart)
	if acc.started != 0 {
		t.Fatalf("after reset: started should be 0, got %d", acc.started)
	}
	if acc.intervalStart != newStart {
		t.Fatal("reset did not update intervalStart")
	}
}
