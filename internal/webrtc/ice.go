package webrtc

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
	"time"

	pion "github.com/pion/webrtc/v4"
)

const defaultICEAuthTTL = 24 * time.Hour

type parsedICEServer struct {
	URL      string
	Username string
	Password string
}

// TURNRESTCredential mints time-limited credentials for coturn --use-auth-secret.
// See draft-uberti-rtcweb-turn-rest / coturn REST API.
func TURNRESTCredential(secret, identity string, ttl time.Duration) (username, password string) {
	if ttl <= 0 {
		ttl = defaultICEAuthTTL
	}
	if strings.TrimSpace(identity) == "" {
		identity = "gossipper"
	}
	expiry := time.Now().Add(ttl).Unix()
	username = fmt.Sprintf("%d:%s", expiry, identity)
	mac := hmac.New(sha1.New, []byte(secret))
	_, _ = mac.Write([]byte(username))
	password = base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return username, password
}

func parseICEServerLine(raw string) (parsedICEServer, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return parsedICEServer{}, fmt.Errorf("webrtc: empty ICE server URL")
	}
	lower := strings.ToLower(raw)
	if !strings.Contains(raw, "@") || (!strings.HasPrefix(lower, "turn:") && !strings.HasPrefix(lower, "turns:")) {
		return parsedICEServer{URL: raw}, nil
	}
	scheme := "turn"
	rest := raw
	if strings.HasPrefix(lower, "turns:") {
		scheme = "turns"
		rest = raw[len("turns:"):]
	} else {
		rest = raw[len("turn:"):]
	}
	u, err := url.Parse(scheme + "://" + rest)
	if err != nil {
		return parsedICEServer{}, fmt.Errorf("webrtc: parse ICE URL %q: %w", raw, err)
	}
	user := ""
	pass := ""
	if u.User != nil {
		user = u.User.Username()
		pass, _ = u.User.Password()
	}
	host := u.Host
	if host == "" {
		return parsedICEServer{}, fmt.Errorf("webrtc: ICE URL missing host: %q", raw)
	}
	outURL := scheme + ":" + host
	if u.RawQuery != "" {
		outURL += "?" + u.RawQuery
	}
	return parsedICEServer{URL: outURL, Username: user, Password: pass}, nil
}

func isTURNURL(u string) bool {
	lower := strings.ToLower(strings.TrimSpace(u))
	return strings.HasPrefix(lower, "turn:") || strings.HasPrefix(lower, "turns:")
}

func (opts Options) iceAuthTTL() time.Duration {
	if opts.ICEAuthTTL > 0 {
		return opts.ICEAuthTTL
	}
	return defaultICEAuthTTL
}

// BuildICEServers converts Options into pion ICEServer entries with per-URL
// credentials, optional coturn REST minting, and static fallback credentials.
func BuildICEServers(opts Options) ([]pion.ICEServer, error) {
	if len(opts.ICEServers) == 0 {
		return nil, nil
	}
	restUser, restPass := "", ""
	if strings.TrimSpace(opts.ICEAuthSecret) != "" {
		restUser, restPass = TURNRESTCredential(opts.ICEAuthSecret, opts.ICEUsername, opts.iceAuthTTL())
	}
	out := make([]pion.ICEServer, 0, len(opts.ICEServers))
	for _, line := range opts.ICEServers {
		p, err := parseICEServerLine(line)
		if err != nil {
			return nil, err
		}
		user, pass := p.Username, p.Password
		if user == "" && pass == "" && isTURNURL(p.URL) {
			if restUser != "" {
				user, pass = restUser, restPass
			} else {
				user, pass = opts.ICEUsername, opts.ICECredential
			}
		}
		srv := pion.ICEServer{URLs: []string{p.URL}}
		if user != "" || pass != "" {
			srv.Username = user
			srv.Credential = pass
			srv.CredentialType = pion.ICECredentialTypePassword
		}
		out = append(out, srv)
	}
	return out, nil
}

func authMode(opts Options) string {
	if strings.TrimSpace(opts.ICEAuthSecret) != "" {
		return "turn_rest"
	}
	if strings.TrimSpace(opts.ICEUsername) != "" || strings.TrimSpace(opts.ICECredential) != "" {
		return "static"
	}
	for _, u := range opts.ICEServers {
		p, err := parseICEServerLine(u)
		if err == nil && (p.Username != "" || p.Password != "") {
			return "inline_url"
		}
	}
	return "none"
}
