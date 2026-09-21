package siplog

import (
	"bytes"
	"testing"
)

func TestAddRTPDisabledIsNoop(t *testing.T) {
	r := New(8)
	r.AddRTP("send", "10.0.0.1", 4000, "10.0.0.2", 5004, "cid", []byte{0x80, 0x08, 0x00, 0x01})
	if n := len(r.RTPPackets()); n != 0 {
		t.Fatalf("captured %d while off", n)
	}
}

func TestAddRTPStoresAndSummarizes(t *testing.T) {
	r := New(8)
	r.SetRTP(true)
	pkt := bytes.Repeat([]byte{0x80, 0x08, 0x00, 0x01, 0xaa}, 4)
	r.AddRTP("send", "10.0.0.1", 4000, "10.0.0.2", 5004, "cid", pkt)
	got := r.RTPPackets()
	if len(got) != 1 {
		t.Fatalf("packets %d", len(got))
	}
	if got[0].SrcPort != 4000 || got[0].DstPort != 5004 || !bytes.Equal(got[0].Payload, pkt) {
		t.Fatalf("%+v", got[0])
	}
	_, recs := r.Since(0, 10)
	if len(recs) != 1 || recs[0].Kind != KindRTP || recs[0].Method != "RTP" {
		t.Fatalf("summary %+v", recs)
	}
	r.Clear()
	if n := len(r.RTPPackets()); n != 0 {
		t.Fatalf("clear left %d", n)
	}
}

func TestAddRTPThrottlesSummaries(t *testing.T) {
	r := New(8)
	r.SetRTP(true)
	raw := []byte{0x80, 0x08, 0x00, 0x01}
	for i := 0; i < rtpSummaryEvery+1; i++ {
		r.AddRTP("send", "10.0.0.1", 4000, "10.0.0.2", 5004, "cid", raw)
	}
	if n := len(r.RTPPackets()); n != rtpSummaryEvery+1 {
		t.Fatalf("stored %d", n)
	}
	_, recs := r.Since(0, 100)
	if len(recs) != 2 {
		t.Fatalf("summaries %d want 2", len(recs))
	}
}
