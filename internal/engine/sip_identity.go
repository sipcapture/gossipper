package engine

import (
	"strconv"
	"strings"
)

// applySIPIdentityKeywords sets ExtraKeywords used by built-in scenarios:
//
//	[trunk_from] — From display-name/URI (without ";tag="; tag is in XML)
//	[trunk_pai] — optional "P-Asserted-Identity: …\r\n"
//	[trunk_provider] — optional "X-provider: …\r\n"
//	[trunk_extra] — optional additional header lines (each ends with CRLF)
func applySIPIdentityKeywords(m map[string]string, cfg Config, localIP string, localPort int) {
	if m == nil {
		return
	}
	from := strings.TrimSpace(cfg.SipFrom)
	if from == "" {
		from = "gossip <sip:gossip@" + localIP + ":" + strconv.Itoa(localPort) + ">"
	}
	m["trunk_from"] = from

	if p := strings.TrimSpace(cfg.SipPAI); p != "" {
		m["trunk_pai"] = "P-Asserted-Identity: " + p + "\r\n"
	} else {
		m["trunk_pai"] = ""
	}

	if pr := strings.TrimSpace(cfg.SipProvider); pr != "" {
		m["trunk_provider"] = "X-provider: " + pr + "\r\n"
	} else {
		m["trunk_provider"] = ""
	}

	var b strings.Builder
	for _, line := range cfg.SipExtraHeaders {
		line = strings.TrimSpace(line)
		if line == "" || !strings.Contains(line, ":") {
			continue
		}
		b.WriteString(line)
		switch {
		case strings.HasSuffix(line, "\r\n"):
			// ok
		case strings.HasSuffix(line, "\n"):
			b.WriteString("\r")
		default:
			b.WriteString("\r\n")
		}
	}
	m["trunk_extra"] = b.String()
}
