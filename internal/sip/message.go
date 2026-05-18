package sip

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

var msgPool = sync.Pool{
	New: func() interface{} {
		return &Message{Headers: make(map[string][]string)}
	},
}

type Message struct {
	StartLine  string
	Method     string
	RequestURI string
	StatusCode int
	Reason     string
	Headers    map[string][]string
	Body       string
	Raw        string
}

// Parse parses raw bytes into a new Message value.
// Uses the same byte scanner as ParseInto to avoid intermediate allocations.
func Parse(raw []byte) (Message, error) {
	var msg Message
	msg.Headers = make(map[string][]string)
	if err := parseBytes(raw, &msg); err != nil {
		return Message{}, err
	}
	return msg, nil
}

// GetMessage returns a Message from the pool for reuse. Call PutMessage when done.
func GetMessage() *Message { return msgPool.Get().(*Message) }

// Copy returns a deep copy of the message. Use when passing a pooled Message
// to code that will outlive the current scope (caller must call PutMessage).
func (m *Message) Copy() Message {
	cpy := Message{
		StartLine:  m.StartLine,
		Method:     m.Method,
		RequestURI: m.RequestURI,
		StatusCode: m.StatusCode,
		Reason:     m.Reason,
		Body:       m.Body,
		Raw:        m.Raw,
		Headers:    make(map[string][]string, len(m.Headers)),
	}
	for k, v := range m.Headers {
		if len(v) == 0 {
			continue
		}
		cpy.Headers[k] = append([]string(nil), v...)
	}
	return cpy
}

// CopyInto copies src into dst, reusing dst's Headers map. Use when adapting
// a Message value (e.g. from transport) to a pooled *Message for the engine.
func CopyInto(dst *Message, src Message) {
	for k, vals := range dst.Headers {
		clear(vals)
		dst.Headers[k] = vals[:0]
	}
	dst.StartLine, dst.Method, dst.RequestURI = src.StartLine, src.Method, src.RequestURI
	dst.StatusCode, dst.Reason = src.StatusCode, src.Reason
	dst.Body, dst.Raw = src.Body, src.Raw
	if dst.Headers == nil {
		dst.Headers = make(map[string][]string)
	}
	for k, v := range src.Headers {
		if len(v) == 0 {
			continue
		}
		dst.Headers[k] = append(dst.Headers[k], v...)
	}
}

// PutMessage returns a Message to the pool. Clears string references in
// header slice backing arrays to allow GC of the underlying msg.Raw, then
// truncates to zero-length so ParseInto can reuse the backing arrays.
func PutMessage(m *Message) {
	for k, vals := range m.Headers {
		clear(vals) // release substring references to msg.Raw
		m.Headers[k] = vals[:0]
	}
	m.StartLine, m.Method, m.RequestURI = "", "", ""
	m.StatusCode, m.Reason = 0, ""
	m.Body, m.Raw = "", ""
	msgPool.Put(m)
}

// ParseInto parses raw into msg, reusing msg's Headers map and slice backing.
// Safe for concurrent use provided each goroutine holds exclusive ownership of msg
// (i.e. obtained via GetMessage and returned via PutMessage).
func ParseInto(msg *Message, raw []byte) error {
	if msg.Headers == nil {
		msg.Headers = make(map[string][]string)
	} else {
		for k, vals := range msg.Headers {
			clear(vals)
			msg.Headers[k] = vals[:0]
		}
	}
	msg.Method, msg.RequestURI, msg.StatusCode, msg.Reason, msg.Body = "", "", 0, "", ""
	return parseBytes(raw, msg)
}

// parseBytes is the shared byte scanner used by both Parse and ParseInto.
// It uses substrings of msg.Raw to avoid per-line heap allocations.
func parseBytes(raw []byte, msg *Message) error {
	msg.Raw = string(raw)

	lineStart := 0
	lineIdx := 0
	bodyByteOffset := -1

	for i := 0; i <= len(raw); i++ {
		if i == len(raw) || raw[i] == '\n' {
			end := i
			if end > lineStart && raw[end-1] == '\r' {
				end--
			}
			// Substring of msg.Raw — shares backing memory, no allocation.
			line := msg.Raw[lineStart:end]

			switch lineIdx {
			case 0:
				line = strings.TrimSpace(line)
				if line == "" {
					return errors.New("empty SIP message")
				}
				msg.StartLine = line
				if len(line) > 8 && line[:8] == "SIP/2.0 " {
					// Parse status line: "SIP/2.0 200 OK"
					rest := line[8:]
					sp := strings.IndexByte(rest, ' ')
					if sp < 0 {
						return fmt.Errorf("invalid SIP status line %q", line)
					}
					code, err := strconv.Atoi(rest[:sp])
					if err != nil {
						return err
					}
					msg.StatusCode = code
					msg.Reason = rest[sp+1:]
				} else {
					// Parse request line: "INVITE sip:... SIP/2.0"
					sp1 := strings.IndexByte(line, ' ')
					if sp1 < 0 {
						return fmt.Errorf("invalid SIP request line %q", line)
					}
					sp2 := strings.IndexByte(line[sp1+1:], ' ')
					if sp2 < 0 {
						return fmt.Errorf("invalid SIP request line %q", line)
					}
					msg.Method = line[:sp1]
					msg.RequestURI = line[sp1+1 : sp1+1+sp2]
				}
			default:
				if line == "" {
					bodyByteOffset = i + 1
					break
				}
				name, value, ok := strings.Cut(line, ":")
				if !ok {
					return fmt.Errorf("malformed SIP header %q", line)
				}
				k := strings.TrimSpace(name)
				msg.Headers[k] = append(msg.Headers[k], strings.TrimSpace(value))
			}

			if bodyByteOffset >= 0 {
				break
			}
			lineStart = i + 1
			lineIdx++
		}
	}

	if bodyByteOffset >= 0 && bodyByteOffset < len(raw) {
		msg.Body = msg.Raw[bodyByteOffset:]
	}
	return nil
}

func ExtractCallID(raw []byte) (string, error) {
	msg := GetMessage()
	defer PutMessage(msg)
	if err := ParseInto(msg, raw); err != nil {
		return "", err
	}
	callID, ok := Header(msg.Headers, "Call-ID")
	if !ok {
		return "", errors.New("missing Call-ID header")
	}
	return callID, nil
}

func Header(headers map[string][]string, name string) (string, bool) {
	for key, values := range headers {
		if strings.EqualFold(key, name) && len(values) > 0 {
			return values[0], true
		}
	}
	return "", false
}

// NormalizeCallID strips an optional SIPp-style "prefix///" segment from a
// Call-ID value so dialog correlation can match peers that use the documented
// prefix form (RFC-compatible Call-ID can contain '/' characters; SIPp uses
// '///' as a sentinel separator). The keyword reference is at:
// https://sipp.readthedocs.io/en/latest/scenarios/keywords.html#call-id
//
// If no "///" sentinel is present the value is returned unchanged. Surrounding
// whitespace is trimmed so headers that retain wrapping or padding compare
// equal to bare Call-ID values.
func NormalizeCallID(value string) string {
	v := strings.TrimSpace(value)
	if v == "" {
		return v
	}
	if i := strings.LastIndex(v, "///"); i >= 0 {
		return v[i+3:]
	}
	return v
}

func Match(msg Message, request, response string) bool {
	return MatchRecv(msg, request, response, nil)
}

// ParseCSeq parses a CSeq header value such as "42 BYE" into number and method.
func ParseCSeq(headers map[string][]string) (num int, method string, ok bool) {
	v, ok := Header(headers, "CSeq")
	if !ok {
		return 0, "", false
	}
	fields := strings.Fields(strings.TrimSpace(v))
	if len(fields) < 2 {
		return 0, "", false
	}
	n, err := strconv.Atoi(fields[0])
	if err != nil {
		return 0, "", false
	}
	return n, fields[len(fields)-1], true
}

func parseRecvResponseFilter(response string) (code int, ok bool) {
	fields := strings.Fields(strings.TrimSpace(response))
	if len(fields) == 0 {
		return 0, false
	}
	n, err := strconv.Atoi(fields[0])
	if err != nil {
		return 0, false
	}
	return n, true
}

// ResponseStatusMatches reports whether msg is a SIP response whose status
// code equals the numeric prefix of recvResp (e.g. "200" or "200 OK").
func ResponseStatusMatches(msg Message, recvResp string) bool {
	wantCode, ok := parseRecvResponseFilter(recvResp)
	return ok && msg.StatusCode == wantCode
}

// MatchRecv matches a <recv> filter against a SIP message. For incoming
// requests, behavior matches Match. For responses (recv response="…"), when
// lastSent contains the last outgoing SIP request on this leg, the response
// must carry the same CSeq number and method as that request (RFC 3261), so a
// stray retransmitted INVITE 200 cannot satisfy recv(200) waiting for the BYE
// transaction.
func MatchRecv(msg Message, request, response string, lastSent []byte) bool {
	if request != "" {
		return strings.EqualFold(msg.Method, request)
	}
	if response == "" {
		return false
	}
	wantCode, ok := parseRecvResponseFilter(response)
	if !ok || msg.StatusCode != wantCode {
		return false
	}
	if len(lastSent) == 0 {
		return true
	}
	req := GetMessage()
	defer PutMessage(req)
	if err := ParseInto(req, lastSent); err != nil {
		return true
	}
	if req.StatusCode != 0 || req.Method == "" {
		// Not a request (e.g. garbage or accidental response bytes): fall back.
		return true
	}
	reqNum, reqMeth, ok1 := ParseCSeq(req.Headers)
	respNum, respMeth, ok2 := ParseCSeq(msg.Headers)
	if !ok1 || !ok2 {
		return true
	}
	return reqNum == respNum && strings.EqualFold(strings.TrimSpace(reqMeth), strings.TrimSpace(respMeth))
}

// MatchRecvCached is a zero-allocation variant of MatchRecv that uses
// pre-extracted CSeq number and method from lastSent.
func MatchRecvCached(msg Message, request, response string, cseqNum int, cseqMethod string) bool {
	if request != "" {
		return strings.EqualFold(msg.Method, request)
	}
	if response == "" {
		return false
	}
	wantCode, ok := parseRecvResponseFilter(response)
	if !ok || msg.StatusCode != wantCode {
		return false
	}
	if cseqMethod == "" {
		return true
	}
	respNum, respMeth, ok := ParseCSeq(msg.Headers)
	if !ok {
		return true
	}
	return cseqNum == respNum && strings.EqualFold(cseqMethod, strings.TrimSpace(respMeth))
}

// ViaBranch extracts the branch parameter from the topmost Via header.
// RFC 3261 §17.1.3: responses are matched to client transactions by the
// branch parameter in the top Via header field.
func ViaBranch(headers map[string][]string) (string, bool) {
	val, ok := Header(headers, "Via")
	if !ok || val == "" {
		return "", false
	}
	// Scan for ";branch=" without allocation.
	for i := 0; i < len(val); i++ {
		if val[i] != ';' {
			continue
		}
		// Skip whitespace after semicolon.
		j := i + 1
		for j < len(val) && (val[j] == ' ' || val[j] == '\t') {
			j++
		}
		if j+7 <= len(val) &&
			(val[j] == 'b' || val[j] == 'B') &&
			(val[j+1] == 'r' || val[j+1] == 'R') &&
			(val[j+2] == 'a' || val[j+2] == 'A') &&
			(val[j+3] == 'n' || val[j+3] == 'N') &&
			(val[j+4] == 'c' || val[j+4] == 'C') &&
			(val[j+5] == 'h' || val[j+5] == 'H') &&
			val[j+6] == '=' {
			start := j + 7
			end := strings.IndexByte(val[start:], ';')
			if end < 0 {
				return strings.TrimSpace(val[start:]), true
			}
			return strings.TrimSpace(val[start : start+end]), true
		}
	}
	return "", false
}

// ResponseMatchesTransaction returns true if the response belongs to the
// same transaction as lastSent per RFC 3261 §17.1.3: the Via branch of the
// response must equal the Via branch of the request, and the CSeq method
// must match. Returns true (safe default) if either side cannot be parsed.
func ResponseMatchesTransaction(msg Message, lastSent []byte) bool {
	if len(lastSent) == 0 {
		return true
	}
	req := GetMessage()
	defer PutMessage(req)
	if err := ParseInto(req, lastSent); err != nil {
		return true
	}
	if req.StatusCode != 0 || req.Method == "" {
		return true
	}
	reqBranch, ok1 := ViaBranch(req.Headers)
	respBranch, ok2 := ViaBranch(msg.Headers)
	if !ok1 || !ok2 {
		return true
	}
	if !strings.EqualFold(reqBranch, respBranch) {
		return false
	}
	// §17.1.3 also requires the CSeq method to match.
	_, reqMeth, ok1 := ParseCSeq(req.Headers)
	_, respMeth, ok2 := ParseCSeq(msg.Headers)
	if !ok1 || !ok2 {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(reqMeth), strings.TrimSpace(respMeth))
}

// ResponseMatchesCached is a zero-allocation variant of ResponseMatchesTransaction
// that uses pre-extracted branch and method from the last sent request.
// Returns true (safe default) if branch/method are empty.
func ResponseMatchesCached(msg Message, branch, method string) bool {
	if branch == "" {
		return true
	}
	respBranch, ok := ViaBranch(msg.Headers)
	if !ok {
		return true
	}
	if !strings.EqualFold(branch, respBranch) {
		return false
	}
	if method == "" {
		return true
	}
	_, respMeth, ok := ParseCSeq(msg.Headers)
	if !ok {
		return true
	}
	return strings.EqualFold(method, strings.TrimSpace(respMeth))
}

// ViaSentBy parses the topmost Via header and extracts sent-by (host and port).
// Format: Via: SIP/2.0/UDP host:port;branch=... or Via: SIP/2.0/UDP host;branch=...
// Default port is 5060 when omitted.
func ViaSentBy(headers map[string][]string) (host string, port int, ok bool) {
	val, ok := Header(headers, "Via")
	if !ok || val == "" {
		return "", 0, false
	}
	parts := strings.SplitN(val, ";", 2)
	sentBy := strings.TrimSpace(parts[0])
	if idx := strings.LastIndex(sentBy, " "); idx >= 0 {
		sentBy = strings.TrimSpace(sentBy[idx+1:])
	}
	if sentBy == "" {
		return "", 0, false
	}
	port = 5060
	if colon := strings.LastIndex(sentBy, ":"); colon >= 0 {
		host = sentBy[:colon]
		if p, err := strconv.Atoi(sentBy[colon+1:]); err == nil && p > 0 && p < 65536 {
			port = p
		}
	} else {
		host = sentBy
	}
	host = strings.TrimSpace(host)
	if host == "" {
		return "", 0, false
	}
	return host, port, true
}

// BuildResponse constructs a minimal SIP response (e.g. 200 OK) to an
// incoming request. Copies Via, From, To, Call-ID, and CSeq per RFC 3261
// §8.2.6. Returns the raw response bytes ready to send on the wire.
func BuildResponse(req Message, statusCode int, reason string) []byte {
	var b strings.Builder
	b.Grow(256)
	b.WriteString("SIP/2.0 ")
	b.WriteString(strconv.Itoa(statusCode))
	b.WriteByte(' ')
	b.WriteString(reason)
	b.WriteString("\r\n")

	// Via (all values, preserving order)
	for _, key := range []string{"Via", "via", "v"} {
		if vals, ok := req.Headers[key]; ok {
			for _, v := range vals {
				b.WriteString("Via: ")
				b.WriteString(v)
				b.WriteString("\r\n")
			}
		}
	}

	writeHdr := func(name string, aliases ...string) {
		if v, ok := Header(req.Headers, name); ok {
			b.WriteString(name)
			b.WriteString(": ")
			b.WriteString(v)
			b.WriteString("\r\n")
		} else {
			for _, a := range aliases {
				if v, ok := Header(req.Headers, a); ok {
					b.WriteString(name)
					b.WriteString(": ")
					b.WriteString(v)
					b.WriteString("\r\n")
					return
				}
			}
		}
	}
	writeHdr("From", "f")
	writeHdr("To", "t")
	writeHdr("Call-ID", "i")
	writeHdr("CSeq")
	b.WriteString("Content-Length: 0\r\n")
	b.WriteString("\r\n")
	return []byte(b.String())
}
