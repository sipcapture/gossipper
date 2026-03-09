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
	ServerIP      string
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
	Users         int
	UserID        int
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

func RenderMessageStrict(raw string, ctx Context) (string, error) {
	first, err := ctx.RenderStrict(raw)
	if err != nil {
		return "", err
	}
	ctx.BodyLength = computeBodyLength(first)
	return ctx.RenderStrict(raw)
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

func (c Context) RenderStrict(raw string) (string, error) {
	lines := splitLines(raw)
	rendered := make([]string, 0, len(lines))
	for _, line := range lines {
		value, keep, err := c.renderLineStrict(line)
		if err != nil {
			return "", err
		}
		if !keep {
			continue
		}
		rendered = append(rendered, value)
	}
	return strings.Join(rendered, "\r\n"), nil
}

func (c Context) renderLineStrict(line string) (string, bool, error) {
	dropLine := false
	var firstErr error
	value := tokenPattern.ReplaceAllStringFunc(line, func(token string) string {
		replacement, drop, err := c.resolveTokenStrict(token)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			return ""
		}
		if drop {
			dropLine = true
			return ""
		}
		return replacement
	})
	if firstErr != nil {
		return "", false, firstErr
	}
	if dropLine {
		return "", false, nil
	}
	return value, true, nil
}

func (c Context) resolveToken(token string) (string, bool, bool) {
	key := strings.TrimSuffix(strings.TrimPrefix(token, "["), "]")
	if strings.HasPrefix(strings.ToLower(key), "file ") {
		value, ok := renderFileToken(key, c.BasePath)
		return value, ok, false
	}
	if field, ok := renderFieldTokenWithVariables(key, c.BasePath, c.CallNumber, c.Variables); ok {
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

func (c Context) resolveTokenStrict(token string) (string, bool, error) {
	key := strings.TrimSuffix(strings.TrimPrefix(token, "["), "]")
	lower := strings.ToLower(key)
	if strings.HasPrefix(lower, "file ") {
		value, ok := renderFileToken(key, c.BasePath)
		if !ok {
			return "", false, fmt.Errorf("unable to resolve file token %q", token)
		}
		return value, false, nil
	}
	if strings.HasPrefix(lower, "field") {
		value, ok := renderFieldTokenWithVariables(key, c.BasePath, c.CallNumber, c.Variables)
		if !ok {
			return "", false, fmt.Errorf("unable to resolve field token %q", token)
		}
		return value, false, nil
	}
	if strings.HasPrefix(lower, "last_") {
		header := strings.TrimPrefix(key, "last_")
		if strings.EqualFold(header, "message") {
			return c.LastMessage, false, nil
		}
		if strings.EqualFold(header, "Request_URI") {
			return extractLastRequestURI(c.LastMessage, c.LastHeaders), false, nil
		}
		includeName := strings.HasSuffix(header, ":")
		header = strings.TrimSuffix(header, ":")
		values, ok := lookupHeader(c.LastHeaders, header)
		if !ok {
			return "", true, nil
		}
		return renderLastHeader(header, values, includeName), false, nil
	}

	base, delta := parseArithmetic(key)
	switch strings.ToLower(base) {
	case "service":
		return c.Service, false, nil
	case "server_ip":
		if c.ServerIP != "" {
			return c.ServerIP, false, nil
		}
		return c.LocalIP, false, nil
	case "remote_host":
		return c.RemoteHost, false, nil
	case "remote_ip":
		return c.RemoteIP, false, nil
	case "remote_port":
		return strconv.Itoa(c.RemotePort + delta), false, nil
	case "local_ip":
		return c.LocalIP, false, nil
	case "local_ip_type":
		return c.LocalIPType, false, nil
	case "local_port":
		return strconv.Itoa(c.LocalPort + delta), false, nil
	case "transport":
		return strings.ToUpper(transportName(c.Transport)), false, nil
	case "media_ip":
		return c.MediaIP, false, nil
	case "media_ip_type":
		return c.MediaIPType, false, nil
	case "media_port":
		return strconv.Itoa(c.MediaPort + delta), false, nil
	case "call_id":
		return c.CallID, false, nil
	case "cseq":
		return strconv.Itoa(c.CSeq + delta), false, nil
	case "call_number":
		return strconv.Itoa(c.CallNumber), false, nil
	case "msg_index":
		return strconv.Itoa(c.MessageIndex), false, nil
	case "pid":
		return strconv.Itoa(c.PID), false, nil
	case "users":
		return strconv.Itoa(c.Users), false, nil
	case "userid":
		return strconv.Itoa(c.UserID), false, nil
	case "branch":
		if delta == 0 {
			return c.BranchBase, false, nil
		}
		return fmt.Sprintf("%s-%d", c.BranchBase, delta), false, nil
	case "len":
		return strconv.Itoa(c.BodyLength + delta), false, nil
	case "last_message":
		return c.LastMessage, false, nil
	case "timestamp":
		return time.Now().Format("2006-01-02 15:04:05"), false, nil
	case "date":
		return time.Now().UTC().Format(time.RFC1123), false, nil
	case "last_cseq_number":
		return extractCSeqNumber(c.LastHeaders), false, nil
	case "next_url":
		return extractContactURI(c.LastHeaders), false, nil
	case "peer_tag_param":
		tag := extractPeerTag(c.LastHeaders)
		if tag == "" {
			return "", false, nil
		}
		return ";tag=" + tag, false, nil
	default:
		if strings.HasPrefix(base, "$") {
			if c.Variables == nil {
				return "", false, nil
			}
			return c.Variables[strings.TrimPrefix(base, "$")], false, nil
		}
		if c.ExtraKeywords != nil {
			if v, ok := c.ExtraKeywords[key]; ok {
				return v, false, nil
			}
		}
		return "", false, fmt.Errorf("unsupported scenario keyword %q", token)
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

func extractLastRequestURI(lastMessage string, headers map[string][]string) string {
	if strings.TrimSpace(lastMessage) != "" {
		lines := splitLines(lastMessage)
		if len(lines) > 0 {
			parts := strings.Fields(strings.TrimSpace(lines[0]))
			if len(parts) >= 2 && !strings.HasPrefix(strings.ToUpper(parts[0]), "SIP/2.0") {
				return parts[1]
			}
		}
	}
	values, ok := lookupHeader(headers, "To")
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
	return renderFieldTokenWithVariables(key, basePath, callNumber, nil)
}

func renderFieldTokenWithVariables(key, basePath string, callNumber int, variables map[string]string) (string, bool) {
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
		resolved, ok := resolveLineNumber(rawLine, variables)
		if !ok {
			return "", false
		}
		lineNumber = resolved
	}

	record, ok, err := csvRecordAt(basePath, name, lineNumber)
	if err != nil || !ok {
		return "", false
	}
	if fieldIndex < 0 || fieldIndex >= len(record) {
		return "", false
	}
	return record[fieldIndex], true
}

func resolveLineNumber(raw string, variables map[string]string) (int, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0, false
	}
	if strings.HasPrefix(value, "$") {
		if variables == nil {
			return 0, false
		}
		resolved, ok := variables[strings.TrimPrefix(value, "$")]
		if !ok {
			return 0, false
		}
		value = strings.TrimSpace(resolved)
	}
	lineNumber, err := strconv.Atoi(value)
	if err != nil || lineNumber <= 0 {
		return 0, false
	}
	return lineNumber, true
}

func csvRecordAt(basePath, name string, lineNumber int) ([]string, bool, error) {
	if lineNumber <= 0 {
		return nil, false, nil
	}
	file, err := os.Open(resolvePath(basePath, name))
	if err != nil {
		return nil, false, err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil || len(records) < lineNumber {
		return nil, false, err
	}
	return records[lineNumber-1], true, nil
}

func LookupCSVLine(basePath, name, key string) (int, bool, error) {
	file, err := os.Open(resolvePath(basePath, name))
	if err != nil {
		return 0, false, err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		return 0, false, err
	}
	for idx, record := range records {
		if len(record) == 0 {
			continue
		}
		if record[0] == key {
			return idx + 1, true, nil
		}
	}
	return 0, false, nil
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
