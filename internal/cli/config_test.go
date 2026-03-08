package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseCommandPeers(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "peers.cfg")
	if err := os.WriteFile(path, []byte("m;127.0.0.1:7001\ns1;127.0.0.1:7002\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(peers) error = %v", err)
	}

	cfg, err := Parse([]string{
		"-sn", "uac",
		"-rsa", "127.0.0.1:5060",
		"-cmd_name", "m",
		"-cmd_peers", path,
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.CommandName != "m" {
		t.Fatalf("unexpected command name %q", cfg.CommandName)
	}
	if cfg.CommandPeers["s1"] != "127.0.0.1:7002" {
		t.Fatalf("unexpected command peers: %+v", cfg.CommandPeers)
	}
}

func TestParseMasterSlaveAliases(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "peers.cfg")
	if err := os.WriteFile(path, []byte("m;127.0.0.1:7001\ns1;127.0.0.1:7002\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(peers) error = %v", err)
	}

	cfg, err := Parse([]string{
		"-sn", "uac",
		"-master", "m",
		"-slave_cfg", path,
		"-rsa", "127.0.0.1:5060",
	})
	if err != nil {
		t.Fatalf("Parse(master) error = %v", err)
	}
	if cfg.CommandName != "m" || cfg.CommandRole != "master" {
		t.Fatalf("unexpected master config: %+v", cfg)
	}

	cfg, err = Parse([]string{
		"-sn", "uac",
		"-slave", "s1",
		"-slave_cfg", path,
	})
	if err != nil {
		t.Fatalf("Parse(slave) error = %v", err)
	}
	if cfg.CommandName != "s1" || cfg.CommandRole != "slave" {
		t.Fatalf("unexpected slave config: %+v", cfg)
	}
}

func TestParseAcceptsServerTransportAliases(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]string{
		"-sn", "uas",
		"-t", "s1",
	})
	if err != nil {
		t.Fatalf("Parse(s1) error = %v", err)
	}
	if cfg.Transport != "s1" {
		t.Fatalf("unexpected transport %q", cfg.Transport)
	}

	cfg, err = Parse([]string{
		"-sn", "uas",
		"-t", "sn",
	})
	if err != nil {
		t.Fatalf("Parse(sn) error = %v", err)
	}
	if cfg.Transport != "sn" {
		t.Fatalf("unexpected transport %q", cfg.Transport)
	}
}

func TestParseEnablesTraceMessagesForMessageFile(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]string{
		"-sn", "uac",
		"-rsa", "127.0.0.1:5060",
		"-message_file", filepath.Join(t.TempDir(), "messages.log"),
		"-trace_shortmsg",
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if !cfg.TraceMessages {
		t.Fatal("expected message_file to enable full message tracing")
	}
	if !cfg.TraceShortMsg {
		t.Fatal("expected trace_shortmsg to be enabled")
	}
}

func TestParseEnablesTraceErrorAndLogFiles(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]string{
		"-sn", "uac",
		"-rsa", "127.0.0.1:5060",
		"-error_file", filepath.Join(t.TempDir(), "errors.log"),
		"-log_file", filepath.Join(t.TempDir(), "actions.log"),
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if !cfg.TraceErrors {
		t.Fatal("expected error_file to enable trace_err")
	}
	if !cfg.TraceLogs {
		t.Fatal("expected log_file to enable trace_logs")
	}
}

func TestParseAcceptsTraceErrorCodes(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]string{
		"-sn", "uac",
		"-rsa", "127.0.0.1:5060",
		"-trace_error_codes",
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if !cfg.TraceErrorCodes {
		t.Fatal("expected trace_error_codes to be enabled")
	}
}

func TestParseAcceptsHEPSettings(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]string{
		"-sn", "uac",
		"-rsa", "127.0.0.1:5060",
		"-hep_addr", "127.0.0.1:9060",
		"-hep_capture_id", "2001",
		"-hep_password", "secret",
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.HEPAddr != "127.0.0.1:9060" {
		t.Fatalf("unexpected HEP addr %q", cfg.HEPAddr)
	}
	if cfg.HEPCaptureID != 2001 {
		t.Fatalf("unexpected HEP capture ID %d", cfg.HEPCaptureID)
	}
	if cfg.HEPPassword != "secret" {
		t.Fatalf("unexpected HEP password %q", cfg.HEPPassword)
	}
}

func TestParseAuthCredentials(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]string{
		"-sn", "uac",
		"-rsa", "127.0.0.1:5060",
		"-s", "alice-service",
	})
	if err != nil {
		t.Fatalf("Parse(default auth) error = %v", err)
	}
	if cfg.AuthUsername != "alice-service" {
		t.Fatalf("expected auth username to default to service, got %q", cfg.AuthUsername)
	}
	if cfg.AuthPassword != "password" {
		t.Fatalf("expected default auth password, got %q", cfg.AuthPassword)
	}

	cfg, err = Parse([]string{
		"-sn", "uac",
		"-rsa", "127.0.0.1:5060",
		"-au", "alice",
		"-ap", "secret",
	})
	if err != nil {
		t.Fatalf("Parse(custom auth) error = %v", err)
	}
	if cfg.AuthUsername != "alice" || cfg.AuthPassword != "secret" {
		t.Fatalf("unexpected auth credentials: %+v", cfg)
	}
}
