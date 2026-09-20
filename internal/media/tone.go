package media

import (
	"math"
	"strings"

	"github.com/gotranspile/g722"
)

// ITU-T E.180 ringback tone. Used for synthetic RTP so Fake ringing / builtin
// UAS produce an audible cadence instead of codec silence.
const (
	syntheticToneHz   = 425.0
	syntheticToneAmp  = 0.35 * 32767
	g722Pcm16kHzFrame = 320 // 20 ms at 16 kHz (G.722 RTP clock is still 8 kHz)
)

type syntheticGen struct {
	phase float64
	g722  *g722.Encoder
}

func newSyntheticGen(cfg StreamConfig) *syntheticGen {
	g := &syntheticGen{}
	if isG722(cfg) {
		g.g722 = g722.NewEncoder(g722.Rate64000, 0)
	}
	return g
}

func isG722(cfg StreamConfig) bool {
	if cfg.PayloadType == 9 {
		return true
	}
	return strings.HasPrefix(strings.ToUpper(cfg.PayloadName), "G722")
}

func (g *syntheticGen) next(cfg StreamConfig) []byte {
	if strings.HasPrefix(strings.ToUpper(cfg.PayloadName), "OPUS") {
		return []byte{0xF8, 0xFF, 0xFE}
	}
	if cfg.PayloadType == 13 {
		return []byte{0x00}
	}

	channels := int(cfg.Channels)
	if channels < 1 {
		channels = 1
	}

	n := int(cfg.SamplesPerPkt)
	if n <= 0 {
		n = 160
	}

	if isG722(cfg) {
		pcm := make([]int16, g722Pcm16kHzFrame)
		fillSine(pcm, 16000, &g.phase)
		out := make([]byte, g722Pcm16kHzFrame/2)
		wrote := g.g722.Encode(out, pcm)
		return out[:wrote]
	}

	name := strings.ToUpper(cfg.PayloadName)
	g711 := cfg.PayloadType == 0 || cfg.PayloadType == 8 ||
		strings.HasPrefix(name, "PCMU") || strings.HasPrefix(name, "PCMA")
	if !g711 {
		return make([]byte, n*channels)
	}

	pcm := make([]int16, n)
	fillSine(pcm, 8000, &g.phase)
	payload := make([]byte, n*channels)
	alaw := cfg.PayloadType == 8 || strings.HasPrefix(name, "PCMA")
	for i, sample := range pcm {
		var b byte
		if alaw {
			b = linearToALaw(sample)
		} else {
			b = linearToMuLaw(sample)
		}
		for ch := 0; ch < channels; ch++ {
			payload[i*channels+ch] = b
		}
	}
	return payload
}

func fillSine(dst []int16, sampleRate float64, phase *float64) {
	step := 2 * math.Pi * syntheticToneHz / sampleRate
	for i := range dst {
		dst[i] = int16(syntheticToneAmp * math.Sin(*phase))
		*phase += step
		if *phase >= 2*math.Pi {
			*phase -= 2 * math.Pi
		}
	}
}
