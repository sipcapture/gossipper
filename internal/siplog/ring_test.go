package siplog

import (
	"strings"
	"testing"
	"time"
)

func TestRingSinceAndClear(t *testing.T) {
	r := New(3)
	r.Add("send", "1.2.3.4:5060", []byte("REGISTER sip:ex SIP/2.0\r\nCall-ID: abc\r\n\r\n"))
	r.Add("recv", "1.2.3.4:5060", []byte("SIP/2.0 401 Unauthorized\r\nCall-ID: abc\r\n\r\n"))
	r.Add("send", "1.2.3.4:5060", []byte("INVITE sip:ex SIP/2.0\r\nCall-ID: xyz\r\n\r\n"))
	r.Add("recv", "1.2.3.4:5060", []byte("SIP/2.0 200 OK\r\nCall-ID: xyz\r\n\r\n"))

	next, recs := r.Since(0, 10)
	if next != 4 {
		t.Fatalf("next=%d want 4", next)
	}
	if len(recs) != 3 {
		t.Fatalf("capped recs=%d want 3", len(recs))
	}
	if recs[0].Seq != 2 || recs[0].Dir != "recv" || recs[0].Status != 401 {
		t.Fatalf("oldest kept %+v", recs[0])
	}
	if recs[2].Method != "" || recs[2].Status != 200 {
		t.Fatalf("last %+v", recs[2])
	}

	next2, recs2 := r.Since(next, 10)
	if next2 != next || len(recs2) != 0 {
		t.Fatalf("incremental since=%d next=%d recs=%d", next, next2, len(recs2))
	}

	r.Clear()
	r.Add("recv", "9.9.9.9:5060", []byte("OPTIONS sip:ex SIP/2.0\r\nCall-ID: ping\r\n\r\n"))
	_, recs3 := r.Since(next, 10)
	if len(recs3) != 1 || recs3[0].Method != "OPTIONS" || recs3[0].CallID != "ping" {
		t.Fatalf("after clear %+v", recs3)
	}
}

func TestSummarizeFirstLine(t *testing.T) {
	r := New(1)
	r.Add("send", "", []byte("REGISTER sip:a@b SIP/2.0\r\nCall-ID: x\r\n\r\n"))
	_, recs := r.Since(0, 1)
	if recs[0].Summary != "REGISTER sip:a@b SIP/2.0" {
		t.Fatalf("summary %q", recs[0].Summary)
	}
	if recs[0].Method != "REGISTER" {
		t.Fatalf("method %q", recs[0].Method)
	}
	if !strings.Contains(recs[0].Raw, "Call-ID: x") {
		t.Fatalf("raw missing header")
	}
}

func TestAddTaggedScenario(t *testing.T) {
	r := New(2)
	r.AddTagged("send", "1.2.3.4:5060", "one_way", []byte("INVITE sip:ex SIP/2.0\r\nCall-ID: z\r\n\r\n"))
	r.AddTagged("recv", "1.2.3.4:5060", "no_rtp", []byte("SIP/2.0 200 OK\r\nCall-ID: z\r\n\r\n"))
	_, recs := r.Since(0, 10)
	if len(recs) != 2 {
		t.Fatalf("recs=%d", len(recs))
	}
	if recs[0].Scenario != "one_way" || recs[1].Scenario != "no_rtp" {
		t.Fatalf("scenarios %+v %+v", recs[0], recs[1])
	}
	if recs[0].Kind != KindSIP || !recs[0].IsSIP() {
		t.Fatalf("kind %+v", recs[0])
	}
}

func TestCaptureGatesSIPAndApp(t *testing.T) {
	r := New(8)
	sip, app := r.Capture()
	if !sip || app {
		t.Fatalf("defaults sip=%v app=%v", sip, app)
	}
	r.SetCapture(false, false)
	r.AddTagged("send", "1.2.3.4:5060", "one_way", []byte("INVITE sip:ex SIP/2.0\r\n\r\n"))
	r.AddApp("one_way", "call.started", "call started", "cid", "call started", "info")
	_, recs := r.Since(0, 10)
	if len(recs) != 0 {
		t.Fatalf("both off recs=%d", len(recs))
	}
	r.SetCapture(false, true)
	r.AddApp("one_way", "scenario.cmd", "send INVITE", "cid", "send", "debug")
	r.AddTagged("send", "1.2.3.4:5060", "one_way", []byte("INVITE sip:ex SIP/2.0\r\n\r\n"))
	_, recs = r.Since(0, 10)
	if len(recs) != 1 || !recs[0].IsApp() || recs[0].Method != "scenario.cmd" {
		t.Fatalf("app only %+v", recs)
	}
	r.SetCapture(true, false)
	r.AddTagged("recv", "1.2.3.4:5060", "one_way", []byte("SIP/2.0 200 OK\r\n\r\n"))
	r.AddApp("one_way", "call.ended", "call ended", "cid", "ended", "info")
	_, recs = r.Since(recs[0].Seq, 10)
	if len(recs) != 1 || !recs[0].IsSIP() || recs[0].Status != 200 {
		t.Fatalf("sip only %+v", recs)
	}
}

func TestMinLevelGatesApp(t *testing.T) {
	r := New(8)
	r.SetCapture(false, true)
	if !r.SetMinLevel("info") || r.MinLevel() != "info" {
		t.Fatalf("min %q", r.MinLevel())
	}
	r.AddApp("one_way", "scenario.cmd", "send", "cid", "send", "debug")
	r.AddApp("one_way", "call.started", "call started", "cid", "started", "info")
	r.AddApp("one_way", "timeout", "recv timeout", "cid", "timeout", "warn")
	_, recs := r.Since(0, 10)
	if len(recs) != 2 || recs[0].Level != "info" || recs[1].Level != "warn" {
		t.Fatalf("%+v", recs)
	}
	if r.SetMinLevel("nope") {
		t.Fatal("invalid level accepted")
	}
}

func TestSubscribeWakesOnAddAndClear(t *testing.T) {
	r := New(4)
	ch, cancel := r.Subscribe()
	defer cancel()
	r.AddTagged("recv", "1.1.1.1:5060", "REGISTER", []byte("OPTIONS sip:ex SIP/2.0\r\nCall-ID: p\r\n\r\n"))
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("add did not notify")
	}
	_, gen1, _ := r.Snapshot(0, 10)
	r.Clear()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("clear did not notify")
	}
	_, gen2, recs := r.Snapshot(0, 10)
	if gen2 <= gen1 || len(recs) != 0 {
		t.Fatalf("after clear gen=%d->%d recs=%d", gen1, gen2, len(recs))
	}
}
