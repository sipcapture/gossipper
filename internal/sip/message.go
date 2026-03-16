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

func Match(msg Message, request, response string) bool {
	if request != "" {
		return strings.EqualFold(msg.Method, request)
	}
	if response != "" {
		code, err := strconv.Atoi(response)
		if err != nil {
			return false
		}
		return msg.StatusCode == code
	}
	return false
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
