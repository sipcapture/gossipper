package media

import (
	"fmt"
	"strconv"
	"strings"
)

// AnswerCodec is the RFC 3264 audio answer picked from a remote offer.
type AnswerCodec struct {
	PT      int
	Name    string // PCMU, PCMA, G722, opus
	Codec   string // descriptor for rtp_stream / ApplyPayloadParams (G722/8000, opus/48000/2)
	RTPMap  string // rtpmap encoding name, e.g. G722/8000 or opus/48000/2
	FMTP    string // fmtp parameter string without "a=fmtp:<pt> "
	TelPT   int    // telephone-event PT, 0 if the offer had none
	TelMap  string
	TelFMTP string
}

var fallbackPCMU = AnswerCodec{
	PT:     0,
	Name:   "PCMU",
	Codec:  "PCMU/8000",
	RTPMap: "PCMU/8000",
}

type offerPayload struct {
	pt     int
	name   string
	rtpmap string
	fmtp   string
}

// AnswerFromSIP picks the first supported audio codec in the last SIP message
// (INVITE offer or 200 answer). Empty or unmatched SDP falls back to PCMU.
func AnswerFromSIP(raw string) AnswerCodec {
	return PickAnswerCodec(SDPBody(raw))
}

// SDPBody returns the SDP fragment of a SIP message, or raw if it already looks
// like SDP.
func SDPBody(raw string) string {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	if i := strings.Index(raw, "\n\n"); i >= 0 {
		body := strings.TrimLeft(raw[i+2:], "\n")
		if strings.Contains(body, "v=") || strings.Contains(body, "m=") {
			return body
		}
	}
	return raw
}

// PickAnswerCodec selects the first offered payload type we can send:
// G722, PCMA, Opus, or PCMU, in offer order. telephone-event is echoed when present.
func PickAnswerCodec(sdp string) AnswerCodec {
	payloads := parseAudioOffer(sdp)
	var tel *offerPayload
	for i := range payloads {
		if payloads[i].name == "telephone-event" {
			p := payloads[i]
			tel = &p
			break
		}
	}
	for _, p := range payloads {
		if !supportedAudio(p.name) {
			continue
		}
		ans := AnswerCodec{
			PT:     p.pt,
			Name:   canonicalName(p.name),
			Codec:  codecDescriptor(p),
			RTPMap: p.rtpmap,
			FMTP:   p.fmtp,
		}
		if ans.RTPMap == "" {
			ans.RTPMap = ans.Codec
		}
		if ans.Name == "opus" && ans.FMTP == "" {
			ans.FMTP = "minptime=10;useinbandfec=1"
		}
		if tel != nil {
			ans.TelPT = tel.pt
			ans.TelMap = tel.rtpmap
			if ans.TelMap == "" {
				ans.TelMap = "telephone-event/8000"
			}
			ans.TelFMTP = tel.fmtp
		}
		return ans
	}
	return fallbackPCMU
}

// FormatAnswerSDP returns m=audio plus rtpmap/fmtp lines (CRLF, no trailing blank).
func FormatAnswerSDP(mediaPort int, ans AnswerCodec) string {
	if mediaPort <= 0 {
		mediaPort = 0
	}
	pts := strconv.Itoa(ans.PT)
	if ans.TelPT > 0 {
		pts += " " + strconv.Itoa(ans.TelPT)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "m=audio %d RTP/AVP %s\r\n", mediaPort, pts)
	fmt.Fprintf(&b, "a=rtpmap:%d %s", ans.PT, ans.RTPMap)
	if ans.FMTP != "" {
		fmt.Fprintf(&b, "\r\na=fmtp:%d %s", ans.PT, ans.FMTP)
	}
	if ans.TelPT > 0 {
		fmt.Fprintf(&b, "\r\na=rtpmap:%d %s", ans.TelPT, ans.TelMap)
		if ans.TelFMTP != "" {
			fmt.Fprintf(&b, "\r\na=fmtp:%d %s", ans.TelPT, ans.TelFMTP)
		}
	}
	return b.String()
}

func parseAudioOffer(sdp string) []offerPayload {
	sdp = strings.ReplaceAll(sdp, "\r\n", "\n")
	var pts []int
	rtpmap := map[int]string{}
	fmtp := map[int]string{}
	inAudio := false
	for _, line := range strings.Split(sdp, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "m=") {
			inAudio = strings.HasPrefix(lower, "m=audio ")
			if !inAudio {
				continue
			}
			fields := strings.Fields(line)
			// m=audio port proto PT...
			for _, f := range fields[3:] {
				n, err := strconv.Atoi(f)
				if err == nil {
					pts = append(pts, n)
				}
			}
			continue
		}
		if !inAudio {
			continue
		}
		if strings.HasPrefix(lower, "a=rtpmap:") {
			rest := strings.TrimSpace(line[len("a=rtpmap:"):])
			ptStr, enc, ok := strings.Cut(rest, " ")
			if !ok {
				continue
			}
			pt, err := strconv.Atoi(ptStr)
			if err != nil {
				continue
			}
			rtpmap[pt] = strings.TrimSpace(enc)
			continue
		}
		if strings.HasPrefix(lower, "a=fmtp:") {
			rest := strings.TrimSpace(line[len("a=fmtp:"):])
			ptStr, params, ok := strings.Cut(rest, " ")
			if !ok {
				continue
			}
			pt, err := strconv.Atoi(ptStr)
			if err != nil {
				continue
			}
			fmtp[pt] = strings.TrimSpace(params)
		}
	}
	out := make([]offerPayload, 0, len(pts))
	for _, pt := range pts {
		enc := rtpmap[pt]
		name := encodingName(pt, enc)
		out = append(out, offerPayload{
			pt:     pt,
			name:   name,
			rtpmap: enc,
			fmtp:   fmtp[pt],
		})
	}
	return out
}

func encodingName(pt int, rtpmap string) string {
	if rtpmap != "" {
		name, _, _ := strings.Cut(rtpmap, "/")
		return strings.ToLower(strings.TrimSpace(name))
	}
	switch pt {
	case 0:
		return "pcmu"
	case 8:
		return "pcma"
	case 9:
		return "g722"
	default:
		return ""
	}
}

func supportedAudio(name string) bool {
	switch name {
	case "pcmu", "pcma", "g722", "opus":
		return true
	default:
		return false
	}
}

func canonicalName(name string) string {
	switch name {
	case "pcmu":
		return "PCMU"
	case "pcma":
		return "PCMA"
	case "g722":
		return "G722"
	case "opus":
		return "opus"
	default:
		return name
	}
}

func codecDescriptor(p offerPayload) string {
	if p.rtpmap != "" {
		return p.rtpmap
	}
	switch p.name {
	case "pcma":
		return "PCMA/8000"
	case "g722":
		return "G722/8000"
	case "opus":
		return "opus/48000/2"
	default:
		return "PCMU/8000"
	}
}
