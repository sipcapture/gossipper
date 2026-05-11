package pcap2scenario

import (
	"fmt"
	"os"

	"github.com/google/gopacket"
	"github.com/google/gopacket/pcapgo"
	"github.com/sipcapture/gossipper/internal/pcaplink"
)

// SplitRTP filters the captured UDP packets by the RTP ports found in the
// dialog and writes two mini-PCAP files: one for the caller's RTP stream and
// one for the callee's.
//
// A packet is included in the caller PCAP when its source or destination port
// matches dlg.CallerRTPPort AND at least one endpoint IP matches the caller
// RTP IP.  The same logic applies to the callee.
func SplitRTP(result ExtractResult, dlg Dialog, callerPath, calleePath string) error {
	callerN, err := writeRTPPcap(callerPath, result.UDPPackets, result.HeaderLinkType, dlg.CallerRTPIP, dlg.CallerRTPPort)
	if err != nil {
		return fmt.Errorf("write caller_rtp.pcap: %w", err)
	}

	calleeN, err := writeRTPPcap(calleePath, result.UDPPackets, result.HeaderLinkType, dlg.CalleeRTPIP, dlg.CalleeRTPPort)
	if err != nil {
		return fmt.Errorf("write callee_rtp.pcap: %w", err)
	}

	if callerN == 0 {
		fmt.Printf("  warning: no RTP packets found for caller (ip=%s port=%d)\n",
			dlg.CallerRTPIP, dlg.CallerRTPPort)
	}
	if calleeN == 0 {
		fmt.Printf("  warning: no RTP packets found for callee (ip=%s port=%d)\n",
			dlg.CalleeRTPIP, dlg.CalleeRTPPort)
	}
	return nil
}

// writeRTPPcap writes all UDP frames whose source or destination port equals
// rtpPort AND whose source or destination IP matches rtpIP.
// Returns the number of packets written.
func writeRTPPcap(path string, pkts []RawUDPPacket, headerLinkType uint32, rtpIP string, rtpPort int) (int, error) {
	f, err := os.Create(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	w := pcapgo.NewWriter(f)
	if err := pcaplink.WriteMicrosecondsFileHeader(f, 65536, headerLinkType); err != nil {
		return 0, err
	}

	if rtpPort == 0 {
		// No RTP port discovered; return a valid but empty PCAP file.
		return 0, nil
	}

	written := 0
	for _, pkt := range pkts {
		// Exact port match — deliberately excludes RTCP (port+1).
		if pkt.SrcPort != rtpPort && pkt.DstPort != rtpPort {
			continue
		}
		// At least one IP endpoint must match the media address from SDP.
		if rtpIP != "" && pkt.SrcIP != rtpIP && pkt.DstIP != rtpIP {
			continue
		}
		ci := gopacket.CaptureInfo{
			Timestamp:     pkt.Timestamp,
			CaptureLength: len(pkt.RawFrame),
			Length:        len(pkt.RawFrame),
		}
		if err := w.WritePacket(ci, pkt.RawFrame); err != nil {
			return written, fmt.Errorf("write packet: %w", err)
		}
		written++
	}
	return written, nil
}
