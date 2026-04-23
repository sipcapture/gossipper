// Package pcap2scenario converts a SIP+RTP PCAP capture into a pair of
// replayable Gossipper scenario files (UAC + UAS) with extracted RTP mini-PCAPs.
package pcap2scenario

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/qxip/gossipper/internal/sip"
)

// RawSIPPacket is a parsed SIP message with its network context from the capture.
type RawSIPPacket struct {
	Index     int
	Timestamp time.Time
	SrcIP     string
	SrcPort   int
	DstIP     string
	DstPort   int
	Message   sip.Message
}

// RawUDPPacket holds the complete capture frame for a UDP datagram so it can be
// written verbatim into a mini-PCAP later.
type RawUDPPacket struct {
	Timestamp time.Time
	SrcIP     string
	SrcPort   int
	DstIP     string
	DstPort   int
	RawFrame  []byte // full frame including link/IP/UDP headers
}

// Dialog is the reconstructed SIP call extracted from the capture.
type Dialog struct {
	CallID string

	// Signalling endpoints (from/to SIP messages)
	CallerIP      string
	CallerSIPPort int
	CalleeIP      string
	CalleeSIPPort int

	// Media endpoints (from SDP)
	CallerRTPIP   string
	CallerRTPPort int
	CalleeRTPIP   string
	CalleeRTPPort int

	// SIP messages from the dialog
	INVITE RawSIPPacket
	OK     RawSIPPacket // 200 OK to INVITE
	ACK    RawSIPPacket
	BYE    RawSIPPacket
	BYEOK  RawSIPPacket // 200 OK to BYE

	// Authentication challenge (401/407 flow).
	// AuthChallengeCode is 0 when no challenge was detected.
	AuthChallengeCode int
	AuthChallenge     RawSIPPacket // 401 or 407 response to the first INVITE
	AuthACK           RawSIPPacket // ACK sent to the 4xx response
	AuthINVITE        RawSIPPacket // second INVITE that carries the credentials

	CallDuration time.Duration
}

// BuildDialog extracts the first complete SIP call dialog from a list of SIP
// messages.  Messages need not be pre-sorted; BuildDialog orders them
// internally.
func BuildDialog(messages []RawSIPPacket) (Dialog, error) {
	// Find the first INVITE — it anchors everything else.
	inviteIdx := -1
	for i, m := range messages {
		if m.Message.Method == "INVITE" {
			inviteIdx = i
			break
		}
	}
	if inviteIdx < 0 {
		return Dialog{}, fmt.Errorf("no INVITE found in capture")
	}

	invite := messages[inviteIdx]
	callID, _ := sip.Header(invite.Message.Headers, "Call-ID")
	if callID == "" {
		return Dialog{}, fmt.Errorf("INVITE has no Call-ID header")
	}

	dlg := Dialog{
		CallID:        callID,
		CallerIP:      invite.SrcIP,
		CallerSIPPort: invite.SrcPort,
		CalleeIP:      invite.DstIP,
		CalleeSIPPort: invite.DstPort,
		INVITE:        invite,
	}

	inviteCSeqNum := parseCSeqNum(invite.Message.Headers)

	// Caller RTP info from INVITE SDP.
	if invite.Message.Body != "" {
		ip, port := parseSDP(invite.Message.Body)
		dlg.CallerRTPPort = port
		if ip != "" {
			dlg.CallerRTPIP = ip
		} else {
			dlg.CallerRTPIP = invite.SrcIP
		}
	}

	// Collect remaining dialog messages by Call-ID.
	for _, m := range messages {
		cid, _ := sip.Header(m.Message.Headers, "Call-ID")
		if cid != callID {
			continue
		}
		cseq, _ := sip.Header(m.Message.Headers, "CSeq")
		cseqUpper := strings.ToUpper(cseq)
		mCSeqNum := parseCSeqNum(m.Message.Headers)

		switch {
		// ── Auth challenge: 401 or 407 in response to the first INVITE ───
		case (m.Message.StatusCode == 401 || m.Message.StatusCode == 407) &&
			strings.Contains(cseqUpper, "INVITE") &&
			mCSeqNum == inviteCSeqNum &&
			dlg.AuthChallengeCode == 0:
			dlg.AuthChallengeCode = m.Message.StatusCode
			dlg.AuthChallenge = m

		// ── ACK to the 4xx (same CSeq number as first INVITE) ────────────
		case m.Message.Method == "ACK" &&
			mCSeqNum == inviteCSeqNum &&
			dlg.AuthChallengeCode != 0 &&
			dlg.AuthACK.Message.Method == "":
			dlg.AuthACK = m

		// ── Second INVITE carrying credentials ────────────────────────────
		case m.Message.Method == "INVITE" &&
			mCSeqNum > inviteCSeqNum &&
			dlg.AuthINVITE.Message.Method == "":
			dlg.AuthINVITE = m
			// Prefer SDP from the authenticated re-INVITE for RTP port.
			if m.Message.Body != "" {
				ip, port := parseSDP(m.Message.Body)
				if port > 0 {
					dlg.CallerRTPPort = port
					if ip != "" {
						dlg.CallerRTPIP = ip
					}
				}
			}

		// ── 200 OK to INVITE ──────────────────────────────────────────────
		case m.Message.StatusCode == 200 &&
			strings.Contains(cseqUpper, "INVITE") &&
			dlg.OK.Message.StatusCode == 0:
			dlg.OK = m
			if m.Message.Body != "" {
				ip, port := parseSDP(m.Message.Body)
				dlg.CalleeRTPPort = port
				if ip != "" {
					dlg.CalleeRTPIP = ip
				} else {
					dlg.CalleeRTPIP = m.SrcIP
				}
			}

		// ── ACK to the 200 OK ─────────────────────────────────────────────
		case m.Message.Method == "ACK" &&
			dlg.OK.Message.StatusCode != 0 &&
			dlg.ACK.Message.Method == "":
			dlg.ACK = m

		case m.Message.Method == "BYE" && dlg.BYE.Message.Method == "":
			dlg.BYE = m

		case m.Message.StatusCode == 200 &&
			strings.Contains(cseqUpper, "BYE") &&
			dlg.BYEOK.Message.StatusCode == 0:
			dlg.BYEOK = m
		}
	}

	if dlg.OK.Message.StatusCode == 0 {
		return Dialog{}, fmt.Errorf("no 200 OK to INVITE found for Call-ID %q", callID)
	}

	// Estimate call duration from timestamps.
	if dlg.BYE.Message.Method != "" &&
		!dlg.OK.Timestamp.IsZero() &&
		!dlg.BYE.Timestamp.IsZero() {
		if d := dlg.BYE.Timestamp.Sub(dlg.OK.Timestamp); d > 0 {
			dlg.CallDuration = d
		}
	}
	if dlg.CallDuration == 0 {
		dlg.CallDuration = 5 * time.Second
	}

	return dlg, nil
}

// parseSDP extracts the RTP connection address (c= line) and audio port
// (m=audio line) from an SDP body.
func parseSDP(body string) (ip string, port int) {
	for _, line := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "c=") {
			fields := strings.Fields(strings.TrimPrefix(line, "c="))
			if len(fields) >= 3 {
				ip = fields[2]
			}
		}
		if strings.HasPrefix(strings.ToLower(line), "m=audio ") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				if p, err := strconv.Atoi(fields[1]); err == nil {
					port = p
				}
			}
		}
	}
	return ip, port
}

// SDPCodecLines returns the codec-related SDP lines from a message body
// (a=rtpmap, a=fmtp, etc.) without the connection/session lines.
func sdpCodecLines(body string) []string {
	var out []string
	for _, line := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "a=rtpmap") ||
			strings.HasPrefix(lower, "a=fmtp") ||
			strings.HasPrefix(lower, "a=sendrecv") ||
			strings.HasPrefix(lower, "a=sendonly") ||
			strings.HasPrefix(lower, "a=recvonly") ||
			strings.HasPrefix(lower, "a=inactive") ||
			strings.HasPrefix(lower, "a=ptime") ||
			strings.HasPrefix(lower, "a=maxptime") {
			out = append(out, line)
		}
	}
	return out
}

// parseCSeqNum extracts the sequence number from a CSeq header value.
// e.g. "2 INVITE" → 2.  Returns 0 on any parse error.
func parseCSeqNum(headers map[string][]string) int {
	cseq, _ := sip.Header(headers, "CSeq")
	fields := strings.Fields(cseq)
	if len(fields) >= 1 {
		if n, err := strconv.Atoi(fields[0]); err == nil {
			return n
		}
	}
	return 0
}

// sdpMediaLine returns the m=audio line payload types from a body.
// e.g. "RTP/AVP 0 8 9" (everything after the port).
func sdpMediaLine(body string) string {
	for _, line := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(line), "m=audio ") {
			fields := strings.Fields(line)
			if len(fields) >= 3 {
				// fields: [m=audio, port, RTP/AVP, pt1, pt2, ...]
				return strings.Join(fields[2:], " ")
			}
		}
	}
	return "RTP/AVP 0"
}
