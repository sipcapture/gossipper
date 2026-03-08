package stats

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/adubovikov/gossipper/internal/media"
)

func TestCollectorAggregatesMediaStats(t *testing.T) {
	t.Parallel()

	collector := New()
	collector.StartCall()
	collector.AddMediaStats(media.Stats{
		RTPPacketsSent:      10,
		RTPOctetsSent:       1600,
		RTPPacketsReceived:  4,
		RTCPSenderReports:   2,
		RTCPReceiverReports: 1,
		RTCPPacketsReceived: 3,
	})
	collector.FinishCall(true, 25*time.Millisecond)

	summary := collector.Snapshot()
	if summary.SuccessCalls != 1 {
		t.Fatalf("expected one successful call, got %+v", summary)
	}
	if summary.Media.RTPPacketsSent != 10 || summary.Media.RTCPReceiverReports != 1 {
		t.Fatalf("unexpected media summary: %+v", summary.Media)
	}
}

func TestCollectorWriteJSONIncludesMediaStats(t *testing.T) {
	t.Parallel()

	collector := New()
	collector.StartCall()
	collector.AddMediaStats(media.Stats{
		RTPPacketsSent:      7,
		RTPOctetsSent:       1120,
		RTCPSenderReports:   2,
		RTCPPacketsReceived: 2,
	})
	collector.FinishCall(true, 10*time.Millisecond)

	path := filepath.Join(t.TempDir(), "summary.json")
	if err := collector.WriteJSON(path); err != nil {
		t.Fatalf("WriteJSON() error = %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(summary) error = %v", err)
	}

	var summary Summary
	if err := json.Unmarshal(raw, &summary); err != nil {
		t.Fatalf("Unmarshal(summary) error = %v", err)
	}
	if summary.Media.RTPPacketsSent != 7 {
		t.Fatalf("expected media stats in JSON, got %+v", summary.Media)
	}
	if summary.Media.RTCPSenderReports != 2 || summary.Media.RTCPPacketsReceived != 2 {
		t.Fatalf("unexpected RTCP stats in JSON: %+v", summary.Media)
	}
}

func TestCollectorAggregatesRTDStats(t *testing.T) {
	t.Parallel()

	collector := New()
	collector.AddRTD("invite", 10*time.Millisecond)
	collector.AddRTD("invite", 20*time.Millisecond)

	summary := collector.Snapshot()
	invite, ok := summary.RTD["invite"]
	if !ok {
		t.Fatalf("expected invite RTD summary, got %+v", summary.RTD)
	}
	if invite.Count != 2 {
		t.Fatalf("expected RTD count 2, got %+v", invite)
	}
	if invite.Min != 10*time.Millisecond || invite.Max != 20*time.Millisecond || invite.Last != 20*time.Millisecond {
		t.Fatalf("unexpected RTD bounds: %+v", invite)
	}
	if invite.Average != 15*time.Millisecond {
		t.Fatalf("expected RTD average 15ms, got %+v", invite)
	}
}

func TestCollectorWriteJSONIncludesRTDStats(t *testing.T) {
	t.Parallel()

	collector := New()
	collector.AddRTD("invite", 12*time.Millisecond)
	collector.AddRTD("invite", 18*time.Millisecond)

	path := filepath.Join(t.TempDir(), "summary.json")
	if err := collector.WriteJSON(path); err != nil {
		t.Fatalf("WriteJSON() error = %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(summary) error = %v", err)
	}

	var summary Summary
	if err := json.Unmarshal(raw, &summary); err != nil {
		t.Fatalf("Unmarshal(summary) error = %v", err)
	}
	invite, ok := summary.RTD["invite"]
	if !ok {
		t.Fatalf("expected invite RTD stats in JSON, got %+v", summary.RTD)
	}
	if invite.Count != 2 || invite.Average != 15*time.Millisecond {
		t.Fatalf("unexpected RTD stats in JSON: %+v", invite)
	}
}

func TestCollectorAggregatesCounterAndDisplayStats(t *testing.T) {
	t.Parallel()

	collector := New()
	collector.AddCounter("invite_sent")
	collector.AddCounter("invite_sent")
	collector.AddDisplay("Invite sent")

	summary := collector.Snapshot()
	if summary.Counters["invite_sent"] != 2 {
		t.Fatalf("expected counter aggregation, got %+v", summary.Counters)
	}
	if summary.Displays["Invite sent"] != 1 {
		t.Fatalf("expected display aggregation, got %+v", summary.Displays)
	}
}

func TestCollectorWriteJSONIncludesCounterAndDisplayStats(t *testing.T) {
	t.Parallel()

	collector := New()
	collector.AddCounter("invite_sent")
	collector.AddDisplay("Invite sent")

	path := filepath.Join(t.TempDir(), "summary.json")
	if err := collector.WriteJSON(path); err != nil {
		t.Fatalf("WriteJSON() error = %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(summary) error = %v", err)
	}

	var summary Summary
	if err := json.Unmarshal(raw, &summary); err != nil {
		t.Fatalf("Unmarshal(summary) error = %v", err)
	}
	if summary.Counters["invite_sent"] != 1 {
		t.Fatalf("expected counters in JSON, got %+v", summary.Counters)
	}
	if summary.Displays["Invite sent"] != 1 {
		t.Fatalf("expected displays in JSON, got %+v", summary.Displays)
	}
}

func TestCollectorWriteJSONIncludesFailureClasses(t *testing.T) {
	t.Parallel()

	collector := New()
	collector.AddFailureClass("timeout")
	collector.AddFailureClass("timeout")
	collector.AddFailureClass("transport_error")

	path := filepath.Join(t.TempDir(), "summary.json")
	if err := collector.WriteJSON(path); err != nil {
		t.Fatalf("WriteJSON() error = %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(summary) error = %v", err)
	}

	var summary Summary
	if err := json.Unmarshal(raw, &summary); err != nil {
		t.Fatalf("Unmarshal(summary) error = %v", err)
	}
	if summary.FailureClasses["timeout"] != 2 || summary.FailureClasses["transport_error"] != 1 {
		t.Fatalf("expected failure classes in JSON, got %+v", summary.FailureClasses)
	}
}
