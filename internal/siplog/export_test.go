package siplog

import (
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
	}
	onlySIP := FilterKind(recs, true, false)
	if len(onlySIP) != 1 || onlySIP[0].Seq != 1 {
		t.Fatalf("sip %+v", onlySIP)
	}
	onlyApp := FilterKind(recs, false, true)
	if len(onlyApp) != 1 || onlyApp[0].Seq != 2 {
		t.Fatalf("app %+v", onlyApp)
	}
	if n := len(FilterKind(recs, true, true)); n != 2 {
		t.Fatalf("both n=%d", n)
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
