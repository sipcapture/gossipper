package shell

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// Session accumulates CLI flags for gossipper (same tokens as os.Args without program name).
type Session struct {
	order []string
	flags map[string]string
}

func newSession() *Session {
	return &Session{
		flags: make(map[string]string),
	}
}

func (s *Session) Reset() {
	s.order = nil
	s.flags = make(map[string]string)
}

// canonicalFlag returns the flag name as used by the stdlib flag set (no leading dash).
func canonicalFlag(name string) string {
	n := strings.TrimSpace(strings.ToLower(name))
	n = strings.TrimPrefix(n, "-")
	switch n {
	case "remote", "dst", "target", "peer":
		return "rsa"
	case "scenario", "mode":
		return "sn"
	case "local", "local_ip", "bind", "listen":
		return "i"
	case "port", "listen_port":
		return "p"
	case "transport", "proto":
		return "t"
	case "calls", "n":
		return "m"
	case "rate", "cps":
		return "r"
	case "concurrency", "max_concurrent":
		return "l"
	case "service", "svc":
		return "s"
	case "scenario_file", "xml":
		return "sf"
	case "auth_user", "username":
		return "au"
	case "auth_pass", "password":
		return "ap"
	case "users":
		return "users"
	case "timeout", "global_timeout":
		return "timeout_global"
	case "hep":
		return "hep_addr"
	case "summary":
		return "summary_json"
	default:
		return n
	}
}

func isBoolFlag(name string) bool {
	_, ok := boolFlagNames[name]
	return ok
}

var boolFlagNames = map[string]struct{}{
	"trace_msg":         {},
	"trace_shortmsg":    {},
	"trace_counts":      {},
	"trace_err":         {},
	"trace_error_codes": {},
	"trace_logs":        {},
	"trace_stat":        {},
	"trace_rtt":         {},
	"trace_screen":      {},
	"reconnect_close":   {},
	"tls_skip_verify":   {},
	"hep_raw_rtcp":      {},
	"send_media_report": {},
	"rtp_send":          {},
}

// Set records a flag. For booleans, empty value means enable (same as CLI bare -flag).
func (s *Session) Set(name, value string) error {
	key := canonicalFlag(name)
	if key == "" {
		return fmt.Errorf("empty flag name")
	}
	if isBoolFlag(key) {
		v := strings.TrimSpace(strings.ToLower(value))
		switch v {
		case "", "1", "true", "yes", "on":
			s.flags[key] = boolTrue
		case "0", "false", "no", "off":
			s.flags[key] = boolFalse
		default:
			return fmt.Errorf("boolean flag %q: use true/false, on/off, 1/0, or omit value to enable", name)
		}
	} else {
		s.flags[key] = value
	}
	s.upsertOrder(key)
	return nil
}

const boolTrue = "\x00true"
const boolFalse = "\x00false"

func (s *Session) upsertOrder(key string) {
	for i, k := range s.order {
		if k == key {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
	s.order = append(s.order, key)
}

func (s *Session) Unset(name string) {
	key := canonicalFlag(name)
	delete(s.flags, key)
	for i, k := range s.order {
		if k == key {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
}

func (s *Session) Argv() []string {
	var out []string
	for _, key := range s.order {
		val, ok := s.flags[key]
		if !ok {
			continue
		}
		flagTok := "-" + key
		if isBoolFlag(key) {
			switch val {
			case boolTrue:
				out = append(out, flagTok)
			case boolFalse:
				out = append(out, flagTok+"=false")
			default:
				out = append(out, flagTok)
			}
			continue
		}
		out = append(out, flagTok, val)
	}
	return out
}

func (s *Session) Show(out io.Writer) {
	argv := s.Argv()
	if len(argv) == 0 {
		fmt.Fprintln(out, "(no flags set; gossipper defaults apply where valid)")
		return
	}
	fmt.Fprintf(out, "effective argv: %s\n", strings.Join(argv, " "))
	keys := make([]string, 0, len(s.flags))
	for k := range s.flags {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := s.flags[k]
		if isBoolFlag(k) {
			switch v {
			case boolTrue:
				v = "true"
			case boolFalse:
				v = "false"
			}
		}
		fmt.Fprintf(out, "  -%s = %q\n", k, v)
	}
}
