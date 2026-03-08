package template

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var tokenPattern = regexp.MustCompile(`\[[^\]]+\]`)

type Context struct {
	Service       string
	Transport     string
	RemoteHost    string
	RemoteIP      string
	RemotePort    int
	LocalIP       string
	LocalIPType   string
	LocalPort     int
	MediaIP       string
	MediaIPType   string
	MediaPort     int
	CallID        string
	CSeq          int
	CallNumber    int
	MessageIndex  int
	PID           int
	BranchBase    string
	LastMessage   string
	LastHeaders   map[string][]string
	BodyLength    int
	ExtraKeywords map[string]string
	Variables     map[string]string
	BasePath      string
}

func (c Context) Render(raw string) string {
	lines := splitLines(raw)
	rendered := make([]string, 0, len(lines))
	for _, line := range lines {
		value, keep := c.renderLine(line)
		if !keep {
			continue
		}
		rendered = append(rendered, value)
	}
	return strings.Join(rendered, "\r\n")
}

func RenderMessage(raw string, ctx Context) string {
	first := ctx.Render(raw)
	ctx.BodyLength = computeBodyLength(first)
	return ctx.Render(raw)
}

func (c Context) renderLine(line string) (string, bool) {
	dropLine := false
	value := tokenPattern.ReplaceAllStringFunc(line, func(token string) string {
		replacement, ok, drop := c.resolveToken(token)
		if drop {
			dropLine = true
			return ""
		}
		if !ok {
			return ""
		}
		return replacement
	})
	if dropLine {
		return "", false
	}
	return value, true
}

func (c Context) resolveToken(token string) (string, bool, bool) {
	key := strings.TrimSuffix(strings.TrimPrefix(token, "["), "]")
	if strings.HasPrefix(strings.ToLower(key), "file ") {
		value, ok := renderFileToken(key, c.BasePath)
		return value, ok, false
	}
	if field, ok := renderFieldToken(key, c.BasePath, c.CallNumber); ok {
		return field, true, false
	}
	if strings.HasPrefix(strings.ToLower(key), "last_") {
		header := strings.TrimPrefix(key, "last_")
		if strings.EqualFold(header, "message") {
			return c.LastMessage, true, false
		}
		includeName := strings.HasSuffix(header, ":")
		header = strings.TrimSuffix(header, ":")
		values, ok := lookupHeader(c.LastHeaders, header)
		if !ok {
			return "", false, true
		}
		return renderLastHeader(header, values, includeName), true, false
	}

	base, delta := parseArithmetic(key)
	switch strings.ToLower(base) {
	case "service":
		return c.Service, true, false
	case "remote_host":
		return c.RemoteHost, true, false
	case "remote_ip":
		return c.RemoteIP, true, false
	case "remote_port":
		return strconv.Itoa(c.RemotePort + delta), true, false
	case "local_ip":
		return c.LocalIP, true, false
	case "local_ip_type":
		return c.LocalIPType, true, false
	case "local_port":
		return strconv.Itoa(c.LocalPort + delta), true, false
	case "transport":
		return strings.ToUpper(transportName(c.Transport)), true, false
	case "media_ip":
		return c.MediaIP, true, false
	case "media_ip_type":
		return c.MediaIPType, true, false
	case "media_port":
		return strconv.Itoa(c.MediaPort + delta), true, false
	case "call_id":
		return c.CallID, true, false
	case "cseq":
		return strconv.Itoa(c.CSeq + delta), true, false
	case "call_number":
		return strconv.Itoa(c.CallNumber), true, false
	case "msg_index":
		return strconv.Itoa(c.MessageIndex), true, false
	case "pid":
		return strconv.Itoa(c.PID), true, false
	case "branch":
		if delta == 0 {
			return c.BranchBase, true, false
		}
		return fmt.Sprintf("%s-%d", c.BranchBase, delta), true, false
	case "len":
		return strconv.Itoa(c.BodyLength + delta), true, false
	case "last_message":
		return c.LastMessage, true, false
	case "timestamp":
		return time.Now().Format("2006-01-02 15:04:05"), true, false
	case "date":
		return time.Now().UTC().Format(time.RFC1123), true, false
	case "last_cseq_number":
		return extractCSeqNumber(c.LastHeaders), true, false
	case "next_url":
		return extractContactURI(c.LastHeaders), true, false
	case "peer_tag_param":
		tag := extractPeerTag(c.LastHeaders)
		if tag == "" {
			return "", true, false
		}
		return ";tag=" + tag, true, false
	default:
		if strings.HasPrefix(base, "$") {
			if c.Variables == nil {
				return "", false, false
			}
			v, ok := c.Variables[strings.TrimPrefix(base, "$")]
			return v, ok, false
		}
		if c.ExtraKeywords == nil {
			return "", false, false
		}
		v, ok := c.ExtraKeywords[key]
		return v, ok, false
	}
}

func splitLines(value string) []string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	return strings.Split(value, "\n")
}

func computeBodyLength(msg string) int {
	const sep = "\r\n\r\n"
	index := strings.Index(msg, sep)
	if index == -1 {
		return 0
	}
	return len(msg[index+len(sep):])
}

func parseArithmetic(value string) (string, int) {
	for i := 1; i < len(value); i++ {
		switch value[i] {
		case '+', '-':
			n, err := strconv.Atoi(value[i:])
			if err == nil {
				return value[:i], n
			}
		}
	}
	return value, 0
}

func transportName(mode string) string {
	switch mode {
	case "u1", "un":
		return "udp"
	case "t1", "tn":
		return "tcp"
	case "l1", "ln":
		return "tls"
	default:
		return mode
	}
}

func lookupHeader(headers map[string][]string, name string) ([]string, bool) {
	if headers == nil {
		return nil, false
	}
	for key, values := range headers {
		if strings.EqualFold(key, name) {
			return values, true
		}
	}
	return nil, false
}

func renderLastHeader(name string, values []string, includeName bool) string {
	if len(values) == 0 {
		return ""
	}
	rendered := make([]string, 0, len(values))
	prefix := strings.ToLower(name) + ":"
	for _, value := range values {
		if includeName {
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(value)), prefix) {
				rendered = append(rendered, value)
				continue
			}
			rendered = append(rendered, name+": "+value)
			continue
		}
		rendered = append(rendered, value)
	}
	return strings.Join(rendered, "\r\n")
}

func extractPeerTag(headers map[string][]string) string {
	values, ok := lookupHeader(headers, "To")
	if !ok || len(values) == 0 {
		return ""
	}
	last := values[len(values)-1]
	for _, part := range strings.Split(last, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(strings.ToLower(part), "tag=") {
			return strings.TrimSpace(strings.TrimPrefix(part, "tag="))
		}
	}
	return ""
}

func extractCSeqNumber(headers map[string][]string) string {
	values, ok := lookupHeader(headers, "CSeq")
	if !ok || len(values) == 0 {
		return ""
	}
	fields := strings.Fields(values[0])
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func extractContactURI(headers map[string][]string) string {
	values, ok := lookupHeader(headers, "Contact")
	if !ok || len(values) == 0 {
		return ""
	}
	value := values[0]
	start := strings.Index(value, "<")
	end := strings.Index(value, ">")
	if start >= 0 && end > start {
		return value[start+1 : end]
	}
	return value
}

func renderFileToken(key, basePath string) (string, bool) {
	params := parseKeyParams(key)
	name := params["name"]
	if name == "" {
		return "", false
	}
	data, err := os.ReadFile(resolvePath(basePath, name))
	if err != nil {
		return "", false
	}
	return string(data), true
}

func renderFieldToken(key, basePath string, callNumber int) (string, bool) {
	lower := strings.ToLower(key)
	if !strings.HasPrefix(lower, "field") {
		return "", false
	}
	fieldName := key
	params := ""
	if idx := strings.Index(key, " "); idx >= 0 {
		fieldName = key[:idx]
		params = key[idx+1:]
	}
	fieldIndex, err := strconv.Atoi(strings.TrimPrefix(strings.ToLower(fieldName), "field"))
	if err != nil {
		return "", false
	}
	parsed := parseKeyParams(params)
	name := parsed["file"]
	if name == "" {
		return "", false
	}
	lineNumber := callNumber
	if rawLine := parsed["line"]; rawLine != "" {
		if value, err := strconv.Atoi(rawLine); err == nil {
			lineNumber = value
		}
	}
	if lineNumber <= 0 {
		lineNumber = 1
	}

	file, err := os.Open(resolvePath(basePath, name))
	if err != nil {
		return "", false
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil || len(records) < lineNumber {
		return "", false
	}
	record := records[lineNumber-1]
	if fieldIndex < 0 || fieldIndex >= len(record) {
		return "", false
	}
	return record[fieldIndex], true
}

func parseKeyParams(value string) map[string]string {
	out := make(map[string]string)
	if value == "" {
		return out
	}
	parts := strings.Fields(value)
	for _, part := range parts {
		key, val, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		out[strings.ToLower(strings.TrimSpace(key))] = strings.Trim(strings.TrimSpace(val), `"`)
	}
	return out
}

func resolvePath(basePath, name string) string {
	if filepath.IsAbs(name) || basePath == "" {
		return name
	}
	return filepath.Join(basePath, name)
}
