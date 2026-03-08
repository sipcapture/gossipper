package sip

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

func ReadMessage(reader *bufio.Reader) (Message, error) {
	startLine, err := reader.ReadString('\n')
	if err != nil {
		return Message{}, err
	}
	startLine = strings.TrimRight(startLine, "\r\n")
	if strings.TrimSpace(startLine) == "" {
		return Message{}, fmt.Errorf("empty SIP start line")
	}

	var builder strings.Builder
	builder.WriteString(startLine)
	builder.WriteString("\r\n")

	headers := make(map[string][]string)
	contentLength := 0
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
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
			return Message{}, err
		}
		builder.Write(body)
	}

	return Parse([]byte(builder.String()))
}
