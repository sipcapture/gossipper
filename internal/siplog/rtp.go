package siplog

import (
	"fmt"
	"net"
	"time"
)

const (
	rtpCap          = 4096
	rtpSummaryEvery = 50
	maxRTPRaw       = 2048
)

// RTPPacket is one captured RTP datagram (full header + payload) for PCAP dump.
type RTPPacket struct {
	Ts      time.Time
	Dir     string
	SrcIP   net.IP
	SrcPort uint16
	DstIP   net.IP
	DstPort uint16
	CallID  string
	Payload []byte
}

// RTPEnabled reports whether RTP datagrams are recorded for dump/pcap.
func (r *Ring) RTPEnabled() bool {
	return r != nil && r.rtpOn.Load()
}

// SetRTP turns RTP packet capture on or off and wakes waiters.
func (r *Ring) SetRTP(on bool) {
	if r == nil {
		return
	}
	if r.rtpOn.Load() == on {
		return
	}
	r.rtpOn.Store(on)
	r.mu.Lock()
	r.notifyLocked()
	r.mu.Unlock()
}

// AddRTP stores one RTP datagram when RTP capture is on. Live Trace gets a
// summary on the first packet and every rtpSummaryEvery packets (not each frame).
func (r *Ring) AddRTP(dir, srcIP string, srcPort int, dstIP string, dstPort int, callID string, payload []byte) {
	if r == nil || len(payload) == 0 {
		return
	}
	r.catalog.AddRTP(dir, srcIP, srcPort, dstIP, dstPort, callID, payload)
	if !r.rtpOn.Load() {
		return
	}
	if dir != "send" && dir != "recv" {
		dir = "send"
	}
	raw := payload
	if len(raw) > maxRTPRaw {
		raw = raw[:maxRTPRaw]
	}
	pkt := RTPPacket{
		Ts:      time.Now().UTC(),
		Dir:     dir,
		SrcIP:   peerIPv4(srcIP),
		SrcPort: udpPort(srcPort),
		DstIP:   peerIPv4(dstIP),
		DstPort: udpPort(dstPort),
		CallID:  callID,
		Payload: append([]byte(nil), raw...),
	}
	peer := net.JoinHostPort(dstIP, fmt.Sprintf("%d", pkt.DstPort))
	if dir == "recv" {
		peer = net.JoinHostPort(srcIP, fmt.Sprintf("%d", pkt.SrcPort))
	}
	pt, seq := rtpPTSeq(raw)
	summary := fmt.Sprintf("RTP %s PT=%d seq=%d %dB %s:%d → %s:%d",
		dir, pt, seq, len(raw), pkt.SrcIP, pkt.SrcPort, pkt.DstIP, pkt.DstPort)

	r.mu.Lock()
	if len(r.rtp) == rtpCap {
		copy(r.rtp, r.rtp[1:])
		r.rtp[rtpCap-1] = pkt
	} else {
		r.rtp = append(r.rtp, pkt)
	}
	r.rtpN++
	n := r.rtpN
	emit := n == 1 || n%rtpSummaryEvery == 0
	r.mu.Unlock()

	if !emit {
		return
	}
	r.append(Record{
		Ts:      pkt.Ts,
		Kind:    KindRTP,
		Dir:     dir,
		Peer:    peer,
		Summary: summary,
		Method:  "RTP",
		CallID:  callID,
		Raw:     summary,
	})
}

// RTPPackets returns a copy of captured RTP datagrams, oldest first.
func (r *Ring) RTPPackets() []RTPPacket {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.rtp) == 0 {
		return nil
	}
	out := make([]RTPPacket, len(r.rtp))
	copy(out, r.rtp)
	return out
}

// IsRTP is true for Live Trace RTP summaries.
func (rec Record) IsRTP() bool {
	return rec.Kind == KindRTP
}

func rtpPTSeq(pkt []byte) (pt int, seq uint16) {
	if len(pkt) < 4 {
		return 0, 0
	}
	return int(pkt[1] & 0x7f), uint16(pkt[2])<<8 | uint16(pkt[3])
}

func udpPort(n int) uint16 {
	if n < 1 || n > 65535 {
		return 0
	}
	u := uint(n)
	if u > 65535 {
		return 0
	}
	return uint16(u)
}
