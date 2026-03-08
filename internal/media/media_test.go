package media

import (
	"context"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/adubovikov/gossipper/internal/sip"
	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcapgo"
	"github.com/pion/rtcp"
)

func TestBuildSilentPCMU(t *testing.T) {
	t.Parallel()

	packet, err := BuildSilentPCMU(StreamConfig{
		PayloadType: 0,
		SSRC:        42,
		Sequence:    1,
		Timestamp:   160,
	}, 160)
	if err != nil {
		t.Fatalf("BuildSilentPCMU() error = %v", err)
	}
	if len(packet) == 0 {
		t.Fatal("expected RTP payload")
	}
}

func TestParseAudioEndpoint(t *testing.T) {
	t.Parallel()

	msg := sip.Message{
		Body: "v=0\r\nc=IN IP4 127.0.0.1\r\nm=audio 40000 RTP/AVP 0\r\n",
	}
	ep, err := ParseAudioEndpoint(msg, "127.0.0.1")
	if err != nil {
		t.Fatalf("ParseAudioEndpoint() error = %v", err)
	}
	if ep.IP != "127.0.0.1" || ep.Port != 40000 {
		t.Fatalf("unexpected endpoint: %+v", ep)
	}
}

func TestParseRTPStreamSpecPayloadParams(t *testing.T) {
	t.Parallel()

	command, cfg, err := ParseRTPStreamSpec("audio.wav,-1,8,PCMA/8000", ".")
	if err != nil {
		t.Fatalf("ParseRTPStreamSpec() error = %v", err)
	}
	if command != "start" {
		t.Fatalf("expected start command, got %q", command)
	}
	if cfg.LoopCount != -1 || cfg.PayloadType != 8 || cfg.PayloadName != "PCMA/8000" {
		t.Fatalf("unexpected cfg: %+v", cfg)
	}
}

func TestSessionEchoAndRTCPStats(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	session := NewSession()
	defer session.Stop()
	if err := session.StartEcho(ctx, "127.0.0.1", 0); err != nil {
		t.Fatalf("StartEcho() error = %v", err)
	}

	rtpPort, rtcpPort := session.Ports()
	if rtpPort == 0 || rtcpPort == 0 {
		t.Fatalf("expected bound RTP/RTCP ports, got %d/%d", rtpPort, rtcpPort)
	}

	clientConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP(client) error = %v", err)
	}
	defer clientConn.Close()

	packet, err := BuildPacket(StreamConfig{
		PayloadType: 0,
		SSRC:        42,
		Sequence:    1,
		Timestamp:   160,
	}, []byte{1, 2, 3, 4})
	if err != nil {
		t.Fatalf("BuildPacket() error = %v", err)
	}
	if _, err := clientConn.WriteToUDP(packet, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: rtpPort}); err != nil {
		t.Fatalf("WriteToUDP(rtp) error = %v", err)
	}

	echoBuf := make([]byte, 1500)
	_ = clientConn.SetReadDeadline(time.Now().Add(time.Second))
	n, _, err := clientConn.ReadFromUDP(echoBuf)
	if err != nil {
		t.Fatalf("ReadFromUDP(echo) error = %v", err)
	}
	if string(echoBuf[:n]) != string(packet) {
		t.Fatalf("unexpected echo payload")
	}

	rtcpClient, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP(rtcp client) error = %v", err)
	}
	defer rtcpClient.Close()
	rr := &rtcp.ReceiverReport{SSRC: 42}
	raw, err := rr.Marshal()
	if err != nil {
		t.Fatalf("Marshal(receiver report) error = %v", err)
	}
	if _, err := rtcpClient.WriteToUDP(raw, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: rtcpPort}); err != nil {
		t.Fatalf("WriteToUDP(rtcp) error = %v", err)
	}

	deadline := time.Now().Add(time.Second)
	for {
		stats := session.Snapshot()
		if stats.RTPPacketsReceived > 0 && stats.RTPPacketsSent > 0 && stats.RTCPReceiverReports > 0 && stats.RTCPPacketsReceived > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected RTP/RTCP stats, got %+v", stats)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestSessionPCAPReplay(t *testing.T) {
	t.Parallel()

	pcapPath := writeTestPCAP(t)

	rtpConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP(rtp) error = %v", err)
	}
	defer rtpConn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	session := NewSession()
	defer session.Stop()
	if err := session.StartPCAPReplay(ctx, Endpoint{IP: "127.0.0.1", Port: rtpConn.LocalAddr().(*net.UDPAddr).Port}, pcapPath, "127.0.0.1", 0); err != nil {
		t.Fatalf("StartPCAPReplay() error = %v", err)
	}

	var arrivals []time.Time
	buffer := make([]byte, 1500)
	for len(arrivals) < 2 {
		_ = rtpConn.SetReadDeadline(time.Now().Add(time.Second))
		n, _, err := rtpConn.ReadFromUDP(buffer)
		if err != nil {
			t.Fatalf("ReadFromUDP(replay) error = %v", err)
		}
		if n > 0 {
			arrivals = append(arrivals, time.Now())
		}
	}

	if gap := arrivals[1].Sub(arrivals[0]); gap < 40*time.Millisecond {
		t.Fatalf("expected preserved PCAP timing gap, got %v", gap)
	}

	stats := session.Snapshot()
	if stats.RTPPacketsSent < 2 {
		t.Fatalf("expected RTP packets sent stats, got %+v", stats)
	}
}

func writeTestPCAP(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "audio.pcap")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create(pcap) error = %v", err)
	}
	defer file.Close()

	writer := pcapgo.NewWriter(file)
	if err := writer.WriteFileHeader(65536, layers.LinkTypeEthernet); err != nil {
		t.Fatalf("WriteFileHeader() error = %v", err)
	}

	payloads := [][]byte{
		buildRTPForPCAP(t, 1, 160, []byte{1, 2, 3, 4}),
		buildRTPForPCAP(t, 2, 320, []byte{5, 6, 7, 8}),
	}
	timestamps := []time.Time{
		time.Unix(1700000000, 0),
		time.Unix(1700000000, int64(60*time.Millisecond)),
	}
	for i, payload := range payloads {
		packet := buildUDPPacketForPCAP(t, payload)
		if err := writer.WritePacket(gopacket.CaptureInfo{
			Timestamp:     timestamps[i],
			CaptureLength: len(packet),
			Length:        len(packet),
		}, packet); err != nil {
			t.Fatalf("WritePacket() error = %v", err)
		}
	}

	return path
}

func buildRTPForPCAP(t *testing.T, seq uint16, ts uint32, payload []byte) []byte {
	t.Helper()

	packet, err := BuildPacket(StreamConfig{
		PayloadType: 0,
		SSRC:        77,
		Sequence:    seq,
		Timestamp:   ts,
	}, payload)
	if err != nil {
		t.Fatalf("BuildPacket() error = %v", err)
	}
	return packet
}

func buildUDPPacketForPCAP(t *testing.T, payload []byte) []byte {
	t.Helper()

	srcIP := netip.MustParseAddr("192.0.2.10")
	dstIP := netip.MustParseAddr("198.51.100.20")
	eth := &layers.Ethernet{
		SrcMAC:       []byte{0, 1, 2, 3, 4, 5},
		DstMAC:       []byte{6, 7, 8, 9, 10, 11},
		EthernetType: layers.EthernetTypeIPv4,
	}
	ip := &layers.IPv4{
		Version:  4,
		TTL:      64,
		Protocol: layers.IPProtocolUDP,
		SrcIP:    srcIP.AsSlice(),
		DstIP:    dstIP.AsSlice(),
	}
	udp := &layers.UDP{
		SrcPort: layers.UDPPort(4000),
		DstPort: layers.UDPPort(5000),
	}
	if err := udp.SetNetworkLayerForChecksum(ip); err != nil {
		t.Fatalf("SetNetworkLayerForChecksum() error = %v", err)
	}

	buffer := gopacket.NewSerializeBuffer()
	if err := gopacket.SerializeLayers(buffer, gopacket.SerializeOptions{
		FixLengths:       true,
		ComputeChecksums: true,
	}, eth, ip, udp, gopacket.Payload(payload)); err != nil {
		t.Fatalf("SerializeLayers() error = %v", err)
	}
	return buffer.Bytes()
}
