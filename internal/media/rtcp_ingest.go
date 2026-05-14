package media

import (
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/rtp"
)

// isProbableRTCPPacket applies a lightweight RTCP vs RTP heuristic for cleartext payloads
// on an RTP socket (RFC 3550 RTCP PT in 192–223, V=2).
func isProbableRTCPPacket(b []byte) bool {
	if len(b) < 8 {
		return false
	}
	if b[0]>>6 != 2 {
		return false
	}
	pt := b[1]
	return pt >= 192 && pt <= 223
}

func (s *Session) processInboundRTCPPayload(now time.Time, buf []byte) {
	packets, err := rtcp.Unmarshal(buf)
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stats.RTCPPacketsReceived += uint32(len(packets))
	for _, packet := range packets {
		switch p := packet.(type) {
		case *rtcp.SenderReport:
			s.stats.RTCPSenderReports++
			ntp := p.NTPTime
			s.rrLastSRNTP = uint32((ntp >> 16) & 0xFFFFFFFF)
			s.rrLastSRAt = now
		case *rtcp.ReceiverReport:
			s.stats.RTCPReceiverReports++
			for _, rep := range p.Reports {
				s.stats.RTCPReportBlocks++
				if rep.FractionLost > s.stats.RTCPMaxFractionLost {
					s.stats.RTCPMaxFractionLost = rep.FractionLost
				}
				if rep.Jitter > s.stats.RTCPMaxJitter {
					s.stats.RTCPMaxJitter = rep.Jitter
				}
				if rep.Jitter > 0 {
					if s.stats.RTCPMinJitter == 0 || rep.Jitter < s.stats.RTCPMinJitter {
						s.stats.RTCPMinJitter = rep.Jitter
					}
				}
				s.stats.RTCPJitterSum += float64(rep.Jitter)
				s.stats.RTCPJitterSamples++
			}
		}
	}
}

func (s *Session) handleInboundRTPPacketBuf(buf []byte, now time.Time) bool {
	var pkt rtp.Packet
	if err := pkt.Unmarshal(buf); err != nil {
		return false
	}
	s.mu.Lock()
	s.observeRTPPacket(&pkt, now)
	s.stats.RTPPacketsReceived++
	if s.recordOn {
		s.appendRecordInboundWithJitter(&pkt)
	}
	s.mu.Unlock()
	return true
}
