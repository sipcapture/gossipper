package stats

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sipcapture/gossipper/internal/media"
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
	if err := collector.WriteJSON(path, nil); err != nil {
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

func TestCollectorAggregatesRTCPReceptionQoS(t *testing.T) {
	t.Parallel()
	collector := New()
	collector.AddMediaStats(media.Stats{
		RTCPReportBlocks:    2,
		RTCPMaxFractionLost: 10,
		RTCPMaxJitter:       100,
		RTCPMinJitter:       80,
		RTCPJitterSum:       30,
		RTCPJitterSamples:   2,
	})
	collector.AddMediaStats(media.Stats{
		RTCPReportBlocks:    1,
		RTCPMaxFractionLost: 20,
		RTCPMaxJitter:       50,
		RTCPMinJitter:       30,
		RTCPJitterSum:       40,
		RTCPJitterSamples:   1,
	})
	summary := collector.Snapshot()
	if summary.Media.RTCPReceptionReports != 3 {
		t.Fatalf("reports: %+v", summary.Media.RTCPReceptionReports)
	}
	wantLoss := 20.0 / 256.0
	if summary.Media.RTCPMaxFractionLost < wantLoss-1e-9 || summary.Media.RTCPMaxFractionLost > wantLoss+1e-9 {
		t.Fatalf("max fraction lost: got %v want %v", summary.Media.RTCPMaxFractionLost, wantLoss)
	}
	if summary.Media.RTCPMaxJitterTS != 100 {
		t.Fatalf("max jitter: %d", summary.Media.RTCPMaxJitterTS)
	}
	if summary.Media.RTCPMinJitterTS != 30 {
		t.Fatalf("min jitter: %d", summary.Media.RTCPMinJitterTS)
	}
	if summary.Media.RTCPAvgJitterTS < 23.32 || summary.Media.RTCPAvgJitterTS > 23.34 {
		t.Fatalf("avg jitter: %v", summary.Media.RTCPAvgJitterTS)
	}
}

func TestCollectorAggregatesRTPRecvPathQoS(t *testing.T) {
	t.Parallel()
	collector := New()
	collector.AddMediaStats(media.Stats{
		RTPRecvMaxCumulativeLost:      5,
		RTPRecvInterarrivalJitterPeak: 42,
	})
	collector.AddMediaStats(media.Stats{
		RTPRecvMaxCumulativeLost:      2,
		RTPRecvInterarrivalJitterPeak: 100,
	})
	summary := collector.Snapshot()
	if summary.Media.RTPRecvMaxCumulativeLost != 5 {
		t.Fatalf("max cumulative lost: %d", summary.Media.RTPRecvMaxCumulativeLost)
	}
	if summary.Media.RTPRecvInterarrivalJitterPeakTS != 100 {
		t.Fatalf("peak interarrival jitter: %d", summary.Media.RTPRecvInterarrivalJitterPeakTS)
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
	if err := collector.WriteJSON(path, nil); err != nil {
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
	if err := collector.WriteJSON(path, nil); err != nil {
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
	if err := collector.WriteJSON(path, nil); err != nil {
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

func TestCollectorLatencyRepartitionSummary(t *testing.T) {
	t.Parallel()

	collector := New()
	collector.StartCall()
	collector.FinishCall(true, 10*time.Millisecond)
	collector.StartCall()
	collector.FinishCall(true, 20*time.Millisecond)
	collector.AddInviteLatency(12 * time.Millisecond)
	collector.AddInviteLatency(18 * time.Millisecond)
	collector.AddRTD("invite", 12*time.Millisecond)
	collector.AddRTD("invite", 18*time.Millisecond)

	summary := collector.Snapshot()
	if summary.CallLength == nil || summary.InviteRTT == nil {
		t.Fatalf("expected call length and invite RTT latency summaries, got %+v", summary)
	}
	if summary.CallLength.StdDev != 5*time.Millisecond {
		t.Fatalf("expected call length stddev 5ms, got %+v", summary.CallLength)
	}
	if summary.InviteRTT.StdDev != 3*time.Millisecond {
		t.Fatalf("expected invite RTT stddev 3ms, got %+v", summary.InviteRTT)
	}
	if len(summary.CallLength.Buckets) == 0 || summary.CallLength.Buckets[0].Count != 1 || summary.CallLength.Buckets[1].Count != 1 {
		t.Fatalf("expected call length repartition buckets, got %+v", summary.CallLength.Buckets)
	}
	invite, ok := summary.RTD["invite"]
	if !ok {
		t.Fatalf("expected RTD latency summary, got %+v", summary.RTD)
	}
	if invite.StdDev != 3*time.Millisecond {
		t.Fatalf("expected RTD stddev 3ms, got %+v", invite)
	}
}

func TestHealthMinRTPPacketsRecvPerCall(t *testing.T) {
	t.Parallel()
	cfg := HealthConfig{HealthMinRTPPacketsRecvPerCall: 100}
	if !cfg.Active() {
		t.Fatal("expected active")
	}
	s := Summary{Media: MediaSummary{CallsWithRTPReceived: 2, PerCallMinRTPPacketsReceived: 50}}
	h, _ := EvaluateHealth(cfg, s)
	if h == nil || h.Pass {
		t.Fatalf("expected fail, got %+v", h)
	}
}

func TestCollectorPerCallMinRTPRecv(t *testing.T) {
	t.Parallel()
	collector := New()
	collector.AddMediaStats(media.Stats{RTPPacketsReceived: 200})
	collector.AddMediaStats(media.Stats{RTPPacketsReceived: 50})
	collector.AddMediaStats(media.Stats{RTPPacketsReceived: 0})
	s := collector.Snapshot()
	if s.Media.CallsWithRTPReceived != 2 {
		t.Fatalf("calls with recv: %d", s.Media.CallsWithRTPReceived)
	}
	if s.Media.PerCallMinRTPPacketsReceived != 50 {
		t.Fatalf("per-call min: %d", s.Media.PerCallMinRTPPacketsReceived)
	}
}
