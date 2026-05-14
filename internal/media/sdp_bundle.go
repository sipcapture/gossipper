package media

import (
	"net"
	"strconv"
	"strings"
)

// ParseBundleMIDs returns mids from the first a=group:BUNDLE line (RFC 8843), in order.
func ParseBundleMIDs(sdpBody string) []string {
	body := strings.ReplaceAll(sdpBody, "\r\n", "\n")
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		low := strings.ToLower(line)
		if !strings.HasPrefix(low, "a=group:") {
			continue
		}
		rest := strings.TrimSpace(line[len("a=group:"):])
		fields := strings.Fields(rest)
		if len(fields) < 2 {
			continue
		}
		if !strings.EqualFold(fields[0], "bundle") {
			continue
		}
		out := make([]string, 0, len(fields)-1)
		for _, m := range fields[1:] {
			m = strings.TrimSpace(m)
			if m != "" {
				out = append(out, m)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return nil
}

func containsStringFold(list []string, want string) bool {
	want = strings.TrimSpace(want)
	for _, s := range list {
		if strings.EqualFold(strings.TrimSpace(s), want) {
			return true
		}
	}
	return false
}

func parseSessionCBeforeFirstM(body string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		low := strings.ToLower(line)
		if strings.HasPrefix(low, "m=") {
			return ""
		}
		if strings.HasPrefix(line, "c=") {
			fields := strings.Fields(strings.TrimPrefix(line, "c="))
			if len(fields) >= 3 {
				return fields[2]
			}
		}
	}
	return ""
}

// splitSDPMediaSections splits at each m= line (session preamble is ignored).
func splitSDPMediaSections(body string) []string {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	var secs []string
	var cur []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		low := strings.ToLower(line)
		if strings.HasPrefix(low, "m=") {
			if len(cur) > 0 {
				secs = append(secs, strings.Join(cur, "\n"))
			}
			cur = []string{line}
			continue
		}
		if len(cur) > 0 {
			cur = append(cur, line)
		}
	}
	if len(cur) > 0 {
		secs = append(secs, strings.Join(cur, "\n"))
	}
	return secs
}

func extractMidFromSection(section string) string {
	for _, line := range strings.Split(section, "\n") {
		line = strings.TrimSpace(line)
		low := strings.ToLower(line)
		if strings.HasPrefix(low, "a=mid:") {
			return strings.TrimSpace(line[len("a=mid:"):])
		}
	}
	return ""
}

func mediaPortFromMSection(section string) int {
	first := strings.Split(section, "\n")[0]
	first = strings.TrimSpace(first)
	fields := strings.Fields(first)
	if len(fields) < 2 {
		return 0
	}
	p, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0
	}
	return p
}

func connectionIPFromSection(section, sessionIP string) string {
	ip := sessionIP
	for _, line := range strings.Split(section, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "c=") {
			fields := strings.Fields(strings.TrimPrefix(line, "c="))
			if len(fields) >= 3 {
				ip = fields[2]
			}
		}
	}
	return strings.TrimSpace(ip)
}

// bundleSectionIPPort returns c=/m= IP and port for the m= section whose a=mid matches mid.
func bundleSectionIPPort(sdpBody, mid string) (ip string, port int, ok bool) {
	body := strings.ReplaceAll(sdpBody, "\r\n", "\n")
	sessionIP := parseSessionCBeforeFirstM(body)
	for _, sec := range splitSDPMediaSections(body) {
		if !strings.EqualFold(strings.TrimSpace(extractMidFromSection(sec)), strings.TrimSpace(mid)) {
			continue
		}
		ip = connectionIPFromSection(sec, sessionIP)
		port = mediaPortFromMSection(sec)
		if ip != "" && port > 0 {
			return ip, port, true
		}
	}
	return "", 0, false
}

// midForFirstMediaSection returns a=mid value in the first m=<media> block, or "".
func midForFirstMediaSection(sdpBody, media string) string {
	body := strings.ReplaceAll(sdpBody, "\r\n", "\n")
	mediaPrefix := "m=" + strings.ToLower(strings.TrimSpace(media))
	for _, sec := range splitSDPMediaSections(body) {
		first := strings.TrimSpace(strings.Split(sec, "\n")[0])
		if strings.HasPrefix(strings.ToLower(first), mediaPrefix) {
			return extractMidFromSection(sec)
		}
	}
	return ""
}

// ApplyBundleMediaEndpointIfNeeded copies the BUNDLE transport (first mid in a=group:BUNDLE)
// when this media's mid is in the bundle and the current endpoint uses placeholders or zero port.
func ApplyBundleMediaEndpointIfNeeded(sdpBody, media string, ip *string, port *int) {
	if ip == nil || port == nil {
		return
	}
	mids := ParseBundleMIDs(sdpBody)
	if len(mids) == 0 {
		return
	}
	myMid := midForFirstMediaSection(sdpBody, media)
	if myMid == "" || !containsStringFold(mids, myMid) {
		return
	}
	tagMid := mids[0]
	bip, bport, ok := bundleSectionIPPort(sdpBody, tagMid)
	if !ok {
		return
	}
	curIP := strings.TrimSpace(*ip)
	curPort := *port
	need := curPort <= 0 || curPort == 9
	if pa := net.ParseIP(curIP); pa != nil && pa.IsUnspecified() {
		need = true
	}
	if !need && !strings.EqualFold(myMid, tagMid) && (curPort == 0 || curPort == 9) {
		need = true
	}
	if !need {
		return
	}
	*ip = bip
	*port = bport
}
