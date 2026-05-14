package media

import (
	"encoding/binary"
	"sort"

	"github.com/pion/rtp"
)

type recordSeqSample struct {
	seq uint16
	pcm []int16
}

func (s *Session) resetRecordReorder() {
	s.recordSeqBuf = nil
	s.recordHaveNext = false
	s.recordNextSeq = 0
}

// pcmFromInboundRTP decodes G.711 (PT 0/8), maps RFC 2833 telephone-event to silence
// of the event duration, and uses one 20 ms silence frame for other payloads.
func pcmFromInboundRTP(pkt *rtp.Packet) []int16 {
	if len(pkt.Payload) == 0 {
		return nil
	}
	samples, err := decodeG711PayloadToPCM16(pkt.PayloadType, pkt.Payload)
	if err == nil && len(samples) > 0 {
		return samples
	}
	if n := rfc2833SilenceSamples(pkt.Payload); n > 0 {
		if n > 8000*3 {
			n = 8000 * 3
		}
		return make([]int16, n)
	}
	return make([]int16, 160)
}

// pcmFromOutboundPayload mirrors pcmFromInboundRTP for sent frames (no RTP header).
func pcmFromOutboundPayload(pt uint8, payload []byte) []int16 {
	if len(payload) == 0 {
		return nil
	}
	samples, err := decodeG711PayloadToPCM16(pt, payload)
	if err == nil && len(samples) > 0 {
		return samples
	}
	if n := rfc2833SilenceSamples(payload); n > 0 {
		if n > 8000*3 {
			n = 8000 * 3
		}
		return make([]int16, n)
	}
	return make([]int16, 160)
}

// rfc2833SilenceSamples returns PCM sample count for silence covering a telephone-event payload.
func rfc2833SilenceSamples(payload []byte) int {
	if len(payload) < 4 {
		return 0
	}
	// RFC 2833: duration in timestamp units (same as RTP clock, typically 8000 for audio).
	dur := int(binary.BigEndian.Uint16(payload[2:4]))
	if dur <= 0 || dur > 8000*10 {
		return 0
	}
	return dur
}

func (s *Session) appendRecordInboundWithJitter(pkt *rtp.Packet) {
	if !s.recordOn {
		return
	}
	pcm := pcmFromInboundRTP(pkt)
	if len(pcm) == 0 {
		return
	}
	if !s.recordHaveNext {
		s.recordNextSeq = pkt.SequenceNumber
		s.recordHaveNext = true
	}
	s.pushRecordPCM(pkt.SequenceNumber, pcm)
}

func (s *Session) pushRecordPCM(seq uint16, pcm []int16) {
	if seq == s.recordNextSeq {
		s.recordRecv = append(s.recordRecv, pcm...)
		s.recordNextSeq++
		s.drainRecordBufHead()
		return
	}
	// Replace duplicate sequence with latest payload.
	for i := range s.recordSeqBuf {
		if s.recordSeqBuf[i].seq == seq {
			s.recordSeqBuf[i].pcm = pcm
			return
		}
	}
	s.recordSeqBuf = append(s.recordSeqBuf, recordSeqSample{seq: seq, pcm: pcm})
	if len(s.recordSeqBuf) > 48 {
		sort.Slice(s.recordSeqBuf, func(i, j int) bool {
			return s.recordSeqBuf[i].seq < s.recordSeqBuf[j].seq
		})
		var last uint16
		for _, fr := range s.recordSeqBuf {
			s.recordRecv = append(s.recordRecv, fr.pcm...)
			last = fr.seq
		}
		s.recordSeqBuf = nil
		s.recordNextSeq = last + 1
		s.recordHaveNext = true
	}
}

func (s *Session) drainRecordBufHead() {
	for {
		idx := -1
		for i := range s.recordSeqBuf {
			if s.recordSeqBuf[i].seq == s.recordNextSeq {
				idx = i
				break
			}
		}
		if idx < 0 {
			return
		}
		s.recordRecv = append(s.recordRecv, s.recordSeqBuf[idx].pcm...)
		s.recordSeqBuf = append(s.recordSeqBuf[:idx], s.recordSeqBuf[idx+1:]...)
		s.recordNextSeq++
	}
}
