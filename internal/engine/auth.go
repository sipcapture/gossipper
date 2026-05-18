package engine

import (
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/sipcapture/gossipper/internal/eventlog"
	"github.com/sipcapture/gossipper/internal/sip"
	templ "github.com/sipcapture/gossipper/internal/template"
)

const authPlaceholder = "Authorization: pending"

type authKeywordOptions struct {
	tokenKey string
	username string
	password string
}

func (e *Engine) renderSIPMessage(raw string, ctx templ.Context) (string, error) {
	// Normalize indentation once per unique template text, then cache.
	if normalized, ok := e.normalizedCache.Load(raw); ok {
		raw = normalized.(string)
	} else {
		n := normalizeSIPScenarioLineIndent(raw)
		e.normalizedCache.Store(raw, n)
		raw = n
	}

	ctx.ClockTick = e.clockTick()
	ctx.DynamicID = e.nextDynamicID()

	options := extractAuthKeywordOptions(raw)
	if len(options) == 0 {
		rendered, err := templ.RenderMessageStrict(raw, ctx)
		if err != nil {
			return "", err
		}
		return ensureMessageTerminator(rendered), nil
	}

	provisionalCtx := ctx
	provisionalCtx.ExtraKeywords = cloneKeywords(ctx.ExtraKeywords)
	for _, option := range options {
		provisionalCtx.ExtraKeywords[option.tokenKey] = authPlaceholder
	}

	provisional, err := templ.RenderMessageStrict(raw, provisionalCtx)
	if err != nil {
		return "", err
	}
	provisional = ensureMessageTerminator(provisional)

	finalCtx := ctx
	finalCtx.ExtraKeywords = cloneKeywords(ctx.ExtraKeywords)
	for _, option := range options {
		authHeader, err := e.buildAuthHeader(provisional, ctx, option)
		if err != nil {
			return "", err
		}
		finalCtx.ExtraKeywords[option.tokenKey] = authHeader
	}
	rendered, err := templ.RenderMessageStrict(raw, finalCtx)
	if err != nil {
		return "", err
	}
	return ensureMessageTerminator(rendered), nil
}

func (e *Engine) buildAuthHeader(outgoing string, ctx templ.Context, option authKeywordOptions) (string, error) {
	header, err := e.buildAuthHeaderInner(outgoing, ctx, option)
	e.emitAuthEvent(ctx, option, header, err)
	return header, err
}

func (e *Engine) buildAuthHeaderInner(outgoing string, ctx templ.Context, option authKeywordOptions) (string, error) {
	if ctx.LastMessage == "" {
		return "", errors.New("authentication keyword requires a previous 401 or 407 challenge")
	}

	challengeMsg, err := sip.Parse([]byte(ctx.LastMessage))
	if err != nil {
		return "", fmt.Errorf("parse authentication challenge: %w", err)
	}

	headerName := "Authorization"
	challengeName := "WWW-Authenticate"
	switch challengeMsg.StatusCode {
	case 401:
	case 407:
		headerName = "Proxy-Authorization"
		challengeName = "Proxy-Authenticate"
	default:
		return "", fmt.Errorf("authentication keyword requires a 401 or 407 challenge, got %d", challengeMsg.StatusCode)
	}

	challenge, ok := sip.Header(challengeMsg.Headers, challengeName)
	if !ok {
		return "", fmt.Errorf("missing %s header in authentication challenge", challengeName)
	}

	outgoingMsg, err := sip.Parse([]byte(outgoing))
	if err != nil {
		return "", fmt.Errorf("parse outgoing authentication request: %w", err)
	}
	if outgoingMsg.Method == "" || outgoingMsg.RequestURI == "" {
		return "", errors.New("authentication keyword can only be used in SIP requests")
	}

	return buildDigestAuthHeader(
		headerName,
		challenge,
		outgoingMsg.Method,
		outgoingMsg.RequestURI,
		outgoingMsg.Body,
		resolveAuthKeywordUsername(option.username, e.cfg.AuthUsername),
		resolveAuthKeywordPassword(option.password, e.cfg.AuthPassword),
	)
}

// emitAuthEvent records a structured event whenever an [authentication]
// keyword is processed. Both successful and failed challenges are surfaced
// so observability stays symmetric with traceError().
func (e *Engine) emitAuthEvent(ctx templ.Context, option authKeywordOptions, header string, buildErr error) {
	if e == nil || e.log == nil {
		return
	}
	attrs := map[string]any{
		"call_id":  ctx.CallID,
		"call_num": ctx.CallNumber,
		"username": resolveAuthKeywordUsername(option.username, e.cfg.AuthUsername),
	}
	level := eventlog.LevelInfo
	msg := "authorization header generated"
	if challengeMsg, err := sip.Parse([]byte(ctx.LastMessage)); err == nil {
		if challengeMsg.StatusCode > 0 {
			attrs["challenge.status"] = challengeMsg.StatusCode
		}
		challengeName := "WWW-Authenticate"
		if challengeMsg.StatusCode == 407 {
			challengeName = "Proxy-Authenticate"
		}
		if raw, ok := sip.Header(challengeMsg.Headers, challengeName); ok {
			if params, perr := parseDigestChallenge(raw); perr == nil {
				if params.realm != "" {
					attrs["realm"] = params.realm
				}
				if params.algorithm != "" {
					attrs["algorithm"] = params.algorithm
				}
				if params.qop != "" {
					attrs["qop"] = params.qop
				}
			}
		}
	}
	if buildErr != nil {
		level = eventlog.LevelError
		msg = "authorization header build failed"
		attrs["error"] = buildErr.Error()
		attrs["result"] = "error"
	} else {
		attrs["result"] = "ok"
	}
	e.log.Emit(eventlog.Event{
		Level: level,
		Kind:  eventlog.KindAuth,
		Msg:   msg,
		Attrs: attrs,
	})
	_ = header
}

func buildDigestAuthHeader(headerName, challenge, method, uri, body, username, password string) (string, error) {
	params, err := parseDigestChallenge(challenge)
	if err != nil {
		return "", err
	}
	if params.realm == "" || params.nonce == "" {
		return "", errors.New("digest challenge must include realm and nonce")
	}

	algorithm := params.algorithm
	if algorithm == "" {
		algorithm = "MD5"
	}

	qop := ""
	if params.qop != "" {
		for _, option := range strings.Split(params.qop, ",") {
			if strings.EqualFold(strings.TrimSpace(option), "auth") {
				qop = "auth"
				break
			}
		}
		if qop == "" {
			return "", fmt.Errorf("unsupported digest qop %q", params.qop)
		}
	}

	ha1 := digestHex(algorithm, fmt.Sprintf("%s:%s:%s", username, params.realm, password))
	if ha1 == "" {
		return "", fmt.Errorf("unsupported digest algorithm %q", algorithm)
	}
	ha2Input := fmt.Sprintf("%s:%s", method, uri)
	ha2 := digestHex(algorithm, ha2Input)
	response := ""
	nc := ""
	cnonce := ""

	if qop == "" {
		response = digestHex(algorithm, fmt.Sprintf("%s:%s:%s", ha1, params.nonce, ha2))
	} else {
		nc = "00000001"
		cnonce, err = randomHex(8)
		if err != nil {
			return "", err
		}
		response = digestHex(algorithm, fmt.Sprintf("%s:%s:%s:%s:%s:%s", ha1, params.nonce, nc, cnonce, qop, ha2))
	}

	parts := []string{
		fmt.Sprintf("%s: Digest username=%q", headerName, username),
		fmt.Sprintf("realm=%q", params.realm),
		fmt.Sprintf("nonce=%q", params.nonce),
		fmt.Sprintf("uri=%q", uri),
		fmt.Sprintf("response=%q", response),
		fmt.Sprintf("algorithm=%s", algorithm),
	}
	if params.opaque != "" {
		parts = append(parts, fmt.Sprintf("opaque=%q", params.opaque))
	}
	if qop != "" {
		parts = append(parts, fmt.Sprintf("qop=%s", qop))
		parts = append(parts, fmt.Sprintf("nc=%s", nc))
		parts = append(parts, fmt.Sprintf("cnonce=%q", cnonce))
	}
	_ = body
	return strings.Join(parts, ", "), nil
}

type digestChallenge struct {
	realm     string
	nonce     string
	opaque    string
	algorithm string
	qop       string
}

type digestAuthorization struct {
	username  string
	realm     string
	nonce     string
	uri       string
	response  string
	algorithm string
	qop       string
	nc        string
	cnonce    string
	opaque    string
}

func parseDigestChallenge(value string) (digestChallenge, error) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(strings.ToLower(value), "digest ") {
		return digestChallenge{}, errors.New("only Digest authentication is supported")
	}

	params := splitAuthParams(strings.TrimSpace(value[len("Digest "):]))
	out := digestChallenge{}
	for _, param := range params {
		key, rawValue, ok := strings.Cut(param, "=")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		rawValue = strings.TrimSpace(rawValue)
		rawValue = strings.Trim(rawValue, `"`)
		switch key {
		case "realm":
			out.realm = rawValue
		case "nonce":
			out.nonce = rawValue
		case "opaque":
			out.opaque = rawValue
		case "algorithm":
			out.algorithm = rawValue
		case "qop":
			out.qop = rawValue
		}
	}
	return out, nil
}

func splitAuthParams(value string) []string {
	var (
		params  []string
		current strings.Builder
		quoted  bool
	)
	for _, r := range value {
		switch r {
		case '"':
			quoted = !quoted
			current.WriteRune(r)
		case ',':
			if quoted {
				current.WriteRune(r)
				continue
			}
			params = append(params, strings.TrimSpace(current.String()))
			current.Reset()
		default:
			current.WriteRune(r)
		}
	}
	if current.Len() > 0 {
		params = append(params, strings.TrimSpace(current.String()))
	}
	return params
}

func md5Hex(value string) string {
	sum := md5.Sum([]byte(value))
	return hex.EncodeToString(sum[:])
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func digestHex(algorithm, value string) string {
	switch strings.ToUpper(strings.TrimSpace(algorithm)) {
	case "", "MD5":
		return md5Hex(value)
	case "SHA-256":
		return sha256Hex(value)
	default:
		return ""
	}
}

func randomHex(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func cloneKeywords(in map[string]string) map[string]string {
	out := make(map[string]string, len(in)+1)
	for key, value := range in {
		out[key] = value
	}
	return out
}

// extractAuthKeywordOptions finds [authentication...] tokens without regexp.
func extractAuthKeywordOptions(raw string) []authKeywordOptions {
	const prefix = "[authentication"
	// Fast path: skip ToLower allocation when the template has no auth keyword.
	if !containsFoldCI(raw, "[authentication") {
		return nil
	}
	var options []authKeywordOptions
	s := strings.ToLower(raw)
	for {
		i := strings.Index(s, prefix)
		if i < 0 {
			break
		}
		start := i
		end := strings.IndexByte(raw[start+1:], ']')
		if end < 0 {
			break
		}
		end += start + 1
		tokenKey := strings.TrimSpace(raw[start+1 : end]) // key without brackets, for ExtraKeywords lookup
		params := parseAuthKeywordParams(tokenKey)
		options = append(options, authKeywordOptions{
			tokenKey: tokenKey,
			username: params["username"],
			password: params["password"],
		})
		s = s[end+1:]
		raw = raw[end+1:]
	}
	return options
}

func parseAuthKeywordParams(tokenKey string) map[string]string {
	params := make(map[string]string)
	fields := strings.Fields(strings.TrimSpace(strings.TrimPrefix(tokenKey, "authentication")))
	for _, field := range fields {
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			continue
		}
		params[strings.ToLower(strings.TrimSpace(key))] = strings.Trim(strings.TrimSpace(value), `"`)
	}
	return params
}

func resolveAuthKeywordUsername(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func resolveAuthKeywordPassword(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func verifyAuthHeader(raw, username, password string) (bool, error) {
	msg, err := sip.Parse([]byte(raw))
	if err != nil {
		return false, err
	}
	headerValue, ok := sip.Header(msg.Headers, "Authorization")
	if !ok {
		headerValue, ok = sip.Header(msg.Headers, "Proxy-Authorization")
	}
	if !ok {
		return false, nil
	}
	auth, err := parseDigestAuthorization(headerValue)
	if err != nil {
		return false, err
	}
	if auth.username != username {
		return false, nil
	}
	algorithm := auth.algorithm
	if algorithm == "" {
		algorithm = "MD5"
	}
	ha1 := digestHex(algorithm, fmt.Sprintf("%s:%s:%s", username, auth.realm, password))
	if ha1 == "" {
		return false, fmt.Errorf("unsupported digest algorithm %q", algorithm)
	}
	ha2 := digestHex(algorithm, fmt.Sprintf("%s:%s", msg.Method, auth.uri))
	expected := ""
	if auth.qop == "" {
		expected = digestHex(algorithm, fmt.Sprintf("%s:%s:%s", ha1, auth.nonce, ha2))
	} else {
		if !strings.EqualFold(auth.qop, "auth") {
			return false, fmt.Errorf("unsupported digest qop %q", auth.qop)
		}
		expected = digestHex(algorithm, fmt.Sprintf("%s:%s:%s:%s:%s:%s", ha1, auth.nonce, auth.nc, auth.cnonce, auth.qop, ha2))
	}
	return strings.EqualFold(expected, auth.response), nil
}

func parseDigestAuthorization(value string) (digestAuthorization, error) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(strings.ToLower(value), "digest ") {
		return digestAuthorization{}, errors.New("only Digest authorization is supported")
	}
	params := splitAuthParams(strings.TrimSpace(value[len("Digest "):]))
	out := digestAuthorization{}
	for _, param := range params {
		key, rawValue, ok := strings.Cut(param, "=")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		rawValue = strings.Trim(strings.TrimSpace(rawValue), `"`)
		switch key {
		case "username":
			out.username = rawValue
		case "realm":
			out.realm = rawValue
		case "nonce":
			out.nonce = rawValue
		case "uri":
			out.uri = rawValue
		case "response":
			out.response = rawValue
		case "algorithm":
			out.algorithm = rawValue
		case "qop":
			out.qop = rawValue
		case "nc":
			out.nc = rawValue
		case "cnonce":
			out.cnonce = rawValue
		case "opaque":
			out.opaque = rawValue
		}
	}
	if out.realm == "" || out.nonce == "" || out.uri == "" || out.response == "" || out.username == "" {
		return digestAuthorization{}, errors.New("invalid Digest authorization header")
	}
	return out, nil
}

// containsFoldCI reports whether s contains substr (case-insensitive) without allocation.
func containsFoldCI(s, substr string) bool {
if len(substr) > len(s) {
return false
}
for i := 0; i <= len(s)-len(substr); i++ {
if strings.EqualFold(s[i:i+len(substr)], substr) {
return true
}
}
return false
}
