package pcap2scenario

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// GenerateScenarios returns the XML text for a UAC and a UAS scenario based on
// the extracted dialog.  callerRTPFile / calleeRTPFile are the relative paths
// (basename) to the mini-PCAP files placed next to the scenario XMLs.
func GenerateScenarios(dlg Dialog, callerRTPFile, calleeRTPFile, pcapName string) (uacXML, uasXML string) {
	uacXML = buildUAC(dlg, callerRTPFile, pcapName)
	uasXML = buildUAS(dlg, calleeRTPFile, pcapName)
	return uacXML, uasXML
}

// ── UAC scenario ──────────────────────────────────────────────────────────────

func buildUAC(dlg Dialog, rtpFile, pcapName string) string {
	durationMS := int(dlg.CallDuration.Milliseconds())

	invite := templatizeUAC(dlg.INVITE.Message.Raw, dlg)
	ack := templatizeUAC(dlg.ACK.Message.Raw, dlg)
	bye := templatizeUAC(dlg.BYE.Message.Raw, dlg)

	// Fall back to minimal synthesised messages if the PCAP was incomplete.
	if invite == "" {
		invite = synthINVITE(dlg)
	}
	if ack == "" {
		ack = synthACK(dlg)
	}
	if bye == "" {
		bye = synthBYE(dlg)
	}

	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	sb.WriteString("\n")
	fmt.Fprintf(&sb, `<scenario name="pcap-uac (from %s)">`, xmlEscape(pcapName))
	sb.WriteString("\n\n")

	// Send INVITE
	sb.WriteString("  <send retrans=\"500\">\n")
	sb.WriteString("    <![CDATA[\n")
	sb.WriteString(indent(invite, "    "))
	sb.WriteString("\n    ]]>\n  </send>\n\n")

	// Receive 100 / 180 (optional) then 200
	sb.WriteString("  <recv response=\"100\" optional=\"true\"/>\n")
	sb.WriteString("  <recv response=\"180\" optional=\"true\"/>\n")
	sb.WriteString("  <recv response=\"200\" rtd=\"true\">\n")
	sb.WriteString("    <action>\n")
	fmt.Fprintf(&sb, "      <exec play_pcap_audio=\"%s\"/>\n", xmlEscape(rtpFile))
	sb.WriteString("    </action>\n")
	sb.WriteString("  </recv>\n\n")

	// Send ACK
	sb.WriteString("  <send>\n")
	sb.WriteString("    <![CDATA[\n")
	sb.WriteString(indent(ack, "    "))
	sb.WriteString("\n    ]]>\n  </send>\n\n")

	// Hold the call for its original duration
	fmt.Fprintf(&sb, "  <pause milliseconds=\"%d\"/>\n\n", durationMS)

	// Stop RTP
	sb.WriteString("  <nop>\n")
	sb.WriteString("    <action>\n")
	sb.WriteString("      <exec rtp_stream=\"stop\"/>\n")
	sb.WriteString("    </action>\n")
	sb.WriteString("  </nop>\n\n")

	// Send BYE
	if dlg.BYE.Message.Method != "" {
		sb.WriteString("  <send retrans=\"500\">\n")
		sb.WriteString("    <![CDATA[\n")
		sb.WriteString(indent(bye, "    "))
		sb.WriteString("\n    ]]>\n  </send>\n\n")
		sb.WriteString("  <recv response=\"200\"/>\n\n")
	}

	sb.WriteString("</scenario>\n")
	return sb.String()
}

// ── UAS scenario ──────────────────────────────────────────────────────────────

func buildUAS(dlg Dialog, rtpFile, pcapName string) string {
	durationMS := int(dlg.CallDuration.Milliseconds())

	// Build SDP body from the callee's 200 OK.
	sdpBody := buildCalleeSDP(dlg)

	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	sb.WriteString("\n")
	fmt.Fprintf(&sb, `<scenario name="pcap-uas (from %s)">`, xmlEscape(pcapName))
	sb.WriteString("\n\n")

	// Wait for INVITE
	sb.WriteString("  <recv request=\"INVITE\"/>\n\n")

	// 180 Ringing
	sb.WriteString("  <send>\n")
	sb.WriteString("    <![CDATA[\n")
	sb.WriteString("    SIP/2.0 180 Ringing\n")
	sb.WriteString("    [last_Via:]\n")
	sb.WriteString("    [last_From:]\n")
	sb.WriteString("    [last_To:];tag=[pid]GossipTag01[call_number]\n")
	sb.WriteString("    [last_Call-ID:]\n")
	sb.WriteString("    [last_CSeq:]\n")
	sb.WriteString("    Contact: <sip:[local_ip]:[local_port];transport=[transport]>\n")
	sb.WriteString("    Content-Length: 0\n")
	sb.WriteString("\n    ]]>\n  </send>\n\n")

	// 200 OK with SDP
	sb.WriteString("  <send retrans=\"500\">\n")
	sb.WriteString("    <![CDATA[\n")
	sb.WriteString("    SIP/2.0 200 OK\n")
	sb.WriteString("    [last_Via:]\n")
	sb.WriteString("    [last_From:]\n")
	sb.WriteString("    [last_To:];tag=[pid]GossipTag01[call_number]\n")
	sb.WriteString("    [last_Call-ID:]\n")
	sb.WriteString("    [last_CSeq:]\n")
	sb.WriteString("    Contact: <sip:[local_ip]:[local_port];transport=[transport]>\n")
	sb.WriteString("    Content-Type: application/sdp\n")
	sb.WriteString("    Content-Length: [len]\n")
	sb.WriteString("\n")
	for _, sdpLine := range strings.Split(sdpBody, "\n") {
		fmt.Fprintf(&sb, "    %s\n", sdpLine)
	}
	sb.WriteString("\n    ]]>\n  </send>\n\n")

	// Receive ACK — start playing RTP immediately
	sb.WriteString("  <recv request=\"ACK\">\n")
	sb.WriteString("    <action>\n")
	fmt.Fprintf(&sb, "      <exec play_pcap_audio=\"%s\"/>\n", xmlEscape(rtpFile))
	sb.WriteString("    </action>\n")
	sb.WriteString("  </recv>\n\n")

	// Hold for call duration
	fmt.Fprintf(&sb, "  <pause milliseconds=\"%d\"/>\n\n", durationMS)

	// Stop RTP
	sb.WriteString("  <nop>\n")
	sb.WriteString("    <action>\n")
	sb.WriteString("      <exec rtp_stream=\"stop\"/>\n")
	sb.WriteString("    </action>\n")
	sb.WriteString("  </nop>\n\n")

	// Receive BYE and reply 200
	sb.WriteString("  <recv request=\"BYE\"/>\n\n")

	sb.WriteString("  <send>\n")
	sb.WriteString("    <![CDATA[\n")
	sb.WriteString("    SIP/2.0 200 OK\n")
	sb.WriteString("    [last_Via:]\n")
	sb.WriteString("    [last_From:]\n")
	sb.WriteString("    [last_To:];tag=[pid]GossipTag01[call_number]\n")
	sb.WriteString("    [last_Call-ID:]\n")
	sb.WriteString("    [last_CSeq:]\n")
	sb.WriteString("    Content-Length: 0\n")
	sb.WriteString("\n    ]]>\n  </send>\n\n")

	sb.WriteString("</scenario>\n")
	return sb.String()
}

// ── SDP helpers ───────────────────────────────────────────────────────────────

// buildCalleeSDP returns a templatised SDP body for the UAS 200 OK, using the
// codec information from the callee's original 200 OK SDP.
func buildCalleeSDP(dlg Dialog) string {
	mediaLine := "RTP/AVP 0"
	codecLines := []string{"a=rtpmap:0 PCMU/8000", "a=sendrecv"}

	if dlg.OK.Message.Body != "" {
		mediaLine = sdpMediaLine(dlg.OK.Message.Body)
		if lines := sdpCodecLines(dlg.OK.Message.Body); len(lines) > 0 {
			codecLines = lines
		}
	}

	var sb strings.Builder
	sb.WriteString("v=0\n")
	sb.WriteString("o=- 0 0 IN IP4 [local_ip]\n")
	sb.WriteString("s=-\n")
	sb.WriteString("c=IN IP4 [local_ip]\n")
	sb.WriteString("t=0 0\n")
	fmt.Fprintf(&sb, "m=audio [media_port] %s\n", mediaLine)
	for _, l := range codecLines {
		sb.WriteString(l)
		sb.WriteString("\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

// ── Templatisation ────────────────────────────────────────────────────────────

// Pre-compiled regexps used by templatizeUAC.
var (
	reVia          = regexp.MustCompile(`(?im)^(v|via):\s*SIP/2\.0/\S+ [^\r\n]+`)
	reCallID       = regexp.MustCompile(`(?im)^(i|call-id):\s*[^\r\n]+`)
	reContentLen   = regexp.MustCompile(`(?im)^(l|content-length):\s*\d+`)
	reFromTag      = regexp.MustCompile(`(?im)^(f|from):[^\r\n]*`)
	reToTagLine    = regexp.MustCompile(`(?im)^(t|to):[^\r\n]*`)
	reRequestLine  = regexp.MustCompile(`(?m)^(INVITE|ACK|BYE|CANCEL|OPTIONS) sip:\S+ SIP/2\.0`)
	reTag          = regexp.MustCompile(`;tag=[^;\s>\r\n]+`)
	reSDPAudioPort = regexp.MustCompile(`(?m)^m=audio \d+`)
	reSDPO         = regexp.MustCompile(`(?m)^o=[^\r\n]+`)
	reSDPC         = regexp.MustCompile(`(?m)^c=IN IP4 \S+`)
)

// templatizeUAC transforms a raw SIP message (from the PCAP) into a gossipper
// UAC scenario template by replacing concrete addresses with variables.
func templatizeUAC(raw string, dlg Dialog) string {
	if raw == "" {
		return ""
	}
	s := raw

	// 1. Replace concrete IP:port strings (more specific before bare IP).
	if dlg.CallerSIPPort > 0 {
		s = strings.ReplaceAll(s,
			dlg.CallerIP+":"+strconv.Itoa(dlg.CallerSIPPort),
			"[local_ip]:[local_port]")
	}
	if dlg.CalleeSIPPort > 0 {
		s = strings.ReplaceAll(s,
			dlg.CalleeIP+":"+strconv.Itoa(dlg.CalleeSIPPort),
			"[remote_ip]:[remote_port]")
	}
	// RTP IPs (may differ from SIP signalling IPs).
	if dlg.CallerRTPIP != "" && dlg.CallerRTPIP != dlg.CallerIP {
		s = strings.ReplaceAll(s, dlg.CallerRTPIP, "[local_ip]")
	}
	if dlg.CalleeRTPIP != "" && dlg.CalleeRTPIP != dlg.CalleeIP {
		s = strings.ReplaceAll(s, dlg.CalleeRTPIP, "[remote_ip]")
	}
	// Bare IPs.
	if dlg.CallerIP != "" {
		s = strings.ReplaceAll(s, dlg.CallerIP, "[local_ip]")
	}
	if dlg.CalleeIP != "" {
		s = strings.ReplaceAll(s, dlg.CalleeIP, "[remote_ip]")
	}

	// 2. Via header → full template.
	s = reVia.ReplaceAllString(s, "Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]")

	// 3. Call-ID.
	s = reCallID.ReplaceAllString(s, "Call-ID: [call_id]")

	// 4. Content-Length.
	if hasBody(s) {
		s = reContentLen.ReplaceAllString(s, "Content-Length: [len]")
	} else {
		s = reContentLen.ReplaceAllString(s, "Content-Length: 0")
	}

	// 5. From tag → gossipper UAC tag.
	s = reFromTag.ReplaceAllStringFunc(s, func(line string) string {
		return reTag.ReplaceAllString(line, ";tag=[pid]GossipTag00[call_number]")
	})

	// 6. To tag → peer tag variable (used in ACK/BYE).
	s = reToTagLine.ReplaceAllStringFunc(s, func(line string) string {
		return reTag.ReplaceAllString(line, "[peer_tag_param]")
	})

	// 7. Request URI first line: keep method + SIP/2.0, replace URI.
	s = reRequestLine.ReplaceAllStringFunc(s, func(m string) string {
		fields := strings.Fields(m)
		if len(fields) >= 3 {
			return fields[0] + " sip:[service]@[remote_ip]:[remote_port] " + fields[2]
		}
		return m
	})

	// 8. SDP: normalise o= session line.
	s = reSDPO.ReplaceAllString(s, "o=- 0 0 IN IP4 [local_ip]")

	// 9. SDP: c= connection address.
	s = reSDPC.ReplaceAllString(s, "c=IN IP4 [local_ip]")

	// 10. SDP: m=audio port.
	s = reSDPAudioPort.ReplaceAllString(s, "m=audio [media_port]")

	return strings.TrimRight(s, "\r\n")
}

// ── Fallback synthetic messages ───────────────────────────────────────────────

func synthINVITE(dlg Dialog) string {
	mediaLine := sdpMediaLine(dlg.INVITE.Message.Body)
	codecLines := sdpCodecLines(dlg.INVITE.Message.Body)
	if mediaLine == "" {
		mediaLine = "RTP/AVP 0"
	}
	if len(codecLines) == 0 {
		codecLines = []string{"a=rtpmap:0 PCMU/8000", "a=sendrecv"}
	}

	sdp := buildSDP("[local_ip]", "[media_port]", mediaLine, codecLines)
	sdpLen := len(sdp)

	var sb strings.Builder
	fmt.Fprintf(&sb, "INVITE sip:[service]@[remote_ip]:[remote_port] SIP/2.0\r\n")
	sb.WriteString("Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]\r\n")
	sb.WriteString("From: <sip:[service]@[local_ip]:[local_port]>;tag=[pid]GossipTag00[call_number]\r\n")
	sb.WriteString("To: <sip:[service]@[remote_ip]:[remote_port]>\r\n")
	sb.WriteString("Call-ID: [call_id]\r\n")
	sb.WriteString("CSeq: 1 INVITE\r\n")
	sb.WriteString("Contact: <sip:[service]@[local_ip]:[local_port];transport=[transport]>\r\n")
	sb.WriteString("Max-Forwards: 70\r\n")
	sb.WriteString("Content-Type: application/sdp\r\n")
	fmt.Fprintf(&sb, "Content-Length: %d\r\n", sdpLen)
	sb.WriteString("\r\n")
	sb.WriteString(sdp)
	return sb.String()
}

func synthACK(dlg Dialog) string {
	return "ACK sip:[service]@[remote_ip]:[remote_port] SIP/2.0\r\n" +
		"Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]\r\n" +
		"From: <sip:[service]@[local_ip]:[local_port]>;tag=[pid]GossipTag00[call_number]\r\n" +
		"To: <sip:[service]@[remote_ip]:[remote_port]>[peer_tag_param]\r\n" +
		"Call-ID: [call_id]\r\n" +
		"CSeq: 1 ACK\r\n" +
		"Contact: <sip:[service]@[local_ip]:[local_port];transport=[transport]>\r\n" +
		"Max-Forwards: 70\r\n" +
		"Content-Length: 0\r\n" +
		"\r\n"
}

func synthBYE(dlg Dialog) string {
	return "BYE sip:[service]@[remote_ip]:[remote_port] SIP/2.0\r\n" +
		"Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]\r\n" +
		"From: <sip:[service]@[local_ip]:[local_port]>;tag=[pid]GossipTag00[call_number]\r\n" +
		"To: <sip:[service]@[remote_ip]:[remote_port]>[peer_tag_param]\r\n" +
		"Call-ID: [call_id]\r\n" +
		"CSeq: 2 BYE\r\n" +
		"Contact: <sip:[service]@[local_ip]:[local_port];transport=[transport]>\r\n" +
		"Max-Forwards: 70\r\n" +
		"Content-Length: 0\r\n" +
		"\r\n"
}

func buildSDP(ip, port, mediaLine string, codecLines []string) string {
	var sb strings.Builder
	sb.WriteString("v=0\r\n")
	fmt.Fprintf(&sb, "o=- 0 0 IN IP4 %s\r\n", ip)
	sb.WriteString("s=-\r\n")
	fmt.Fprintf(&sb, "c=IN IP4 %s\r\n", ip)
	sb.WriteString("t=0 0\r\n")
	fmt.Fprintf(&sb, "m=audio %s %s\r\n", port, mediaLine)
	for _, l := range codecLines {
		sb.WriteString(l)
		sb.WriteString("\r\n")
	}
	return sb.String()
}

// ── Utilities ─────────────────────────────────────────────────────────────────

// hasBody returns true if the raw SIP message has a non-empty body section.
func hasBody(raw string) bool {
	if idx := strings.Index(raw, "\r\n\r\n"); idx >= 0 {
		return idx+4 < len(raw)
	}
	if idx := strings.Index(raw, "\n\n"); idx >= 0 {
		return idx+2 < len(raw)
	}
	return false
}

// indent prefixes every non-empty line with the given padding string.
func indent(s, pad string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if l != "" {
			lines[i] = pad + l
		}
	}
	return strings.Join(lines, "\n")
}

// xmlEscape replaces the minimal set of characters that must be escaped in
// XML attribute values and text content.
func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	return s
}
