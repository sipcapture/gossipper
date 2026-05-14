package media

// muLawSampleToLinear decodes one G.711 μ-law byte to 16-bit linear PCM (ITU-T G.711).
func muLawSampleToLinear(b byte) int16 {
	b = ^b
	sign := int16(b & 0x80)
	exponent := int16((b >> 4) & 0x07)
	mantissa := int16(b & 0x0F)
	sample := mantissa<<1 + 33
	sample <<= exponent + 2
	sample -= 33
	if sign != 0 {
		sample = -sample
	}
	return sample
}

// aLawSampleToLinear decodes one G.711 A-law byte to 16-bit linear PCM (ITU-T G.711).
func aLawSampleToLinear(b byte) int16 {
	b ^= 0x55
	t := int16(b&0x0F) << 4
	seg := int16((b & 0x70) >> 4)
	switch seg {
	case 0:
		t += 8
	case 1:
		t += 0x108
	default:
		t += 0x108
		t <<= uint(seg - 1)
	}
	if b&0x80 == 0 {
		return t
	}
	return -t
}

func decodeG711PayloadToPCM16(pt uint8, payload []byte) ([]int16, error) {
	if len(payload) == 0 {
		return nil, nil
	}
	out := make([]int16, len(payload))
	switch pt {
	case 0:
		for i, b := range payload {
			out[i] = muLawSampleToLinear(b)
		}
	case 8:
		for i, b := range payload {
			out[i] = aLawSampleToLinear(b)
		}
	default:
		return nil, nil
	}
	return out, nil
}
