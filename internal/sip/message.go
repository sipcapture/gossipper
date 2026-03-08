package sip

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

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

func Parse(raw []byte) (Message, error) {
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) == "" {
		return Message{}, errors.New("empty SIP message")
	}

	msg := Message{
		StartLine: strings.TrimSpace(lines[0]),
		Headers:   make(map[string][]string),
		Raw:       string(raw),
	}

	if strings.HasPrefix(msg.StartLine, "SIP/2.0 ") {
		parts := strings.SplitN(msg.StartLine, " ", 3)
		if len(parts) < 3 {
			return Message{}, fmt.Errorf("invalid SIP status line %q", msg.StartLine)
		}
		code, err := strconv.Atoi(parts[1])
		if err != nil {
			return Message{}, err
		}
		msg.StatusCode = code
		msg.Reason = parts[2]
	} else {
		parts := strings.SplitN(msg.StartLine, " ", 3)
		if len(parts) < 3 {
			return Message{}, fmt.Errorf("invalid SIP request line %q", msg.StartLine)
		}
		msg.Method = parts[0]
		msg.RequestURI = parts[1]
	}

	bodyIndex := -1
	for i := 1; i < len(lines); i++ {
		line := strings.TrimRight(lines[i], "\r")
		if line == "" {
			bodyIndex = i + 1
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			return Message{}, fmt.Errorf("malformed SIP header %q", line)
		}
		msg.Headers[strings.TrimSpace(name)] = append(msg.Headers[strings.TrimSpace(name)], strings.TrimSpace(value))
	}

	if bodyIndex != -1 && bodyIndex < len(lines) {
		msg.Body = strings.Join(lines[bodyIndex:], "\n")
	}

	return msg, nil
}

func ExtractCallID(raw []byte) (string, error) {
	msg, err := Parse(raw)
	if err != nil {
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
