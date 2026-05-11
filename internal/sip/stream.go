package sip

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

func ReadMessage(reader *bufio.Reader) (Message, error) {
	// Skip blank lines before the SIP start line.  RFC 3261 §7.5 requires
	// implementations to silently discard stray CRLFs, and RFC 5626 §4.4.1
	// uses a double-CRLF as a keep-alive ping that must not close the
	// connection.
	var startLine string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) && strings.TrimSpace(line) == "" {
				return Message{}, fmt.Errorf("peer closed connection before any SIP data: %w", err)
			}
			if errors.Is(err, io.EOF) {
				return Message{}, fmt.Errorf("connection closed while reading SIP start line: %w", err)
			}
			return Message{}, err
		}
		startLine = strings.TrimRight(line, "\r\n")
		if strings.TrimSpace(startLine) != "" {
			break
		}
		// blank line (CRLF keep-alive ping) — skip it
	}

	var builder strings.Builder
	builder.WriteString(startLine)
	builder.WriteString("\r\n")

	headers := make(map[string][]string)
	contentLength := 0
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				return Message{}, fmt.Errorf("connection closed while reading SIP headers: %w", err)
			}
			return Message{}, err
		}
		line = strings.TrimRight(line, "\r\n")
		builder.WriteString(line)
		builder.WriteString("\r\n")
		if line == "" {
			break
		}

		name, value, ok := strings.Cut(line, ":")
		if !ok {
			return Message{}, fmt.Errorf("malformed SIP header %q", line)
		}
		name = strings.TrimSpace(name)
		value = strings.TrimSpace(value)
		headers[name] = append(headers[name], value)
		if strings.EqualFold(name, "Content-Length") {
			contentLength, _ = strconv.Atoi(value)
		}
	}

	body := make([]byte, contentLength)
	if contentLength > 0 {
		if _, err := io.ReadFull(reader, body); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return Message{}, fmt.Errorf("connection closed while reading SIP body (%d bytes): %w", contentLength, err)
			}
			return Message{}, err
		}
		builder.Write(body)
	}

	return Parse([]byte(builder.String()))
}
