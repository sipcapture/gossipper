package siplog

import (
	"archive/zip"
	"bytes"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcapgo"
)

func TestFilterScenario(t *testing.T) {
	recs := []Record{
		{Seq: 1, Scenario: "REGISTER", Raw: "REGISTER"},
		{Seq: 2, Scenario: "one_way_uas", Raw: "INVITE"},
	}
	got := FilterScenario(recs, "REGISTER")
	if len(got) != 1 || got[0].Seq != 1 {
		t.Fatalf("filter %+v", got)
	}
	if n := len(FilterScenario(recs, "")); n != 2 {
		t.Fatalf("empty filter n=%d", n)
	}
}

func TestFilterKind(t *testing.T) {
	recs := []Record{
		{Seq: 1, Kind: KindSIP, Raw: "INVITE"},
		{Seq: 2, Kind: KindApp, Method: "call.started", Raw: "call started"},
		{Seq: 3, Kind: KindRTP, Method: "RTP", Raw: "RTP send PT=8"},
	}
	onlySIP := FilterKind(recs, true, false, false)
	if len(onlySIP) != 1 || onlySIP[0].Seq != 1 {
		t.Fatalf("sip %+v", onlySIP)
	}
	onlyApp := FilterKind(recs, false, true, false)
	if len(onlyApp) != 1 || onlyApp[0].Seq != 2 {
		t.Fatalf("app %+v", onlyApp)
	}
	onlyRTP := FilterKind(recs, false, false, true)
	if len(onlyRTP) != 1 || onlyRTP[0].Seq != 3 {
		t.Fatalf("rtp %+v", onlyRTP)
	}
	if n := len(FilterKind(recs, true, true, true)); n != 3 {
		t.Fatalf("all n=%d", n)
	}
}

func TestFilterLevel(t *testing.T) {
	recs := []Record{
		{Seq: 1, Kind: KindSIP, Raw: "INVITE"},
		{Seq: 2, Kind: KindApp, Level: "debug", Raw: "cmd"},
		{Seq: 3, Kind: KindApp, Level: "info", Raw: "started"},
		{Seq: 4, Kind: KindApp, Level: "warn", Raw: "timeout"},
	}
	got := FilterLevel(recs, "info")
	if len(got) != 3 || got[0].Seq != 1 || got[1].Seq != 3 || got[2].Seq != 4 {
		t.Fatalf("%+v", got)
	}
}

func TestFormatText(t *testing.T) {
	if FormatText(nil) != "# no messages\n" {
		t.Fatalf("empty %q", FormatText(nil))
	}
	ts := time.Date(2026, 9, 20, 21, 0, 0, 0, time.UTC)
	out := FormatText([]Record{{
		Seq:      3,
		Ts:       ts,
		Dir:      "send",
		Peer:     "10.0.0.1:5060",
		Scenario: "REGISTER",
		Raw:      "REGISTER sip:ex SIP/2.0\r\nCall-ID: a1\r\n\r\n",
	}})
	if !strings.Contains(out, "======= #3 2026-09-20T21:00:00Z OUT 10.0.0.1:5060 REGISTER =======") {
		t.Fatalf("header %q", out)
	}
	if !strings.Contains(out, "REGISTER sip:ex SIP/2.0\nCall-ID: a1") {
		t.Fatalf("body %q", out)
	}
}

func TestWritePCAPDirections(t *testing.T) {
	ts := time.Date(2026, 9, 20, 21, 0, 1, 0, time.UTC)
	var buf bytes.Buffer
	err := WritePCAP(&buf, []Record{
		{Seq: 1, Ts: ts, Dir: "send", Peer: "10.0.0.1:5060", Raw: "REGISTER sip:ex SIP/2.0\r\n\r\n"},
		{Seq: 2, Ts: ts.Add(time.Second), Dir: "recv", Peer: "10.0.0.1:5060", Raw: "SIP/2.0 200 OK\r\n\r\n"},
	})
	if err != nil {
		t.Fatal(err)
	}
	r, err := pcapgo.NewReader(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if r.LinkType() != layers.LinkTypeEthernet {
		t.Fatalf("link %v", r.LinkType())
	}
	pkt, _, err := r.ReadPacketData()
	if err != nil {
		t.Fatal(err)
	}
	src, dst, payload := decodeIPv4UDP(t, pkt)
	if !src.Equal(pcapLocalIP) || !dst.Equal(net.IPv4(10, 0, 0, 1)) {
		t.Fatalf("send src=%v dst=%v", src, dst)
	}
	if !bytes.Contains(payload, []byte("REGISTER sip:ex")) {
		t.Fatalf("send payload %q", payload)
	}
	pkt, _, err = r.ReadPacketData()
	if err != nil {
		t.Fatal(err)
	}
	src, dst, payload = decodeIPv4UDP(t, pkt)
	if !src.Equal(net.IPv4(10, 0, 0, 1)) || !dst.Equal(pcapLocalIP) {
		t.Fatalf("recv src=%v dst=%v", src, dst)
	}
	if !bytes.Contains(payload, []byte("SIP/2.0 200 OK")) {
		t.Fatalf("recv payload %q", payload)
	}
	if _, _, err := r.ReadPacketData(); err != io.EOF {
		t.Fatalf("extra packet: %v", err)
	}
}

func TestParsePeerAddr(t *testing.T) {
	t.Parallel()
	ip, port := parsePeerAddr("10.0.0.1:5070")
	if !ip.Equal(net.IPv4(10, 0, 0, 1)) || port != 5070 {
		t.Fatalf("got %v:%d", ip, port)
	}
	_, port = parsePeerAddr("not-an-addr")
	if port != 5060 {
		t.Fatalf("fallback port %d", port)
	}
	_, port = parsePeerAddr("10.0.0.1:0")
	if port != 5060 {
		t.Fatalf("port 0 fallback %d", port)
	}
	_, port = parsePeerAddr("10.0.0.1:70000")
	if port != 5060 {
		t.Fatalf("overflow fallback %d", port)
	}
}

func TestWritePCAPAllMergesRTP(t *testing.T) {
	ts := time.Date(2026, 9, 21, 8, 0, 0, 0, time.UTC)
	rtp := []RTPPacket{{
		Ts:      ts.Add(500 * time.Millisecond),
		Dir:     "send",
		SrcIP:   net.IPv4(127, 0, 0, 1),
		SrcPort: 4000,
		DstIP:   net.IPv4(10, 0, 0, 2),
		DstPort: 5004,
		Payload: []byte{0x80, 0x08, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x11, 0x22, 0x33, 0x44, 0xd5},
	}}
	var buf bytes.Buffer
	err := WritePCAPAll(&buf, []Record{
		{Seq: 1, Ts: ts, Dir: "send", Peer: "10.0.0.1:5060", Kind: KindSIP, Raw: "INVITE sip:ex SIP/2.0\r\n\r\n"},
		{Seq: 2, Ts: ts.Add(time.Second), Dir: "recv", Peer: "10.0.0.1:5060", Kind: KindSIP, Raw: "SIP/2.0 200 OK\r\n\r\n"},
	}, rtp)
	if err != nil {
		t.Fatal(err)
	}
	r, err := pcapgo.NewReader(&buf)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	var sawRTP bool
	for {
		pkt, _, err := r.ReadPacketData()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		n++
		_, _, payload := decodeIPv4UDP(t, pkt)
		if bytes.Contains(payload, []byte{0x80, 0x08, 0x00, 0x01}) {
			sawRTP = true
		}
	}
	if n != 3 || !sawRTP {
		t.Fatalf("packets=%d rtp=%v", n, sawRTP)
	}
}

func TestWriteCallDumpZip(t *testing.T) {
	ts := time.Date(2026, 9, 21, 8, 0, 0, 0, time.UTC)
	var buf bytes.Buffer
	err := WriteCallDump(&buf, []Record{
		{Seq: 1, Ts: ts, Dir: "send", Peer: "10.0.0.1:5060", Kind: KindSIP, Scenario: "uas", Raw: "INVITE sip:ex SIP/2.0\r\n\r\n"},
		{Seq: 2, Ts: ts, Kind: KindApp, Method: "call.started", Level: "info", Raw: "call started"},
	}, []RTPPacket{{
		Ts:      ts,
		SrcIP:   net.IPv4(127, 0, 0, 1),
		SrcPort: 4000,
		DstIP:   net.IPv4(10, 0, 0, 2),
		DstPort: 5004,
		Payload: []byte{0x80, 0x08, 0x00, 0x01},
	}})
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		switch f.Name {
		case "call.log":
			if !strings.Contains(string(body), "INVITE sip:ex") || !strings.Contains(string(body), "call started") {
				t.Fatalf("log %q", body)
			}
		case "call.pcap":
			if len(body) < 24 {
				t.Fatalf("pcap too small %d", len(body))
			}
		}
	}
	if !names["README.txt"] || !names["call.log"] || !names["call.pcap"] {
		t.Fatalf("zip names %+v", names)
	}
}

func decodeIPv4UDP(t *testing.T, pkt []byte) (src, dst net.IP, payload []byte) {
	t.Helper()
	p := gopacket.NewPacket(pkt, layers.LayerTypeEthernet, gopacket.Default)
	ip, ok := p.Layer(layers.LayerTypeIPv4).(*layers.IPv4)
	if !ok {
		t.Fatalf("no ipv4: %v", p.ErrorLayer())
	}
	udp, ok := p.Layer(layers.LayerTypeUDP).(*layers.UDP)
	if !ok {
		t.Fatalf("no udp")
	}
	return ip.SrcIP, ip.DstIP, udp.Payload
}
