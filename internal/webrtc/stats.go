package webrtc

import (
	pion "github.com/pion/webrtc/v4"
)

// RTPStats aggregates audio RTP metrics from the peer connection stats report.
type RTPStats struct {
	PacketsSent      uint32
	PacketsReceived  uint32
	PacketsLost      int32
	JitterSeconds    float64
	FractionLost     float64
	RemoteFractionOK bool
}

// RTPStats reads the latest audio RTP counters from pion GetStats().
func (b *Bridge) RTPStats() RTPStats {
	if b == nil || b.pc == nil {
		return RTPStats{}
	}
	var out RTPStats
	for _, s := range b.pc.GetStats() {
		switch st := s.(type) {
		case pion.InboundRTPStreamStats:
			if st.Kind != "audio" {
				continue
			}
			if st.PacketsReceived > out.PacketsReceived {
				out.PacketsReceived = st.PacketsReceived
			}
			if st.PacketsLost > out.PacketsLost {
				out.PacketsLost = st.PacketsLost
			}
			if st.Jitter > out.JitterSeconds {
				out.JitterSeconds = st.Jitter
			}
		case pion.OutboundRTPStreamStats:
			if st.Kind != "audio" {
				continue
			}
			if st.PacketsSent > out.PacketsSent {
				out.PacketsSent = st.PacketsSent
			}
		case pion.RemoteInboundRTPStreamStats:
			if st.Kind != "audio" {
				continue
			}
			if st.FractionLost >= out.FractionLost {
				out.FractionLost = st.FractionLost
				out.RemoteFractionOK = true
			}
			if st.PacketsLost > out.PacketsLost {
				out.PacketsLost = st.PacketsLost
			}
			if st.Jitter > out.JitterSeconds {
				out.JitterSeconds = st.Jitter
			}
		}
	}
	return out
}
