package siplog

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestCatalogSQLiteSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "calls.sqlite")
	r := New(8)
	r.SetCapture(false, false)
	if err := r.OpenPersist(path); err != nil {
		t.Fatalf("OpenPersist: %v", err)
	}
	invite := []byte("INVITE sip:1001@ex SIP/2.0\r\nFrom: <sip:alice@ex>;tag=a\r\nTo: <sip:1001@ex>\r\nCall-ID: cid-persist@host\r\nCSeq: 1 INVITE\r\n\r\n")
	r.AddTagged("recv", "10.0.0.1:5060", "one_way_uas", invite)
	r.AddTagged("send", "10.0.0.1:5060", "one_way_uas", []byte("SIP/2.0 200 OK\r\nCall-ID: cid-persist@host\r\nCSeq: 1 INVITE\r\n\r\n"))
	r.AddApp("one_way_uas", "call.ended", "call ended", "cid-persist@host", "call.ended\ncall ended\nresult=success duration_ms=1200", "info")
	r.AddRTP("send", "192.168.1.10", 4000, "10.0.0.2", 5004, "cid-persist@host", []byte{0x80, 0x00, 0x00, 0x01, 0xaa})
	r.Calls().Flush()
	r.ClosePersist()

	r2 := New(8)
	if err := r2.OpenPersist(path); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(r2.ClosePersist)

	list := r2.Calls().List()
	if len(list) != 1 {
		t.Fatalf("list=%d want 1 after reopen", len(list))
	}
	row := list[0]
	if row.CallID != "cid-persist@host" || row.From != "alice@ex" || row.To != "1001@ex" {
		t.Fatalf("ids %+v", row)
	}
	if row.State != "ended" || row.Result != "success" {
		t.Fatalf("end %+v", row)
	}
	if row.SIPCount != 2 || row.RTPSend != 1 || row.Codec != "PCMU" {
		t.Fatalf("counts %+v", row)
	}

	d, ok := r2.Calls().Get("cid-persist@host")
	if !ok || len(d.SIP) != 2 || len(d.Debug) != 1 || len(d.RTP) != 1 {
		t.Fatalf("detail sip=%d debug=%d rtp=%d ok=%v", len(d.SIP), len(d.Debug), len(d.RTP), ok)
	}
	if !strings.Contains(d.SIP[0].Raw, "INVITE sip:1001") {
		t.Fatalf("sip raw %q", d.SIP[0].Raw)
	}

	recs, rtp, ok := r2.Calls().Dump("cid-persist@host")
	if !ok || len(recs) != 3 || len(rtp) != 1 {
		t.Fatalf("dump recs=%d rtp=%d ok=%v", len(recs), len(rtp), ok)
	}
	if !bytes.Equal(rtp[0].Payload, []byte{0x80, 0x00, 0x00, 0x01, 0xaa}) {
		t.Fatalf("dump payload %x", rtp[0].Payload)
	}
}

func TestCatalogSQLiteClearWipesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "calls.sqlite")
	r := New(8)
	if err := r.OpenPersist(path); err != nil {
		t.Fatalf("OpenPersist: %v", err)
	}
	t.Cleanup(r.ClosePersist)
	r.AddTagged("recv", "1.1.1.1:5060", "uas", []byte("INVITE sip:x SIP/2.0\r\nCall-ID: wipe-me\r\n\r\n"))
	r.Calls().Flush()
	if n := len(r.Calls().List()); n != 1 {
		t.Fatalf("before clear %d", n)
	}
	r.Calls().Clear()
	if n := len(r.Calls().List()); n != 0 {
		t.Fatalf("after clear %d", n)
	}

	r2 := New(8)
	if err := r2.OpenPersist(path); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(r2.ClosePersist)
	if n := len(r2.Calls().List()); n != 0 {
		t.Fatalf("reopen after clear %d", n)
	}
}
