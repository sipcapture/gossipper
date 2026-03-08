package engine

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/adubovikov/gossipper/internal/sip"
	templ "github.com/adubovikov/gossipper/internal/template"
)

const authPlaceholder = "Authorization: pending"

func (e *Engine) renderSIPMessage(raw string, ctx templ.Context) (string, error) {
	if !strings.Contains(raw, "[authentication]") {
		return ensureMessageTerminator(templ.RenderMessage(raw, ctx)), nil
	}

	provisionalCtx := ctx
	provisionalCtx.ExtraKeywords = cloneKeywords(ctx.ExtraKeywords)
	provisionalCtx.ExtraKeywords["authentication"] = authPlaceholder

	provisional := ensureMessageTerminator(templ.RenderMessage(raw, provisionalCtx))
	authHeader, err := e.buildAuthHeader(provisional, ctx)
	if err != nil {
		return "", err
	}

	finalCtx := ctx
	finalCtx.ExtraKeywords = cloneKeywords(ctx.ExtraKeywords)
	finalCtx.ExtraKeywords["authentication"] = authHeader
	return ensureMessageTerminator(templ.RenderMessage(raw, finalCtx)), nil
}

func (e *Engine) buildAuthHeader(outgoing string, ctx templ.Context) (string, error) {
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
		e.cfg.AuthUsername,
		e.cfg.AuthPassword,
	)
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
	if !strings.EqualFold(algorithm, "MD5") {
		return "", fmt.Errorf("unsupported digest algorithm %q", algorithm)
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

	ha1 := md5Hex(fmt.Sprintf("%s:%s:%s", username, params.realm, password))
	ha2 := md5Hex(fmt.Sprintf("%s:%s", method, uri))
	response := ""
	nc := ""
	cnonce := ""

	if qop == "" {
		response = md5Hex(fmt.Sprintf("%s:%s:%s", ha1, params.nonce, ha2))
	} else {
		nc = "00000001"
		cnonce, err = randomHex(8)
		if err != nil {
			return "", err
		}
		response = md5Hex(fmt.Sprintf("%s:%s:%s:%s:%s:%s", ha1, params.nonce, nc, cnonce, qop, ha2))
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
