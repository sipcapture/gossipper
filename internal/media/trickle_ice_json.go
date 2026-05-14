package media

import (
	"encoding/json"
	"strings"
)

// trickleICEEnvelope supports common browser / RFC 8839-style trickle JSON over SIP.
type trickleICEEnvelope struct {
	Candidates       []trickleCandidate `json:"candidates"`
	Candidate        json.RawMessage    `json:"candidate"` // string or object
	ICECandidates    []trickleCandidate `json:"iceCandidates"`
	UsernameFragment string             `json:"usernameFragment"`
	Password         string             `json:"password"`
	ICEUfrag         string             `json:"iceUfrag"`
	ICEPwd           string             `json:"icePwd"`
	Ufrag            string             `json:"ufrag"`
	Pwd              string             `json:"pwd"`
}

type trickleCandidate struct {
	Candidate        string `json:"candidate"`
	SDPMid           string `json:"sdpMid"`
	SDPMLineIndex    int    `json:"sdpMLineIndex"`
	UsernameFragment string `json:"usernameFragment"`
	Password         string `json:"password"`
}

// ParseTrickleICEJSONToSDPFragment converts application/trickle-ice+json body into SDP attribute lines
// (a=candidate / optional a=ice-ufrag / a=ice-pwd) so existing ICE parsers can consume it.
func ParseTrickleICEJSONToSDPFragment(jsonBody string) (string, error) {
	jsonBody = strings.TrimSpace(jsonBody)
	if jsonBody == "" {
		return "", nil
	}
	var arr []trickleICEEnvelope
	if err := json.Unmarshal([]byte(jsonBody), &arr); err == nil && len(arr) > 0 {
		return buildSDPFragmentFromTrickle(arr[0]), nil
	}
	var env trickleICEEnvelope
	if err := json.Unmarshal([]byte(jsonBody), &env); err != nil {
		return "", err
	}
	return buildSDPFragmentFromTrickle(env), nil
}

func buildSDPFragmentFromTrickle(env trickleICEEnvelope) string {
	var b strings.Builder
	add := func(line string) {
		line = strings.TrimSpace(line)
		if line == "" {
			return
		}
		if !strings.HasSuffix(line, "\n") {
			line += "\r\n"
		}
		b.WriteString(line)
	}
	ufrag := firstNonEmpty(env.ICEUfrag, env.Ufrag, env.UsernameFragment)
	pwd := firstNonEmpty(env.ICEPwd, env.Pwd, env.Password)
	if ufrag != "" {
		add("a=ice-ufrag:" + ufrag)
	}
	if pwd != "" {
		add("a=ice-pwd:" + pwd)
	}
	for _, c := range env.Candidates {
		writeOneCandidate(&b, c.Candidate)
	}
	for _, c := range env.ICECandidates {
		writeOneCandidate(&b, c.Candidate)
	}
	// single "candidate" field: string or {"candidate":"..."}
	if len(env.Candidate) > 0 {
		var s string
		if json.Unmarshal(env.Candidate, &s) == nil && s != "" {
			writeOneCandidate(&b, s)
		} else {
			var obj struct {
				Candidate string `json:"candidate"`
			}
			if json.Unmarshal(env.Candidate, &obj) == nil {
				writeOneCandidate(&b, obj.Candidate)
			}
		}
	}
	return b.String()
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func writeOneCandidate(b *strings.Builder, cand string) {
	cand = strings.TrimSpace(cand)
	if cand == "" {
		return
	}
	low := strings.ToLower(cand)
	if strings.HasPrefix(low, "a=candidate:") {
		b.WriteString(cand)
		if !strings.HasSuffix(cand, "\n") {
			b.WriteString("\r\n")
		}
		return
	}
	if strings.HasPrefix(low, "candidate:") {
		b.WriteString("a=" + cand)
		if !strings.HasSuffix(cand, "\n") {
			b.WriteString("\r\n")
		}
		return
	}
	b.WriteString("a=candidate:")
	b.WriteString(cand)
	b.WriteString("\r\n")
}
