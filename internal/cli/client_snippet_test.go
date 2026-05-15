package cli

import (
	"testing"
)

func TestApplyClientSnippetFromJSON(t *testing.T) {
	parent := Config{
		ToolVersion: "parent-v",
		ApiAddr:     "127.0.0.1:9999",
		ServerMode:  true,
	}
	raw := `{"transport":"udp","local_addr":"127.0.0.1:0","remote_addr":"127.0.0.1:5060","scenario":"<?xml version=\"1.0\"?><scenario/>"}`
	cfg, err := ApplyClientSnippetFromJSON(parent, []byte(raw), "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ApiAddr != "" {
		t.Fatalf("ApiAddr should be cleared, got %q", cfg.ApiAddr)
	}
	if cfg.ServerMode {
		t.Fatal("ServerMode should be false")
	}
	if cfg.ToolVersion != parent.ToolVersion {
		t.Fatalf("ToolVersion: got %q want %q", cfg.ToolVersion, parent.ToolVersion)
	}
	if cfg.Transport != "udp" {
		t.Fatalf("Transport: got %q", cfg.Transport)
	}
}

func TestApplyClientSnippetFromJSONForbiddenKeys(t *testing.T) {
	parent := Config{}
	for _, key := range []string{"aliases", "workloads", "server", "clients", "client", "listeners", "api_addr", "api_token", "auth"} {
		data := []byte(`{"` + key + `":{}}`)
		_, err := ApplyClientSnippetFromJSON(parent, data, "")
		if err == nil {
			t.Fatalf("expected error for key %q", key)
		}
	}
}
