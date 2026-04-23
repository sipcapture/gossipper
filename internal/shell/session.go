package shell

import (
	"fmt"
	"io"
	"net"
	"sort"
	"strings"
)

// Session accumulates CLI flags for gossipper (same tokens as os.Args without program name).
type Session struct {
	order []string
	flags map[string]string
	// Optional split remote (SIP peer); merged into -rsa by applyDestinationParts.
	destHost string
	destPort string
}

func newSession() *Session {
	return &Session{
		flags: make(map[string]string),
	}
}

func (s *Session) Reset() {
	s.order = nil
	s.flags = make(map[string]string)
	s.destHost = ""
	s.destPort = ""
}

func normalizeShellKey(name string) string {
	n := strings.TrimSpace(strings.ToLower(name))
	n = strings.TrimPrefix(n, "-")
	return strings.ReplaceAll(n, "-", "_")
}

// canonicalFlag returns the flag name as used by the stdlib flag set (no leading dash).
func canonicalFlag(name string) string {
	n := normalizeShellKey(name)
	if v, ok := readableShellFlagAliases[n]; ok {
		return v
	}
	return n
}

// readableShellFlagAliases maps human-readable shell keys to gossipper CLI flag names.
var readableShellFlagAliases = map[string]string{
	// Remote SIP peer (host:port) — same as -rsa
	"remote":             "rsa",
	"dst":                "rsa",
	"target":             "rsa",
	"peer":               "rsa",
	"destination":        "rsa",
	"destination_addr":   "rsa",
	"remote_address":     "rsa",
	"remote_addr":        "rsa",
	"sip_remote":         "rsa",
	"sip_peer":           "rsa",
	"sip_destination":    "rsa",
	"remote_sip":         "rsa",
	"destination_sip":    "rsa",
	"sip_peer_address":   "rsa",
	// Built-in / file scenario
	"scenario":          "sn",
	"mode":              "sn",
	"builtin_scenario":  "sn",
	"scenario_name":     "sn",
	"scenario_file":     "sf",
	"xml":               "sf",
	"xml_scenario":      "sf",
	// Service / transport / bind
	"service_name": "s",
	"svc":          "s",
	"transport":    "t",
	"proto":        "t",
	"local":        "i",
	"local_ip":     "i",
	"bind":         "i",
	"listen":       "i",
	"bind_ip":      "i",
	"listen_ip":    "i",
	"source_ip":    "i",
	"listen_address": "i",
	"local_bind_ip":  "i",
	"port":           "p",
	"listen_port":    "p",
	"local_port":     "p",
	"bind_port":      "p",
	"sip_local_port": "p",
	// Calls / rate / concurrency
	"calls":               "m",
	"n":                   "m",
	"total_calls":         "m",
	"call_count":          "m",
	"calls_total":         "m",
	"rate":                "r",
	"cps":                 "r",
	"calls_per_second":    "r",
	"concurrency":         "l",
	"max_concurrent":      "l",
	"parallel_calls":      "l",
	"max_parallel_calls":  "l",
	// Auth
	"auth_user":     "au",
	"username":      "au",
	"auth_username": "au",
	"auth_pass":     "ap",
	"password":      "ap",
	"auth_password": "ap",
	// Injection (UI transport)
	"injection_file": "inf",
	"injection_csv":  "inf",
	"csv_inf":        "inf",
	"ipfield":         "ipfield",
	"source_ip_field": "ip_field",
	"ip_column":       "ip_field",
	// Timeouts / stats output
	"timeout":              "timeout_global",
	"global_timeout":       "timeout_global",
	"runtime_limit":        "timeout_global",
	"runtime_limit_seconds": "timeout_global",
	"hep":                  "hep_addr",
	"hep_collector":        "hep_addr",
	"hep_address":          "hep_addr",
	"hep_auth_key": "hep_password",
	"summary":              "summary_json",
	"summary_out":          "summary_json",
	"stats_file":           "summary_json",
	"rate_period_ms": "rp",
	"rate_period":    "rp",
	"recv_timeout":     "recv_timeout_ms",
	"recv_timeout_ms":  "recv_timeout_ms",
	"pause":            "pause_ms",
	"pause_ms":         "pause_ms",
	"default_pause_ms": "pause_ms",
}

func (s *Session) isSplitDestinationKey(n string) bool {
	switch n {
	case "destination_host", "remote_host", "sip_peer_host", "peer_host", "sip_remote_host":
		return true
	case "destination_port", "remote_port", "sip_peer_port", "peer_port", "sip_remote_port":
		return true
	default:
		return false
	}
}

func (s *Session) applyDestinationParts() error {
	if s.destHost == "" {
		s.removeFlagFromOrder("rsa")
		delete(s.flags, "rsa")
		return nil
	}
	port := s.destPort
	if port == "" {
		port = "5060"
	}
	host := s.destHost
	combined := net.JoinHostPort(host, port)
	s.flags["rsa"] = combined
	s.upsertOrder("rsa")
	return nil
}

func (s *Session) removeFlagFromOrder(key string) {
	for i, k := range s.order {
		if k == key {
			s.order = append(s.order[:i], s.order[i+1:]...)
			return
		}
	}
}

// Set records a flag. For booleans, empty value means enable (same as CLI bare -flag).
func (s *Session) Set(name, value string) error {
	raw := normalizeShellKey(name)
	if s.isSplitDestinationKey(raw) {
		val := strings.TrimSpace(value)
		switch raw {
		case "destination_host", "remote_host", "sip_peer_host", "peer_host", "sip_remote_host":
			s.destHost = val
		case "destination_port", "remote_port", "sip_peer_port", "peer_port", "sip_remote_port":
			s.destPort = val
		}
		return s.applyDestinationParts()
	}

	key := canonicalFlag(name)
	if key == "" {
		return fmt.Errorf("empty flag name")
	}
	if key == "rsa" {
		s.destHost = ""
		s.destPort = ""
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
	raw := normalizeShellKey(name)
	if s.isSplitDestinationKey(raw) {
		switch raw {
		case "destination_host", "remote_host", "sip_peer_host", "peer_host", "sip_remote_host":
			s.destHost = ""
		case "destination_port", "remote_port", "sip_peer_port", "peer_port", "sip_remote_port":
			s.destPort = ""
		}
		_ = s.applyDestinationParts()
		return
	}

	key := canonicalFlag(name)
	if key == "rsa" {
		s.destHost = ""
		s.destPort = ""
	}
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
	if len(argv) == 0 && s.destHost == "" && s.destPort == "" {
		fmt.Fprintln(out, "(no flags set; gossipper defaults apply where valid)")
		return
	}
	if len(argv) > 0 {
		fmt.Fprintf(out, "effective argv: %s\n", strings.Join(argv, " "))
	}
	if s.destHost != "" || s.destPort != "" {
		fmt.Fprintf(out, "split destination: host=%q port=%q\n", s.destHost, s.destPort)
	}
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
