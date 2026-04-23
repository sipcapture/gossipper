package template

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	linesPool = sync.Pool{
		New: func() interface{} { return new([]string) },
	}
	builderPool = sync.Pool{
		New: func() interface{} { return new(strings.Builder) },
	}
)

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
	SIPpVersion   string
	ClockTick     int64
	DynamicID     int64
	ExtraKeywords map[string]string
	Variables     map[string]string
	// CSVFieldOverrides stores per-file, per-line, per-field in-memory overrides.
	CSVFieldOverrides map[string]map[int]map[int]string
	BasePath          string
}

func (c Context) Render(raw string) string {
	ptr := linesPool.Get().(*[]string)
	splitLinesTo(ptr, raw)
	out := builderPool.Get().(*strings.Builder)
	out.Reset()
	out.Grow(len(raw) + 64)
	first := true
	for _, line := range *ptr {
		value, keep := c.renderLine(line)
		if !keep {
			continue
		}
		if !first {
			out.WriteString("\r\n")
		}
		out.WriteString(value)
		first = false
	}
	result := out.String()
	builderPool.Put(out)
	linesPool.Put(ptr)
	return result
}

func RenderMessage(raw string, ctx Context) string {
	// Fast path: no [len] token → single render (most SIP messages without body).
	if !hasLenToken(raw) {
		return ctx.Render(raw)
	}
	first := ctx.Render(raw)
	ctx.BodyLength = computeBodyLength(first)
	return ctx.Render(raw)
}

func RenderMessageStrict(raw string, ctx Context) (string, error) {
	// Fast path: no [len] token → single render.
	if !hasLenToken(raw) {
		return ctx.RenderStrict(raw)
	}
	first, err := ctx.RenderStrict(raw)
	if err != nil {
		return "", err
	}
	ctx.BodyLength = computeBodyLength(first)
	return ctx.RenderStrict(raw)
}

// hasLenToken reports whether raw contains a [len...] keyword (case-insensitive).
// Used to skip the double-render when there is no body-length substitution.
func hasLenToken(raw string) bool {
	for i := 0; i+4 <= len(raw); i++ {
		if raw[i] == '[' &&
			(raw[i+1] == 'l' || raw[i+1] == 'L') &&
			(raw[i+2] == 'e' || raw[i+2] == 'E') &&
			(raw[i+3] == 'n' || raw[i+3] == 'N') {
			return true
		}
	}
	return false
}

// expandTokens replaces [token] substrings via manual scan (no regexp).
func expandTokens(line string, resolve func(token string) (replacement string, ok bool, drop bool)) (string, bool) {
	b := builderPool.Get().(*strings.Builder)
	b.Reset()
	b.Grow(len(line) + 64)
	dropLine := false
	i := 0
	for i < len(line) {
		if line[i] == '[' {
			end := strings.IndexByte(line[i+1:], ']')
			if end == -1 {
				b.WriteByte(line[i])
				i++
				continue
			}
			end += i + 2
			token := line[i:end]
			rep, ok, drop := resolve(token)
			if drop {
				dropLine = true
			}
			if ok {
				b.WriteString(rep)
			}
			i = end
			continue
		}
		b.WriteByte(line[i])
		i++
	}
	result := b.String()
	builderPool.Put(b)
	return result, dropLine
}

func (c Context) renderLine(line string) (string, bool) {
	value, dropLine := expandTokens(line, c.resolveToken)
	if dropLine {
		return "", false
	}
	return value, true
}

func (c Context) RenderStrict(raw string) (string, error) {
	ptr := linesPool.Get().(*[]string)
	splitLinesTo(ptr, raw)
	out := builderPool.Get().(*strings.Builder)
	out.Reset()
	out.Grow(len(raw) + 64)
	first := true
	for _, line := range *ptr {
		value, keep, err := c.renderLineStrict(line)
		if err != nil {
			builderPool.Put(out)
			return "", err
		}
		if !keep {
			continue
		}
		if !first {
			out.WriteString("\r\n")
		}
		out.WriteString(value)
		first = false
	}
	result := out.String()
	builderPool.Put(out)
	linesPool.Put(ptr)
	return result, nil
}

func expandTokensStrict(line string, resolve func(token string) (replacement string, drop bool, err error)) (string, bool, error) {
	b := builderPool.Get().(*strings.Builder)
	b.Reset()
	b.Grow(len(line) + 64)
	dropLine := false
	var firstErr error
	i := 0
	for i < len(line) {
		if line[i] == '[' {
			end := strings.IndexByte(line[i+1:], ']')
			if end == -1 {
				b.WriteByte(line[i])
				i++
				continue
			}
			end += i + 2
			token := line[i:end]
			rep, drop, err := resolve(token)
			if err != nil && firstErr == nil {
				firstErr = err
			}
			if drop {
				dropLine = true
			}
			b.WriteString(rep)
			i = end
			continue
		}
		b.WriteByte(line[i])
		i++
	}
	result := b.String()
	builderPool.Put(b)
	return result, dropLine, firstErr
}

func (c Context) renderLineStrict(line string) (string, bool, error) {
	value, dropLine, firstErr := expandTokensStrict(line, c.resolveTokenStrict)
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
	if field, ok := renderFieldTokenWithVariables(key, c.BasePath, c.CallNumber, c.Variables, c.CSVFieldOverrides); ok {
		return field, true, false
	}
	if value, ok := renderFillToken(key, c.Variables); ok {
		return value, true, false
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
	case "sipp_version":
		if strings.TrimSpace(c.SIPpVersion) == "" {
			return "Gossipper", true, false
		}
		return c.SIPpVersion, true, false
	case "clock_tick":
		return strconv.FormatInt(c.ClockTick+int64(delta), 10), true, false
	case "dynamic_id":
		return strconv.FormatInt(c.DynamicID+int64(delta), 10), true, false
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
	case "dtmf_digits":
		if c.Variables != nil {
			return c.Variables["dtmf_digits"], true, false
		}
		return "", true, false
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
		value, ok := renderFieldTokenWithVariables(key, c.BasePath, c.CallNumber, c.Variables, c.CSVFieldOverrides)
		if !ok {
			return "", false, fmt.Errorf("unable to resolve field token %q", token)
		}
		return value, false, nil
	}
	if strings.HasPrefix(lower, "fill") {
		value, ok := renderFillToken(key, c.Variables)
		if !ok {
			return "", false, fmt.Errorf("unable to resolve fill token %q", token)
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
	case "sipp_version":
		if strings.TrimSpace(c.SIPpVersion) == "" {
			return "Gossipper", false, nil
		}
		return c.SIPpVersion, false, nil
	case "clock_tick":
		return strconv.FormatInt(c.ClockTick+int64(delta), 10), false, nil
	case "dynamic_id":
		return strconv.FormatInt(c.DynamicID+int64(delta), 10), false, nil
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
	case "dtmf_digits":
		if c.Variables != nil {
			return c.Variables["dtmf_digits"], false, nil
		}
		return "", false, nil
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

// SplitLinesTo fills dst with lines from value, reusing the slice to avoid allocs.
// Handles \r\n without allocating (avoids strings.ReplaceAll). Exported for engine.
func SplitLinesTo(dst *[]string, value string) {
	splitLinesTo(dst, value)
}

func splitLinesTo(dst *[]string, value string) {
	*dst = (*dst)[:0]
	start := 0
	for i := 0; i < len(value); i++ {
		if value[i] == '\n' {
			*dst = append(*dst, value[start:i])
			start = i + 1
			continue
		}
		if value[i] == '\r' && i+1 < len(value) && value[i+1] == '\n' {
			*dst = append(*dst, value[start:i])
			start = i + 2
			i++ // skip \n in next iteration
			continue
		}
	}
	if start < len(value) {
		*dst = append(*dst, value[start:])
	}
}

func splitLines(value string) []string {
	ptr := linesPool.Get().(*[]string)
	splitLinesTo(ptr, value)
	result := make([]string, len(*ptr))
	copy(result, *ptr)
	linesPool.Put(ptr)
	return result
}

// firstLineFromMessage returns the first line of msg (handles \n and \r\n), no alloc.
func firstLineFromMessage(msg string) string {
	for i := 0; i < len(msg); i++ {
		if msg[i] == '\n' {
			return msg[:i]
		}
		if msg[i] == '\r' && i+1 < len(msg) && msg[i+1] == '\n' {
			return msg[:i]
		}
	}
	return msg
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
	case "u1", "un", "ui":
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
	s := values[len(values)-1]
	for {
		part, rest, ok := strings.Cut(s, ";")
		part = strings.TrimSpace(part)
		if len(part) >= 4 && strings.HasPrefix(strings.ToLower(part), "tag=") {
			return strings.TrimSpace(part[4:])
		}
		if !ok {
			break
		}
		s = rest
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
		first := firstLineFromMessage(lastMessage)
		first = strings.TrimSpace(first)
		if first != "" {
			parts := strings.Fields(first)
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
	return renderFieldTokenWithVariables(key, basePath, callNumber, nil, nil)
}

func renderFieldTokenWithVariables(key, basePath string, callNumber int, variables map[string]string, overrides map[string]map[int]map[int]string) (string, bool) {
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

	record, ok, err := csvRecordAt(basePath, name, lineNumber, overrides)
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

func csvRecordAt(basePath, name string, lineNumber int, overrides map[string]map[int]map[int]string) ([]string, bool, error) {
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
	record := append([]string(nil), records[lineNumber-1]...)
	applyCSVFieldOverrides(resolvePath(basePath, name), lineNumber, record, overrides)
	return record, true, nil
}

// ApplyCSVMutation mutates one CSV cell in memory for subsequent [fieldN ...] reads.
// Supported modes: "replace", "insert" (append by default, or prefix with position=prefix).
func ApplyCSVMutation(
	basePath, name string,
	lineNumber, field int,
	mode, text, position string,
	overrides map[string]map[int]map[int]string,
) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("csv mutation requires file")
	}
	if lineNumber <= 0 {
		return fmt.Errorf("csv mutation line must be > 0")
	}
	if field < 0 {
		return fmt.Errorf("csv mutation field must be >= 0")
	}
	record, ok, err := csvRecordAt(basePath, name, lineNumber, overrides)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("csv mutation line %d not found", lineNumber)
	}
	if field >= len(record) {
		return fmt.Errorf("csv mutation field index %d out of range", field)
	}
	current := record[field]
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "replace":
		record[field] = text
	case "insert":
		switch strings.ToLower(strings.TrimSpace(position)) {
		case "", "suffix", "append":
			record[field] = current + text
		case "prefix", "prepend":
			record[field] = text + current
		default:
			return fmt.Errorf("csv insert position must be prefix|suffix")
		}
	default:
		return fmt.Errorf("unsupported csv mutation mode %q", mode)
	}
	storeCSVFieldOverride(resolvePath(basePath, name), lineNumber, field, record[field], overrides)
	return nil
}

func applyCSVFieldOverrides(fileKey string, lineNumber int, record []string, overrides map[string]map[int]map[int]string) {
	if overrides == nil {
		return
	}
	byFile, ok := overrides[fileKey]
	if !ok {
		return
	}
	byLine, ok := byFile[lineNumber]
	if !ok {
		return
	}
	for fieldIndex, value := range byLine {
		if fieldIndex < 0 || fieldIndex >= len(record) {
			continue
		}
		record[fieldIndex] = value
	}
}

func storeCSVFieldOverride(fileKey string, lineNumber, field int, value string, overrides map[string]map[int]map[int]string) {
	if overrides == nil {
		return
	}
	byFile, ok := overrides[fileKey]
	if !ok {
		byFile = make(map[int]map[int]string)
		overrides[fileKey] = byFile
	}
	byLine, ok := byFile[lineNumber]
	if !ok {
		byLine = make(map[int]string)
		byFile[lineNumber] = byLine
	}
	byLine[field] = value
}

func LookupCSVLine(basePath, name, key string) (int, bool, error) {
	resolvedPath := resolvePath(basePath, name)
	if line, found, ok := lookupCSVLineFromIndex(resolvedPath, 0, key); ok {
		return line, found, nil
	}

	file, err := os.Open(resolvedPath)
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

func GenerateCSVIndex(basePath, name string, field int) (string, int, error) {
	if field < 0 {
		return "", 0, fmt.Errorf("infindex field must be greater than or equal to zero")
	}
	resolvedPath := resolvePath(basePath, name)
	file, err := os.Open(resolvedPath)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		return "", 0, err
	}

	index := csvIndex{
		Field: field,
		Lines: make(map[string]int),
	}
	for idx, record := range records {
		if field >= len(record) {
			continue
		}
		key := strings.TrimSpace(record[field])
		if key == "" {
			continue
		}
		if _, exists := index.Lines[key]; exists {
			continue
		}
		index.Lines[key] = idx + 1
	}

	data, err := json.Marshal(index)
	if err != nil {
		return "", 0, err
	}
	indexPath := buildCSVIndexPath(resolvedPath, field)
	if err := os.WriteFile(indexPath, data, 0o644); err != nil {
		return "", 0, err
	}
	return indexPath, len(index.Lines), nil
}

type csvIndex struct {
	Field int            `json:"field"`
	Lines map[string]int `json:"lines"`
}

func buildCSVIndexPath(csvPath string, field int) string {
	return fmt.Sprintf("%s.gossipper.idx.%d.json", csvPath, field)
}

func lookupCSVLineFromIndex(csvPath string, field int, key string) (int, bool, bool) {
	data, err := os.ReadFile(buildCSVIndexPath(csvPath, field))
	if err != nil {
		return 0, false, false
	}
	var index csvIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return 0, false, false
	}
	line, found := index.Lines[key]
	return line, found, true
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

func renderFillToken(key string, variables map[string]string) (string, bool) {
	lower := strings.ToLower(key)
	if !strings.HasPrefix(lower, "fill") {
		return "", false
	}
	paramsRaw := ""
	if idx := strings.IndexAny(key, " \t"); idx >= 0 {
		paramsRaw = key[idx+1:]
	}
	params := parseKeyParams(paramsRaw)
	varName := strings.TrimSpace(params["variable"])
	if varName == "" || variables == nil {
		return "", false
	}
	varName = strings.TrimPrefix(varName, "$")
	rawLen, ok := variables[varName]
	if !ok {
		return "", false
	}
	length, ok := parseFillLength(rawLen)
	if !ok || length < 0 {
		return "", false
	}
	seed := params["text"]
	if seed == "" {
		seed = "X"
	}
	return fillByPattern(seed, length), true
}

func parseFillLength(raw string) (int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	if v, err := strconv.Atoi(raw); err == nil {
		return v, true
	}
	f, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, false
	}
	return int(f), true
}

func fillByPattern(seed string, length int) string {
	if length <= 0 {
		return ""
	}
	if len(seed) == 1 {
		return strings.Repeat(seed, length)
	}
	var builder strings.Builder
	builder.Grow(length)
	for builder.Len() < length {
		builder.WriteString(seed)
	}
	value := builder.String()
	if len(value) > length {
		return value[:length]
	}
	return value
}

func resolvePath(basePath, name string) string {
	if filepath.IsAbs(name) || basePath == "" {
		return name
	}
	return filepath.Join(basePath, name)
}
