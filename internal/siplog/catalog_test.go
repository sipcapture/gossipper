package siplog

import (
	"bytes"
	"strings"
	"testing"
)

func TestCatalogSkipsRegisterAndIndexesInvite(t *testing.T) {
	r := New(8)
	r.SetCapture(false, false)
	r.AddTagged("send", "10.0.0.1:5060", "REGISTER", []byte("REGISTER sip:ex SIP/2.0\r\nCall-ID: reg-1\r\n\r\n"))
	invite := []byte("INVITE sip:1001@ex SIP/2.0\r\nFrom: <sip:alice@ex>;tag=a\r\nTo: <sip:1001@ex>\r\nCall-ID: cid-1@host\r\nCSeq: 1 INVITE\r\n\r\n")
	r.AddTagged("recv", "10.0.0.1:5060", "one_way_uas", invite)
	r.AddTagged("send", "10.0.0.1:5060", "one_way_uas", []byte("SIP/2.0 200 OK\r\nCall-ID: cid-1@host\r\nCSeq: 1 INVITE\r\n\r\n"))
	r.AddApp("one_way_uas", "call.ended", "call ended", "cid-1@host", "call.ended\ncall ended\nresult=success duration_ms=1200", "info")
	r.AddRTP("send", "192.168.1.10", 4000, "10.0.0.2", 5004, "cid-1@host", []byte{0x80, 0x00, 0x00, 0x01, 0xaa})

	_, live := r.Since(0, 20)
	if len(live) != 0 {
		t.Fatalf("live ring should stay empty when SIP/debug off, got %d", len(live))
	}
	if n := len(r.RTPPackets()); n != 0 {
		t.Fatalf("live RTP dump %d", n)
	}

	list := r.Calls().List()
	if len(list) != 1 {
		t.Fatalf("calls=%d want 1 (REGISTER skipped)", len(list))
	}
	row := list[0]
	if row.CallID != "cid-1@host" || row.From != "alice@ex" || row.To != "1001@ex" {
		t.Fatalf("ids %+v", row)
	}
	if row.Direction != "inbound" || row.Scenario != "one_way_uas" {
		t.Fatalf("meta %+v", row)
	}
	if row.SIPCount != 2 || row.SIPRecv != 1 || row.SIPSend != 1 {
		t.Fatalf("sip counts %+v", row)
	}
	if row.RTPSend != 1 || row.RTPDest != "10.0.0.2:5004" || row.Codec != "PCMU" {
		t.Fatalf("rtp %+v", row)
	}
	if row.State != "ended" || row.Result != "success" {
		t.Fatalf("end %+v", row)
	}

	d, ok := r.Calls().Get("cid-1@host")
	if !ok || len(d.SIP) != 2 || len(d.Debug) != 1 || len(d.RTP) != 1 {
		t.Fatalf("detail sip=%d debug=%d rtp=%d", len(d.SIP), len(d.Debug), len(d.RTP))
	}
	if !strings.Contains(d.SIP[0].Raw, "INVITE sip:1001") {
		t.Fatalf("sip raw %q", d.SIP[0].Raw)
	}

	recs, rtp, ok := r.Calls().Dump("cid-1@host")
	if !ok || len(recs) != 3 || len(rtp) != 1 {
		t.Fatalf("dump recs=%d rtp=%d ok=%v", len(recs), len(rtp), ok)
	}
	if !bytes.Equal(rtp[0].Payload, []byte{0x80, 0x00, 0x00, 0x01, 0xaa}) {
		t.Fatalf("dump payload %x", rtp[0].Payload)
	}
	if _, _, ok := r.Calls().Dump("nope"); ok {
		t.Fatal("missing dump")
	}
}

func TestCatalogClearIndependentOfLiveClear(t *testing.T) {
	r := New(8)
	r.AddTagged("recv", "1.1.1.1:5060", "uas", []byte("INVITE sip:x SIP/2.0\r\nCall-ID: keep\r\n\r\n"))
	r.Clear()
	if _, recs := r.Since(0, 10); len(recs) != 0 {
		t.Fatalf("live leftover %d", len(recs))
	}
	if n := len(r.Calls().List()); n != 1 {
		t.Fatalf("catalog cleared by live Clear: %d", n)
	}
	r.Calls().Clear()
	if n := len(r.Calls().List()); n != 0 {
		t.Fatalf("after catalog clear %d", n)
	}
}
