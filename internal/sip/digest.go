package sip

import (
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// DigestChallenge holds WWW-Authenticate / Proxy-Authenticate Digest parameters.
type DigestChallenge struct {
	Realm     string
	Nonce     string
	Opaque    string
	Algorithm string
	Qop       string
}

// BuildDigestAuthHeader builds an Authorization or Proxy-Authorization Digest
// header from a challenge (RFC 3261 / RFC 7616). MD5 and SHA-256 are the
// algorithms required by those RFCs — not used for password storage.
func BuildDigestAuthHeader(headerName, challenge, method, uri, body, username, password string) (string, error) {
	params, err := ParseDigestChallenge(challenge)
	if err != nil {
		return "", err
	}
	if params.Realm == "" || params.Nonce == "" {
		return "", errors.New("digest challenge must include realm and nonce")
	}

	algorithm := params.Algorithm
	if algorithm == "" {
		algorithm = "MD5"
	}

	qop := ""
	if params.Qop != "" {
		for _, option := range strings.Split(params.Qop, ",") {
			if strings.EqualFold(strings.TrimSpace(option), "auth") {
				qop = "auth"
				break
			}
		}
		if qop == "" {
			return "", fmt.Errorf("unsupported digest qop %q", params.Qop)
		}
	}

	ha1 := DigestHex(algorithm, fmt.Sprintf("%s:%s:%s", username, params.Realm, password))
	if ha1 == "" {
		return "", fmt.Errorf("unsupported digest algorithm %q", algorithm)
	}
	ha2 := DigestHex(algorithm, fmt.Sprintf("%s:%s", method, uri))
	response := ""
	nc := ""
	cnonce := ""

	if qop == "" {
		response = DigestHex(algorithm, fmt.Sprintf("%s:%s:%s", ha1, params.Nonce, ha2))
	} else {
		nc = "00000001"
		cnonce, err = RandomHex(8)
		if err != nil {
			return "", err
		}
		response = DigestHex(algorithm, fmt.Sprintf("%s:%s:%s:%s:%s:%s", ha1, params.Nonce, nc, cnonce, qop, ha2))
	}

	parts := []string{
		fmt.Sprintf("%s: Digest username=%q", headerName, username),
		fmt.Sprintf("realm=%q", params.Realm),
		fmt.Sprintf("nonce=%q", params.Nonce),
		fmt.Sprintf("uri=%q", uri),
		fmt.Sprintf("response=%q", response),
		fmt.Sprintf("algorithm=%s", algorithm),
	}
	if params.Opaque != "" {
		parts = append(parts, fmt.Sprintf("opaque=%q", params.Opaque))
	}
	if qop != "" {
		parts = append(parts, fmt.Sprintf("qop=%s", qop))
		parts = append(parts, fmt.Sprintf("nc=%s", nc))
		parts = append(parts, fmt.Sprintf("cnonce=%q", cnonce))
	}
	_ = body
	return strings.Join(parts, ", "), nil
}

// ParseDigestChallenge parses a Digest WWW-Authenticate / Proxy-Authenticate value.
func ParseDigestChallenge(value string) (DigestChallenge, error) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(strings.ToLower(value), "digest ") {
		return DigestChallenge{}, errors.New("only Digest authentication is supported")
	}

	params := SplitAuthParams(strings.TrimSpace(value[len("Digest "):]))
	out := DigestChallenge{}
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
			out.Realm = rawValue
		case "nonce":
			out.Nonce = rawValue
		case "opaque":
			out.Opaque = rawValue
		case "algorithm":
			out.Algorithm = rawValue
		case "qop":
			out.Qop = rawValue
		}
	}
	return out, nil
}

// SplitAuthParams splits a Digest parameter list, honoring quoted commas.
func SplitAuthParams(value string) []string {
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

// DigestHex hashes value with the SIP Digest algorithm (MD5 or SHA-256).
// MD5/SHA-256 are required by RFC 3261/7616 — not used for password storage.
// See SECURITY.md (CodeQL go/weak-sensitive-data-hashing).
func DigestHex(algorithm, value string) string {
	switch strings.ToUpper(strings.TrimSpace(algorithm)) {
	case "", "MD5":
		return md5Hex(value)
	case "SHA-256":
		return sha256Hex(value)
	default:
		return ""
	}
}

func md5Hex(value string) string {
	sum := md5.Sum([]byte(value)) // lgtm[go/weak-sensitive-data-hashing] // codeql[go/weak-sensitive-data-hashing]
	return hex.EncodeToString(sum[:])
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value)) // lgtm[go/weak-sensitive-data-hashing] // codeql[go/weak-sensitive-data-hashing]
	return hex.EncodeToString(sum[:])
}

// RandomHex returns size cryptographically random bytes as lowercase hex.
func RandomHex(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// StatelessResponse builds a SIP response that copies Via/From/To/Call-ID/CSeq
// from req (out-of-dialog OPTIONS keep-alive, etc.).
func StatelessResponse(req Message, code int, reason string) []byte {
	via, _ := Header(req.Headers, "Via")
	from, _ := Header(req.Headers, "From")
	to, _ := Header(req.Headers, "To")
	if to != "" && !strings.Contains(strings.ToLower(to), "tag=") {
		to = to + ";tag=gw"
	}
	callID, _ := Header(req.Headers, "Call-ID")
	cseq, _ := Header(req.Headers, "CSeq")
	return []byte(fmt.Sprintf(
		"SIP/2.0 %d %s\r\nVia: %s\r\nFrom: %s\r\nTo: %s\r\nCall-ID: %s\r\nCSeq: %s\r\nContent-Length: 0\r\n\r\n",
		code, reason, via, from, to, callID, cseq,
	))
}
