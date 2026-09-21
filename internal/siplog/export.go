package siplog

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcapgo"
)

var (
	pcapLocalIP          = net.IPv4(127, 0, 0, 1)
	pcapLocalPort uint16 = 5060
	pcapLocalMAC         = net.HardwareAddr{0x02, 0x00, 0x00, 0x00, 0x00, 0x01}
	pcapPeerMAC          = net.HardwareAddr{0x02, 0x00, 0x00, 0x00, 0x00, 0x02}
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

// FilterKind keeps SIP, app, and/or RTP-summary records.
func FilterKind(recs []Record, sip, app, rtp bool) []Record {
	if sip && app && rtp {
		return recs
	}
	out := make([]Record, 0, len(recs))
	for _, rec := range recs {
		switch {
		case rec.IsRTP():
			if rtp {
				out = append(out, rec)
			}
		case rec.IsApp():
			if app {
				out = append(out, rec)
			}
		default:
			if sip && rec.IsSIP() {
				out = append(out, rec)
			}
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
		if rec.IsSIP() || rec.IsRTP() || LevelRank(rec.Level) >= rank {
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
		} else if rec.IsRTP() {
			dir = "RTP"
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
	return WritePCAPAll(w, recs, nil)
}

// WritePCAPAll writes SIP records plus captured RTP datagrams, merged by timestamp.
func WritePCAPAll(w io.Writer, recs []Record, rtp []RTPPacket) error {
	pw := pcapgo.NewWriter(w)
	if err := pw.WriteFileHeader(65535, layers.LinkTypeEthernet); err != nil {
		return err
	}
	type timed struct {
		ts    time.Time
		frame []byte
	}
	frames := make([]timed, 0, len(recs)+len(rtp))
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
		frames = append(frames, timed{ts: ts, frame: frame})
	}
	for _, pkt := range rtp {
		frame, err := serializeUDPFrame(pkt.SrcIP, pkt.SrcPort, pkt.DstIP, pkt.DstPort, pkt.Payload)
		if err != nil {
			return err
		}
		ts := pkt.Ts
		if ts.IsZero() {
			ts = time.Now().UTC()
		}
		frames = append(frames, timed{ts: ts, frame: frame})
	}
	sort.SliceStable(frames, func(i, j int) bool {
		return frames[i].ts.Before(frames[j].ts)
	})
	for _, f := range frames {
		if err := pw.WritePacket(gopacket.CaptureInfo{
			Timestamp:     f.ts,
			CaptureLength: len(f.frame),
			Length:        len(f.frame),
		}, f.frame); err != nil {
			return err
		}
	}
	return nil
}

const callDumpReadme = `gossipper call dump
- call.pcap  SIP + RTP (Wireshark: Telephony → RTP → Stream Analysis / Play Streams)
- call.log   SIP, scenario debug, RTP summaries
`

// WriteCallDump writes a zip with call.pcap (SIP+RTP) and call.log (text).
func WriteCallDump(w io.Writer, recs []Record, rtp []RTPPacket) error {
	zw := zip.NewWriter(w)
	defer zw.Close()
	if err := zipBytes(zw, "README.txt", []byte(callDumpReadme)); err != nil {
		return err
	}
	if err := zipBytes(zw, "call.log", []byte(FormatText(recs))); err != nil {
		return err
	}
	var pcap bytes.Buffer
	if err := WritePCAPAll(&pcap, recs, rtp); err != nil {
		return err
	}
	return zipBytes(zw, "call.pcap", pcap.Bytes())
}

func zipBytes(zw *zip.Writer, name string, body []byte) error {
	fw, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = fw.Write(body)
	return err
}

func serializeSIPFrame(rec Record) ([]byte, error) {
	peerIP, peerPort := parsePeerAddr(rec.Peer)
	srcIP, srcPort, dstIP, dstPort := pcapLocalIP, pcapLocalPort, peerIP, peerPort
	if rec.Dir != "send" {
		srcIP, srcPort, dstIP, dstPort = peerIP, peerPort, pcapLocalIP, pcapLocalPort
	}
	return serializeUDPFrame(srcIP, srcPort, dstIP, dstPort, []byte(rec.Raw))
}

func serializeUDPFrame(srcIP net.IP, srcPort uint16, dstIP net.IP, dstPort uint16, payload []byte) ([]byte, error) {
	if srcIP == nil || srcIP.To4() == nil {
		srcIP = net.IPv4(0, 0, 0, 0)
	}
	if dstIP == nil || dstIP.To4() == nil {
		dstIP = net.IPv4(0, 0, 0, 0)
	}
	srcMAC, dstMAC := pcapLocalMAC, pcapPeerMAC
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
	}, eth, ip, udp, gopacket.Payload(payload)); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func parsePeerAddr(peer string) (net.IP, uint16) {
	const fallback uint16 = 5060
	host, portStr, err := net.SplitHostPort(strings.TrimSpace(peer))
	if err != nil {
		return net.IPv4(0, 0, 0, 0), fallback
	}
	// bitSize 16 bounds the value; CodeQL does not treat Atoi (int) → uint16 as safe.
	u, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil || u < 1 {
		return peerIPv4(host), fallback
	}
	return peerIPv4(host), uint16(u)
}

func peerIPv4(host string) net.IP {
	if v4 := net.ParseIP(host).To4(); v4 != nil {
		return v4
	}
	return net.IPv4(0, 0, 0, 0)
}
