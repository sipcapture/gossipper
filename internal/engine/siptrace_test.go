package engine

import (
	"testing"

	"github.com/sipcapture/gossipper/internal/eventlog"
	"github.com/sipcapture/gossipper/internal/scenario"
	"github.com/sipcapture/gossipper/internal/siplog"
	"github.com/sipcapture/gossipper/internal/transport"
)

func TestSIPTraceRecordsWithoutTraceMessages(t *testing.T) {
	ring := siplog.New(8)
	e := New(Config{
		Scenario: scenario.Scenario{Name: "uac_basic"},
		SIPLog:   ring,
	})
	send := e.wrapSIPSend(1, "cid", "127.0.0.1", 5060, "10.0.0.1", 5060, func([]byte) error { return nil })
	payload := []byte("INVITE sip:1001@ex SIP/2.0\r\nCall-ID: cid\r\n\r\n")
	if err := send(payload); err != nil {
		t.Fatal(err)
	}
	_, recs := ring.Since(0, 10)
	if len(recs) != 1 {
		t.Fatalf("recs=%d", len(recs))
	}
	if recs[0].Dir != "send" || recs[0].Scenario != "uac_basic" || recs[0].Method != "INVITE" {
		t.Fatalf("%+v", recs[0])
	}
	if recs[0].Peer != "10.0.0.1:5060" {
		t.Fatalf("peer %q", recs[0].Peer)
	}
}

func TestSIPTraceCaptureOffSkipsSIP(t *testing.T) {
	ring := siplog.New(8)
	ring.SetCapture(false, false)
	e := New(Config{
		Scenario: scenario.Scenario{Name: "uac_basic"},
		SIPLog:   ring,
	})
	send := e.wrapSIPSend(1, "cid", "127.0.0.1", 5060, "10.0.0.1", 5060, func([]byte) error { return nil })
	if err := send([]byte("INVITE sip:1001@ex SIP/2.0\r\nCall-ID: cid\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	_, recs := ring.Since(0, 10)
	if len(recs) != 0 {
		t.Fatalf("expected no SIP while capture off, got %+v", recs)
	}
}

func TestAppTraceRecordsCallEvents(t *testing.T) {
	ring := siplog.New(8)
	e := New(Config{
		Scenario: scenario.Scenario{Name: "uac_basic"},
		SIPLog:   ring,
	})
	ring.SetCapture(false, true)
	e.emitEvent(eventlog.Event{
		Kind:  eventlog.KindCallStart,
		Msg:   "call started",
		Attrs: map[string]any{"call_id": "cid"},
	})
	e.emitEvent(eventlog.Event{Kind: eventlog.KindSIPSend, Msg: "INVITE"})
	_, recs := ring.Since(0, 10)
	if len(recs) != 1 || !recs[0].IsApp() || recs[0].Scenario != "uac_basic" || recs[0].CallID != "cid" {
		t.Fatalf("%+v", recs)
	}
	if recs[0].Method != eventlog.KindCallStart {
		t.Fatalf("method %q", recs[0].Method)
	}
}

func TestAppTraceMinLevelDropsDebug(t *testing.T) {
	ring := siplog.New(8)
	e := New(Config{
		Scenario: scenario.Scenario{Name: "uac_basic"},
		SIPLog:   ring,
	})
	ring.SetCapture(false, true)
	if !ring.SetMinLevel("info") {
		t.Fatal("set info")
	}
	e.traceScenarioCmd(scenario.Command{Type: scenario.CommandSend, Index: 1}, 1, "cid")
	e.emitEvent(eventlog.Event{
		Level: eventlog.LevelInfo,
		Kind:  eventlog.KindCallStart,
		Msg:   "call started",
		Attrs: map[string]any{"call_id": "cid"},
	})
	_, recs := ring.Since(0, 10)
	if len(recs) != 1 || recs[0].Level != "info" || recs[0].Method != eventlog.KindCallStart {
		t.Fatalf("%+v", recs)
	}
}

func TestSIPTraceSharedRingFromTwoScenarios(t *testing.T) {
	ring := siplog.New(8)
	a := New(Config{Scenario: scenario.Scenario{Name: "one_way"}, SIPLog: ring})
	b := New(Config{Scenario: scenario.Scenario{Name: "no_rtp"}, SIPLog: ring})
	sendA := a.wrapSIPSend(1, "a", "127.0.0.1", 5060, "10.0.0.1", 5060, func([]byte) error { return nil })
	sendB := b.wrapSIPSend(1, "b", "127.0.0.1", 5060, "10.0.0.2", 5060, func([]byte) error { return nil })
	if err := sendA([]byte("INVITE sip:a@ex SIP/2.0\r\nCall-ID: a\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	if err := sendB([]byte("INVITE sip:b@ex SIP/2.0\r\nCall-ID: b\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	_, recs := ring.Since(0, 10)
	if len(recs) != 2 {
		t.Fatalf("recs=%d", len(recs))
	}
	if recs[0].Scenario != "one_way" || recs[1].Scenario != "no_rtp" {
		t.Fatalf("scenarios %q %q", recs[0].Scenario, recs[1].Scenario)
	}
}

func TestSIPTraceScenarioFromInviteResolver(t *testing.T) {
	ring := siplog.New(4)
	e := New(Config{Scenario: scenario.Scenario{Name: "fallback"}, SIPLog: ring})
	e.SetInviteScenarioResolver(func(user string) (scenario.Scenario, bool) {
		if user == "1001" {
			return scenario.Scenario{Name: "one_way_uas"}, true
		}
		return scenario.Scenario{}, false
	})
	if got := e.sipTraceScenario([]byte("INVITE sip:1001@pbx SIP/2.0\r\n\r\n")); got != "one_way_uas" {
		t.Fatalf("invite user got %q", got)
	}
	if got := e.sipTraceScenario([]byte("REGISTER sip:pbx SIP/2.0\r\n\r\n")); got != "REGISTER" {
		t.Fatalf("register got %q", got)
	}
	if got := e.sipTraceScenario([]byte("SIP/2.0 200 OK\r\n\r\n")); got != "fallback" {
		t.Fatalf("response got %q", got)
	}
}

func TestSIPTraceSkipsWrapWhenUASUDPTapped(t *testing.T) {
	ring := siplog.New(4)
	e := New(Config{Scenario: scenario.Scenario{Name: "uac"}, SIPLog: ring})
	e.setUASUDP(&transport.SharedUDP{})
	send := e.wrapSIPSend(1, "cid", "127.0.0.1", 5060, "10.0.0.1", 5060, func([]byte) error { return nil })
	if err := send([]byte("INVITE sip:1001@ex SIP/2.0\r\nCall-ID: cid\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	_, recs := ring.Since(0, 10)
	if len(recs) != 0 {
		t.Fatalf("expected UDP tap to own the ring, got %+v", recs)
	}
}
