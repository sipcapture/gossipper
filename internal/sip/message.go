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
		cpy.Headers[k] = append([]string(nil), v...)
	}
	return cpy
}

// CopyInto copies src into dst, reusing dst's Headers map. Use when adapting
// a Message value (e.g. from transport) to a pooled *Message for the engine.
func CopyInto(dst *Message, src Message) {
	for k := range dst.Headers {
		delete(dst.Headers, k)
	}
	dst.StartLine, dst.Method, dst.RequestURI = src.StartLine, src.Method, src.RequestURI
	dst.StatusCode, dst.Reason = src.StatusCode, src.Reason
	dst.Body, dst.Raw = src.Body, src.Raw
	if dst.Headers == nil {
		dst.Headers = make(map[string][]string)
	}
	for k, v := range src.Headers {
		dst.Headers[k] = append([]string(nil), v...)
	}
}

// PutMessage returns a Message to the pool. Clear Headers before putting if reused.
func PutMessage(m *Message) {
	for k := range m.Headers {
		delete(m.Headers, k)
	}
	m.StartLine, m.Method, m.RequestURI = "", "", ""
	m.StatusCode, m.Reason = 0, ""
	m.Body, m.Raw = "", ""
	msgPool.Put(m)
}

// ParseInto parses raw into msg, reusing msg's Headers map.
// Safe for concurrent use provided each goroutine holds exclusive ownership of msg
// (i.e. obtained via GetMessage and returned via PutMessage).
func ParseInto(msg *Message, raw []byte) error {
	if msg.Headers == nil {
		msg.Headers = make(map[string][]string)
	} else {
		for k := range msg.Headers {
			delete(msg.Headers, k)
		}
	}
	msg.Method, msg.RequestURI, msg.StatusCode, msg.Reason, msg.Body = "", "", 0, "", ""
	return parseBytes(raw, msg)
}

// parseBytes is the shared byte scanner used by both Parse and ParseInto.
// It scans raw without intermediate string copies (no ReplaceAll, no Split).
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
			line := string(raw[lineStart:end])

			switch lineIdx {
			case 0:
				line = strings.TrimSpace(line)
				if line == "" {
					return errors.New("empty SIP message")
				}
				msg.StartLine = line
				if strings.HasPrefix(line, "SIP/2.0 ") {
					parts := strings.SplitN(line, " ", 3)
					if len(parts) < 3 {
						return fmt.Errorf("invalid SIP status line %q", line)
					}
					code, err := strconv.Atoi(parts[1])
					if err != nil {
						return err
					}
					msg.StatusCode = code
					msg.Reason = parts[2]
				} else {
					parts := strings.SplitN(line, " ", 3)
					if len(parts) < 3 {
						return fmt.Errorf("invalid SIP request line %q", line)
					}
					msg.Method = parts[0]
					msg.RequestURI = parts[1]
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
		msg.Body = string(raw[bodyByteOffset:])
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

// ViaBranch extracts the branch parameter from the topmost Via header.
// RFC 3261 §17.1.3: responses are matched to client transactions by the
// branch parameter in the top Via header field.
func ViaBranch(headers map[string][]string) (string, bool) {
	val, ok := Header(headers, "Via")
	if !ok || val == "" {
		return "", false
	}
	// Via params are semicolon-delimited after the sent-by.
	for _, param := range strings.Split(val, ";") {
		param = strings.TrimSpace(param)
		if strings.HasPrefix(strings.ToLower(param), "branch=") {
			return param[len("branch="):], true
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
