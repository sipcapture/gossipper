package media

// EncodeG711Frame encodes mono 16-bit PCM samples into one G.711 RTP payload
// (PCMU payload type 0 or PCMA payload type 8).
func EncodeG711Frame(pt uint8, samples []int16) []byte {
	switch pt {
	case 0:
		out := make([]byte, len(samples))
		for i, s := range samples {
			out[i] = linearToMuLaw(s)
		}
		return out
	case 8:
		out := make([]byte, len(samples))
		for i, s := range samples {
			out[i] = linearToALaw(s)
		}
		return out
	default:
		return nil
	}
}
