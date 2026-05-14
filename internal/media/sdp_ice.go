package media

import (
	"context"
	"net"
	"strconv"
	"strings"
	"time"
)

// ParseAudioIceCredentials returns the first a=ice-ufrag / a=ice-pwd in m=audio.
func ParseAudioIceCredentials(sdpBody string) (ufrag, pwd string, ok bool) {
	return parseIceCredentials(sdpBody, true)
}

// ParseAudioIceCredentialsTrickleFragment parses a=ice-ufrag / a=ice-pwd from any line
// when the body has no m=audio line (e.g. SIP INFO trickle payload). Returns false if m=audio is present.
func ParseAudioIceCredentialsTrickleFragment(sdpBody string) (ufrag, pwd string, ok bool) {
	body := strings.ReplaceAll(sdpBody, "\r\n", "\n")
	if strings.Contains(strings.ToLower(body), "m=audio") {
		return "", "", false
	}
	return parseIceCredentials(sdpBody, false)
}

func parseIceCredentials(sdpBody string, audioSectionOnly bool) (ufrag, pwd string, ok bool) {
	body := strings.ReplaceAll(sdpBody, "\r\n", "\n")
	lines := strings.Split(body, "\n")
	inAudio := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		low := strings.ToLower(line)
		if audioSectionOnly {
			if strings.HasPrefix(low, "m=audio") {
				inAudio = true
				continue
			}
			if strings.HasPrefix(low, "m=") {
				if inAudio {
					break
				}
				continue
			}
			if !inAudio {
				continue
			}
		}
		if strings.HasPrefix(low, "a=ice-ufrag:") {
			ufrag = strings.TrimSpace(line[len("a=ice-ufrag:"):])
		}
		if strings.HasPrefix(low, "a=ice-pwd:") {
			pwd = strings.TrimSpace(line[len("a=ice-pwd:"):])
		}
	}
	if ufrag == "" || pwd == "" {
		return "", "", false
	}
	return ufrag, pwd, true
}

// SDPHasIceMediaAttributes reports ICE-related attributes without requiring SRTP (SAVP/fingerprint).
// Used to detect trickle fragments so we do not wipe an existing DTLS/SDES negotiation.
func SDPHasIceMediaAttributes(sdpBody string) bool {
	low := strings.ToLower(strings.ReplaceAll(sdpBody, "\r\n", "\n"))
	return strings.Contains(low, "a=candidate:") ||
		strings.Contains(low, "a=ice-ufrag:") ||
		strings.Contains(low, "a=ice-pwd:") ||
		strings.Contains(low, "a=ice-options:")
}

func iceCandidateTypeRank(typ string) int {
	switch typ {
	case "host":
		return 40
	case "srflx":
		return 30
	case "prflx":
		return 25
	case "relay":
		return 20
	default:
		return 0
	}
}

func resolveICEAddr(addr string) (string, bool) {
	if net.ParseIP(addr) != nil {
		return addr, true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, addr)
	if err != nil || len(ips) == 0 {
		return "", false
	}
	for _, ipa := range ips {
		if v4 := ipa.IP.To4(); v4 != nil {
			return v4.String(), true
		}
	}
	return ips[0].IP.String(), true
}

// parseICECandidateLine parses one SDP a=candidate line (RFC 5245 / ICE).
// Returns UDP RTP (component 1) host/srflx/prflx/relay addresses only.
func parseICECandidateLine(line string) (addr string, port int, candTyp string, priority uint32, ok bool) {
	line = strings.TrimSpace(line)
	low := strings.ToLower(line)
	if !strings.HasPrefix(low, "a=candidate:") {
		return "", 0, "", 0, false
	}
	rest := strings.TrimSpace(line[len("a=candidate:"):])
	lowRest := strings.ToLower(rest)
	typIdx := strings.Index(lowRest, " typ ")
	if typIdx < 0 {
		return "", 0, "", 0, false
	}
	head := strings.TrimSpace(rest[:typIdx])
	tail := strings.TrimSpace(rest[typIdx+len(" typ "):])
	tailFields := strings.Fields(tail)
	if len(tailFields) < 1 {
		return "", 0, "", 0, false
	}
	candTyp = strings.ToLower(tailFields[0])
	hf := strings.Fields(head)
	if len(hf) < 6 {
		return "", 0, "", 0, false
	}
	if hf[1] != "1" {
		return "", 0, "", 0, false
	}
	if strings.ToLower(hf[2]) != "udp" {
		return "", 0, "", 0, false
	}
	prio64, err := strconv.ParseUint(hf[3], 10, 32)
	if err != nil {
		return "", 0, "", 0, false
	}
	port, err = strconv.Atoi(hf[5])
	if err != nil || port <= 0 || port > 65535 {
		return "", 0, "", 0, false
	}
	addr = hf[4]
	if net.ParseIP(addr) == nil {
		resolved, okRes := resolveICEAddr(addr)
		if !okRes {
			return "", 0, "", 0, false
		}
		addr = resolved
	}
	return addr, port, candTyp, uint32(prio64), true
}

type iceRTPPick struct {
	ip       string
	port     int
	typ      string
	priority uint32
}

func betterICERTPPick(a, b iceRTPPick) bool {
	ra, rb := iceCandidateTypeRank(a.typ), iceCandidateTypeRank(b.typ)
	if ra != rb {
		return ra > rb
	}
	return a.priority > b.priority
}

// scanICERtpUDP collects UDP RTP (component 1) ICE candidates. When sectionOnly is true, only the first
// m=<media> block is scanned (media is "audio", "video", or "image"). When sectionOnly is false, every
// a=candidate line in the body is considered (trickle fragment).
func scanICERtpUDP(sdpBody string, media string, sectionOnly bool) (ip string, port int, iceTyp string, ok bool) {
	media = strings.ToLower(strings.TrimSpace(media))
	sectionPrefix := "m=" + media
	body := strings.ReplaceAll(sdpBody, "\r\n", "\n")
	lines := strings.Split(body, "\n")
	inSection := false
	var best iceRTPPick
	have := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		low := strings.ToLower(line)
		if sectionOnly {
			if strings.HasPrefix(low, sectionPrefix) {
				inSection = true
				continue
			}
			if strings.HasPrefix(low, "m=") {
				if inSection {
					break
				}
				continue
			}
			if !inSection {
				continue
			}
		}
		addr, port, candTyp, prio, okLine := parseICECandidateLine(line)
		if !okLine {
			continue
		}
		cand := iceRTPPick{ip: addr, port: port, typ: candTyp, priority: prio}
		if !have || betterICERTPPick(cand, best) {
			best = cand
			have = true
		}
	}
	if !have {
		return "", 0, "", false
	}
	return best.ip, best.port, best.typ, true
}

func parseICERtpUDPCandidateTrickleFragment(sdpBody, media string) (ip string, port int, iceTyp string, ok bool) {
	body := strings.ReplaceAll(sdpBody, "\r\n", "\n")
	media = strings.ToLower(strings.TrimSpace(media))
	if strings.Contains(strings.ToLower(body), "m="+media) {
		return "", 0, "", false
	}
	return scanICERtpUDP(sdpBody, "", false)
}

// ParseAudioICERtpUDPCandidate picks the best UDP / RTP (component 1) ICE candidate from the first m=audio.
// Prefers typ=host, then srflx/prflx/relay, breaking ties by higher ICE priority value.
func ParseAudioICERtpUDPCandidate(sdpBody string) (ip string, port int, ok bool) {
	ip, port, _, ok = scanICERtpUDP(sdpBody, "audio", true)
	return ip, port, ok
}

// ParseVideoICERtpUDPCandidate is like ParseAudioICERtpUDPCandidate for the first m=video section.
func ParseVideoICERtpUDPCandidate(sdpBody string) (ip string, port int, ok bool) {
	ip, port, _, ok = scanICERtpUDP(sdpBody, "video", true)
	return ip, port, ok
}

// ParseImageICERtpUDPCandidate is like ParseAudioICERtpUDPCandidate for the first m=image section.
func ParseImageICERtpUDPCandidate(sdpBody string) (ip string, port int, ok bool) {
	ip, port, _, ok = scanICERtpUDP(sdpBody, "image", true)
	return ip, port, ok
}

// ParseAudioICERtpUDPCandidateTrickleFragment picks the best UDP RTP candidate when the body has no m=audio
// line (e.g. trickle-only SIP INFO with a=candidate lines). Returns false if m=audio is present.
func ParseAudioICERtpUDPCandidateTrickleFragment(sdpBody string) (ip string, port int, ok bool) {
	ip, port, _, ok = parseICERtpUDPCandidateTrickleFragment(sdpBody, "audio")
	return ip, port, ok
}

// ParseVideoICERtpUDPCandidateTrickleFragment is the m=video analogue of ParseAudioICERtpUDPCandidateTrickleFragment.
func ParseVideoICERtpUDPCandidateTrickleFragment(sdpBody string) (ip string, port int, ok bool) {
	ip, port, _, ok = parseICERtpUDPCandidateTrickleFragment(sdpBody, "video")
	return ip, port, ok
}

// ParseImageICERtpUDPCandidateTrickleFragment is the m=image analogue of ParseAudioICERtpUDPCandidateTrickleFragment.
func ParseImageICERtpUDPCandidateTrickleFragment(sdpBody string) (ip string, port int, ok bool) {
	ip, port, _, ok = parseICERtpUDPCandidateTrickleFragment(sdpBody, "image")
	return ip, port, ok
}

// shouldPlaceholderMediaEndpoint is true when c=/m= use WebRTC-style placeholders (0.0.0.0 / :: or discarded port 9)
// so a=candidate in the matching m= section should supply the real RTP address.
func shouldPlaceholderMediaEndpoint(ip string, port int) bool {
	if port == 9 || port <= 0 {
		return true
	}
	s := strings.TrimSpace(ip)
	if parsed := net.ParseIP(s); parsed != nil && parsed.IsUnspecified() {
		return true
	}
	return false
}

// PickMediaICERTPEndpoint returns the best RTP address from ICE for m=<media> when placeholders need replacement
// or when a trickle-only SDP fragment supplies candidates for that media (see Parse*TrickleFragment).
// media must be audio, video, or image. iceTyp is the chosen candidate typ (e.g. relay) when ok.
func PickMediaICERTPEndpoint(sdpBody string, media string, cIP string, mPort int) (ip string, port int, iceTyp string, ok bool) {
	if !shouldPlaceholderMediaEndpoint(cIP, mPort) && mPort > 0 {
		return "", 0, "", false
	}
	media = strings.ToLower(strings.TrimSpace(media))
	if a, b, t, ok2 := scanICERtpUDP(sdpBody, media, true); ok2 {
		return a, b, t, ok2
	}
	switch media {
	case "audio":
		return parseICERtpUDPCandidateTrickleFragment(sdpBody, "audio")
	case "video":
		return parseICERtpUDPCandidateTrickleFragment(sdpBody, "video")
	case "image":
		return parseICERtpUDPCandidateTrickleFragment(sdpBody, "image")
	default:
		return "", 0, "", false
	}
}

// PickAudioICERTPEndpoint returns the best RTP address from ICE when placeholders need replacement
// (0.0.0.0 / :: / m= port 9 / missing m=) or when trickle-only SDP supplies candidates.
func PickAudioICERTPEndpoint(sdpBody string, cIP string, mPort int) (ip string, port int, iceTyp string, ok bool) {
	return PickMediaICERTPEndpoint(sdpBody, "audio", cIP, mPort)
}
