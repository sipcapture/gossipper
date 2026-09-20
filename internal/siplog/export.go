package siplog

import (
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcapgo"
)

var (
	pcapLocalIP   = net.IPv4(127, 0, 0, 1)
	pcapLocalPort = 5060
	pcapLocalMAC  = net.HardwareAddr{0x02, 0x00, 0x00, 0x00, 0x00, 0x01}
	pcapPeerMAC   = net.HardwareAddr{0x02, 0x00, 0x00, 0x00, 0x00, 0x02}
)

// FilterScenario keeps records whose Scenario equals scenario. Empty scenario returns recs.
func FilterScenario(recs []Record, scenario string) []Record {
	scenario = strings.TrimSpace(scenario)
	if scenario == "" || len(recs) == 0 {
		return recs
	}
	out := make([]Record, 0, len(recs))
	for _, rec := range recs {
		if rec.Scenario == scenario {
			out = append(out, rec)
		}
	}
	return out
}

// FilterKind keeps SIP and/or app records. Both true returns recs unchanged.
func FilterKind(recs []Record, sip, app bool) []Record {
	if sip && app {
		return recs
	}
	out := make([]Record, 0, len(recs))
	for _, rec := range recs {
		if rec.IsSIP() {
			if sip {
				out = append(out, rec)
			}
			continue
		}
		if rec.IsApp() && app {
			out = append(out, rec)
		}
	}
	return out
}

// FilterLevel keeps SIP records and app records at or above min (debug|info|warn|error).
func FilterLevel(recs []Record, min string) []Record {
	rank := LevelRank(min)
	if rank == 0 {
		return recs
	}
	out := make([]Record, 0, len(recs))
	for _, rec := range recs {
		if rec.IsSIP() || LevelRank(rec.Level) >= rank {
			out = append(out, rec)
		}
	}
	return out
}

// FormatText renders SIP and app records as a timestamped dump (one message per block).
func FormatText(recs []Record) string {
	if len(recs) == 0 {
		return "# no messages\n"
	}
	var b strings.Builder
	for i, rec := range recs {
		if i > 0 {
			b.WriteByte('\n')
		}
		dir := "IN"
		if rec.IsApp() {
			dir = "APP"
		} else if rec.Dir == "send" {
			dir = "OUT"
		}
		ts := rec.Ts.UTC().Format(time.RFC3339Nano)
		fmt.Fprintf(&b, "======= #%d %s %s %s", rec.Seq, ts, dir, rec.Peer)
		if rec.IsApp() {
			if rec.Level != "" {
				fmt.Fprintf(&b, " %s", rec.Level)
			}
			if rec.Method != "" {
				fmt.Fprintf(&b, " %s", rec.Method)
			}
		}
		if rec.Scenario != "" {
			fmt.Fprintf(&b, " %s", rec.Scenario)
		}
		b.WriteString(" =======\n")
		raw := strings.ReplaceAll(rec.Raw, "\r\n", "\n")
		b.WriteString(raw)
		if !strings.HasSuffix(raw, "\n") {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// WritePCAP writes reconstructed Ethernet/IPv4/UDP frames for each SIP datagram.
// Peer is the remote host:port. Local is 127.0.0.1:5060 (the ring does not store bind).
func WritePCAP(w io.Writer, recs []Record) error {
	pw := pcapgo.NewWriter(w)
	if err := pw.WriteFileHeader(65535, layers.LinkTypeEthernet); err != nil {
		return err
	}
	for _, rec := range recs {
		if !rec.IsSIP() {
			continue
		}
		frame, err := serializeSIPFrame(rec)
		if err != nil {
			return err
		}
		ts := rec.Ts
		if ts.IsZero() {
			ts = time.Now().UTC()
		}
		if err := pw.WritePacket(gopacket.CaptureInfo{
			Timestamp:     ts,
			CaptureLength: len(frame),
			Length:        len(frame),
		}, frame); err != nil {
			return err
		}
	}
	return nil
}

func serializeSIPFrame(rec Record) ([]byte, error) {
	peerIP, peerPort := parsePeerAddr(rec.Peer)
	srcIP, srcPort, dstIP, dstPort := pcapLocalIP, pcapLocalPort, peerIP, peerPort
	srcMAC, dstMAC := pcapLocalMAC, pcapPeerMAC
	if rec.Dir != "send" {
		srcIP, srcPort, dstIP, dstPort = peerIP, peerPort, pcapLocalIP, pcapLocalPort
		srcMAC, dstMAC = pcapPeerMAC, pcapLocalMAC
	}
	eth := &layers.Ethernet{
		SrcMAC:       srcMAC,
		DstMAC:       dstMAC,
		EthernetType: layers.EthernetTypeIPv4,
	}
	ip := &layers.IPv4{
		Version:  4,
		TTL:      64,
		Protocol: layers.IPProtocolUDP,
		SrcIP:    srcIP.To4(),
		DstIP:    dstIP.To4(),
	}
	udp := &layers.UDP{
		SrcPort: layers.UDPPort(srcPort),
		DstPort: layers.UDPPort(dstPort),
	}
	if err := udp.SetNetworkLayerForChecksum(ip); err != nil {
		return nil, err
	}
	buf := gopacket.NewSerializeBuffer()
	if err := gopacket.SerializeLayers(buf, gopacket.SerializeOptions{
		FixLengths:       true,
		ComputeChecksums: true,
	}, eth, ip, udp, gopacket.Payload([]byte(rec.Raw))); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func parsePeerAddr(peer string) (net.IP, int) {
	host, portStr, err := net.SplitHostPort(strings.TrimSpace(peer))
	if err != nil {
		return net.IPv4(0, 0, 0, 0), 5060
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		port = 5060
	}
	ip := net.ParseIP(host)
	if v4 := ip.To4(); v4 != nil {
		return v4, port
	}
	return net.IPv4(0, 0, 0, 0), port
}
