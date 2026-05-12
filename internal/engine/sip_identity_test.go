package engine

import (
	"testing"
)

func TestApplySIPIdentityKeywordsDefaults(t *testing.T) {
	t.Parallel()
	m := map[string]string{"routes": ""}
	cfg := Config{}
	applySIPIdentityKeywords(m, cfg, "10.0.0.5", 5060)
	if got := m["trunk_from"]; got != "gossip <sip:gossip@10.0.0.5:5060>" {
		t.Fatalf("trunk_from: %q", got)
	}
	if m["trunk_pai"] != "" || m["trunk_provider"] != "" || m["trunk_extra"] != "" {
		t.Fatalf("expected empty optional headers, got pai=%q prov=%q extra=%q", m["trunk_pai"], m["trunk_provider"], m["trunk_extra"])
	}
}

func TestApplySIPIdentityKeywordsTrunk(t *testing.T) {
	t.Parallel()
	m := map[string]string{}
	cfg := Config{
		SipFrom:         `"ACME" <sip:+15551212@trunk.example>`,
		SipPAI:          "sip:+15551212@trunk.example",
		SipProvider:     "prov1",
		SipExtraHeaders: []string{"X-Custom: abc", "X-Other: def"},
	}
	applySIPIdentityKeywords(m, cfg, "10.0.0.1", 5060)
	if m["trunk_from"] != cfg.SipFrom {
		t.Fatalf("trunk_from: %q", m["trunk_from"])
	}
	if m["trunk_pai"] != "P-Asserted-Identity: sip:+15551212@trunk.example\r\n" {
		t.Fatalf("trunk_pai: %q", m["trunk_pai"])
	}
	if m["trunk_provider"] != "X-provider: prov1\r\n" {
		t.Fatalf("trunk_provider: %q", m["trunk_provider"])
	}
	if m["trunk_extra"] != "X-Custom: abc\r\nX-Other: def\r\n" {
		t.Fatalf("trunk_extra: %q", m["trunk_extra"])
	}
}
