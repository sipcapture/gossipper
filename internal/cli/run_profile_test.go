package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestParseRunProfileMeta(t *testing.T) {
	rest, meta, err := parseRunProfileMeta([]string{"-config", "/a/b.json", "-run-alias", "u1", "-sn", "uac"})
	if err != nil {
		t.Fatal(err)
	}
	if meta.ConfigPath != "/a/b.json" || meta.RunAlias != "u1" || meta.ListAliases {
		t.Fatalf("meta=%+v", meta)
	}
	if got := strings.Join(rest, " "); got != "-sn uac" {
		t.Fatalf("rest=%q", got)
	}
}

func TestParseRunProfileMetaConfigServer(t *testing.T) {
	rest, meta, err := parseRunProfileMeta([]string{"-config-server", "/etc/gossipper/s.json", "-api_addr", ":9000"})
	if err != nil {
		t.Fatal(err)
	}
	if meta.ServerConfigPath != "/etc/gossipper/s.json" || meta.ConfigPath != "" || meta.RunAlias != "" {
		t.Fatalf("meta=%+v", meta)
	}
	if got := strings.Join(rest, " "); got != "-api_addr :9000" {
		t.Fatalf("rest=%q", got)
	}
}

func TestParseRunProfileMetaConfigClient(t *testing.T) {
	rest, meta, err := parseRunProfileMeta([]string{"-config-client", "/etc/gossipper/c.json", "-m", "5"})
	if err != nil {
		t.Fatal(err)
	}
	if meta.ClientConfigPath != "/etc/gossipper/c.json" || meta.ServerConfigPath != "" || meta.ConfigPath != "" {
		t.Fatalf("meta=%+v", meta)
	}
	if got := strings.Join(rest, " "); got != "-m 5" {
		t.Fatalf("rest=%q", got)
	}
}

func TestLoadAndApplyServerConfigRejectsAliasesLayout(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	examplePath := filepath.Join(repoRoot, "testdata", "run-profiles", "example.json")
	cfg := DefaultConfig()
	if _, err := LoadAndApplyServerConfig(&cfg, examplePath); err == nil {
		t.Fatal("expected error for aliases-shaped JSON")
	} else if !strings.Contains(err.Error(), "aliases") {
		t.Fatalf("err=%v", err)
	}
}

func TestLoadAndApplyServerConfigFlat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.json")
	raw := []byte(`{"transport":"u1","local_port":5099,"api_addr":":7070","rate":1,"max_concurrent":8}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	if _, err := LoadAndApplyServerConfig(&cfg, path); err != nil {
		t.Fatal(err)
	}
	if cfg.Transport != "u1" || cfg.LocalPort != 5099 || cfg.ApiAddr != ":7070" || cfg.MaxConcurrent != 8 {
		t.Fatalf("cfg=%+v", cfg)
	}
}

func TestLoadAndApplyClientConfigRejectsAliasesLayout(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	examplePath := filepath.Join(repoRoot, "testdata", "run-profiles", "example.json")
	cfg := DefaultConfig()
	if _, err := LoadAndApplyClientConfig(&cfg, examplePath); err == nil {
		t.Fatal("expected error for aliases-shaped JSON")
	} else if !strings.Contains(err.Error(), "aliases") {
		t.Fatalf("err=%v", err)
	}
}

func TestLoadAndApplyClientConfigClearsServerMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "client.json")
	raw := []byte(`{"server":true,"scenario_name":"uac","remote_addr":"10.0.0.1:5060","total_calls":1,"rate":1,"max_concurrent":1}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	if _, err := LoadAndApplyClientConfig(&cfg, path); err != nil {
		t.Fatal(err)
	}
	if cfg.ServerMode {
		t.Fatal("expected ServerMode cleared by -config-client load")
	}
	if cfg.ScenarioName != "uac" || cfg.RemoteHost != "10.0.0.1" || cfg.RemotePort != 5060 {
		t.Fatalf("cfg=%+v", cfg)
	}
}

func TestApplyRunSpecRelativeScenario(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig()
	spec := runSpec{
		ScenarioFile: ptr("scen/uas.xml"),
		LocalPort:    ptr(9050),
	}
	if err := applyRunSpec(&cfg, &spec, dir); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "scen", "uas.xml")
	if cfg.ScenarioFile != want {
		t.Fatalf("path: got %q want %q", cfg.ScenarioFile, want)
	}
	if cfg.LocalPort != 9050 {
		t.Fatalf("port %d", cfg.LocalPort)
	}
}

func TestLoadAndApplyRunProfileUnknownAlias(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "g.json")
	if err := os.WriteFile(path, []byte(`{"aliases":{"a":{}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	_, err := LoadAndApplyRunProfile(&cfg, path, "missing")
	if err == nil || !strings.Contains(err.Error(), "unknown alias") {
		t.Fatalf("err=%v", err)
	}
}

func TestParseListAliasesExit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "g.json")
	if err := os.WriteFile(path, []byte(`{"aliases":{"x":{}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Parse([]string{"-config", path, "-list-aliases"})
	if !errors.Is(err, ErrListAliases) {
		t.Fatalf("got err=%v", err)
	}
}

func TestPrintRunProfileAliases(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "g.json")
	doc := map[string]map[string]struct{}{
		"aliases": {
			"zebra": {},
			"alpha": {},
		},
	}
	raw, _ := json.Marshal(doc)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	// Smoke: no panic, sorted output checked indirectly via Parse ErrListAliases path in integration test below.
	if err := printRunProfileAliases(path); err != nil {
		t.Fatal(err)
	}
}

func TestParseWithBundledExampleRunProfile(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	cfgPath := filepath.Join(repoRoot, "testdata", "run-profiles", "example.json")
	cfg, err := Parse([]string{"-config", cfgPath, "-run-alias", "uac-local"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ScenarioName != "uac" || cfg.RemoteHost != "127.0.0.1" || cfg.RemotePort != 5060 {
		t.Fatalf("cfg=%+v", cfg)
	}
	if !cfg.SendMediaReport || !cfg.HEPHomerLakeRTCP || cfg.HEPRawRTCP {
		t.Fatalf("uac-local homer-lake: send=%v homer=%v raw=%v", cfg.SendMediaReport, cfg.HEPHomerLakeRTCP, cfg.HEPRawRTCP)
	}

	cfg2, err := Parse([]string{"-config", cfgPath, "-run-alias", "hep-uas-listen"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cfg2.ScenarioFile); err != nil {
		t.Fatalf("resolved scenario_file %q: %v", cfg2.ScenarioFile, err)
	}
	if cfg2.LogOTELEndpoint != "http://127.0.0.1:4318" || cfg2.LogOTELProto != "http" || !cfg2.LogOTELInsecure {
		t.Fatalf("otlp from profile: endpoint=%q proto=%q insecure=%v", cfg2.LogOTELEndpoint, cfg2.LogOTELProto, cfg2.LogOTELInsecure)
	}
	if cfg2.HEPAddr != "127.0.0.1:9060" || cfg2.HEPCaptureID != 2001 {
		t.Fatalf("hep from profile: addr=%q capture_id=%d", cfg2.HEPAddr, cfg2.HEPCaptureID)
	}
	if !cfg2.SendMediaReport || !cfg2.HEPHomerLakeRTCP || cfg2.HEPRawRTCP {
		t.Fatalf("homer-lake media from profile: send=%v homer=%v raw=%v", cfg2.SendMediaReport, cfg2.HEPHomerLakeRTCP, cfg2.HEPRawRTCP)
	}

	hepPath := filepath.Join(repoRoot, "testdata", "run-profiles", "hep-scripts.json")
	cfg3, err := Parse([]string{"-config", hepPath, "-run-alias", "hep-uac-send-raw-rtcp", "-hep_addr", "127.0.0.1:9060"})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg3.SendMediaReport || cfg3.HEPHomerLakeRTCP || !cfg3.HEPRawRTCP {
		t.Fatalf("raw rtcp alias: homer=%v raw=%v send=%v", cfg3.HEPHomerLakeRTCP, cfg3.HEPRawRTCP, cfg3.SendMediaReport)
	}
}

func TestApplyRunSpecServer(t *testing.T) {
	cfg := DefaultConfig()
	trueVal := true
	spec := runSpec{Server: &trueVal}
	if err := applyRunSpec(&cfg, &spec, "."); err != nil {
		t.Fatal(err)
	}
	if !cfg.ServerMode {
		t.Fatal("ServerMode")
	}
}

func TestParseConfigServerExampleJSON(t *testing.T) {
	t.Parallel()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	path := filepath.Join(repoRoot, "examples", "gossipper-server.json")
	cfg, err := Parse([]string{"-config-server", path})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.ServerMode {
		t.Fatal("expected ServerMode from -config-server")
	}
	if cfg.ApiAddr != ":8080" || cfg.LocalPort != 5060 {
		t.Fatalf("api=%q port=%d", cfg.ApiAddr, cfg.LocalPort)
	}
	if cfg.ScenarioName != "management" {
		t.Fatalf("scenario=%q want management", cfg.ScenarioName)
	}
}

func TestParseConfigClientExampleJSON(t *testing.T) {
	t.Parallel()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	path := filepath.Join(repoRoot, "examples", "gossipper-client.json")
	cfg, err := Parse([]string{"-config-client", path})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ServerMode {
		t.Fatal("expected non-server from -config-client")
	}
	if cfg.ScenarioName != "uac" || cfg.RemoteHost != "127.0.0.1" || cfg.RemotePort != 5060 {
		t.Fatalf("cfg=%+v", cfg)
	}
	if cfg.TotalCalls != 1 || !cfg.TotalCallsSetExplicitly {
		t.Fatalf("total_calls: m=%d explicit=%v", cfg.TotalCalls, cfg.TotalCallsSetExplicitly)
	}
}

func ptr[T any](v T) *T {
	return &v
}

