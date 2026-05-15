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

func TestLegacyConfigServerFlagErrors(t *testing.T) {
	_, _, err := parseRunProfileMeta([]string{"-config-server", "/etc/gossipper/s.json"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "was removed") {
		t.Fatalf("err=%v", err)
	}
}

func TestLegacyConfigClientFlagErrors(t *testing.T) {
	_, _, err := parseRunProfileMeta([]string{"-config-client", "/etc/gossipper/c.json"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "was removed") {
		t.Fatalf("err=%v", err)
	}
}

func TestPostNormalizeRunProfileMetaMovesImplicitServerConfig(t *testing.T) {
	rest, meta, err := parseRunProfileMeta([]string{InternalServerSubcommandArgv, "-config", "/flat.json", "-api_addr", ":9000"})
	if err != nil {
		t.Fatal(err)
	}
	if !meta.ImplicitServerSubcommand || meta.ConfigPath != "" || meta.ServerFlatConfigPath != "/flat.json" {
		t.Fatalf("meta=%+v", meta)
	}
	if got := strings.Join(rest, " "); got != "-api_addr :9000" {
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
		t.Fatal("expected ServerMode cleared by client flat load")
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
	path := filepath.Join(repoRoot, "examples", "gossipper-management.json")
	cfg, err := Parse(ServerSubcommandPrependsFlag([]string{"-config", path}))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.ServerMode {
		t.Fatal("expected ServerMode from flat management JSON")
	}
	if cfg.ApiAddr != ":8080" || cfg.LocalPort != 5060 {
		t.Fatalf("api=%q port=%d", cfg.ApiAddr, cfg.LocalPort)
	}
	if cfg.ScenarioName != "management" {
		t.Fatalf("scenario=%q want management", cfg.ScenarioName)
	}
}

func TestParseConfigServerMultiListenerExampleJSON(t *testing.T) {
	t.Parallel()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	path := filepath.Join(repoRoot, "examples", "gossipper-hybrid.json")
	cfg, err := Parse(ServerSubcommandPrependsFlag([]string{"-config", path}))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.ServerListeners) != 3 {
		t.Fatalf("listeners=%+v", cfg.ServerListeners)
	}
	if cfg.ServerListeners[0].LocalPort != 5060 || cfg.ServerListeners[1].LocalPort != 5061 || cfg.ServerListeners[2].LocalPort != 5062 {
		t.Fatalf("ports %+v", cfg.ServerListeners)
	}
	if cfg.ServerListeners[1].Transport != "t1" {
		t.Fatalf("second transport=%q want t1", cfg.ServerListeners[1].Transport)
	}
	if cfg.ScenarioName != "management" {
		t.Fatalf("scenario=%q", cfg.ScenarioName)
	}
}

func TestParseConfigClientExampleJSON(t *testing.T) {
	t.Parallel()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	path := filepath.Join(repoRoot, "examples", "gossipper-uac.json")
	cfg, err := Parse(ServerSubcommandPrependsFlag([]string{"-config", path}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ServerMode {
		t.Fatal("expected non-server from flat client JSON")
	}
	if cfg.ScenarioName != "uac" || cfg.RemoteHost != "127.0.0.1" || cfg.RemotePort != 5060 {
		t.Fatalf("cfg=%+v", cfg)
	}
	if cfg.TotalCalls != 1 || !cfg.TotalCallsSetExplicitly {
		t.Fatalf("total_calls: m=%d explicit=%v", cfg.TotalCalls, cfg.TotalCallsSetExplicitly)
	}
}

func TestParseCompositeHybridFlatJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "hybrid.json")
	raw := []byte(`{
  "api_addr": ":18080",
  "server": {"id": "sip", "role": "management", "scenario_name": "management", "transport": "u1", "local_ip": "127.0.0.1", "local_port": 50660},
  "clients": [
    {"id": "load", "role": "load", "scenario_name": "uac", "transport": "u1", "local_ip": "127.0.0.1", "local_port": 50661, "remote_addr": "127.0.0.1:50660", "total_calls": 1, "rate": 1, "max_concurrent": 1}
  ]
}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Parse(ServerSubcommandPrependsFlag([]string{"-config", path}))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.ServerMode {
		t.Fatal("expected ServerMode from composite primary")
	}
	if cfg.ServerProfileID != "sip" {
		t.Fatalf("ServerProfileID=%q", cfg.ServerProfileID)
	}
	if cfg.ApiAddr != ":18080" {
		t.Fatalf("ApiAddr=%q", cfg.ApiAddr)
	}
	if len(cfg.JoinedClients) != 1 {
		t.Fatalf("joined=%d", len(cfg.JoinedClients))
	}
	if cfg.JoinedClients[0].ID != "load" {
		t.Fatalf("id=%q", cfg.JoinedClients[0].ID)
	}
	if cfg.JoinedClients[0].Config.ServerMode {
		t.Fatal("joined profile should be load (ServerMode false)")
	}
	if cfg.JoinedClients[0].Config.ApiAddr != "" {
		t.Fatalf("joined should clear ApiAddr, got %q", cfg.JoinedClients[0].Config.ApiAddr)
	}
	if cfg.JoinedClients[0].Config.RemotePort != 50660 {
		t.Fatalf("remote port=%d", cfg.JoinedClients[0].Config.RemotePort)
	}
}

func TestParseCompositeHybridMultiListenerMultiLoad(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "hybrid-multi.json")
	raw := []byte(`{
  "api_addr": ":18080",
  "server": {"id": "sip", "role": "management", "scenario_name": "management", "listeners": [
      {"transport": "u1", "local_ip": "127.0.0.1", "local_port": 50660},
      {"transport": "t1", "local_ip": "127.0.0.1", "local_port": 50661}
    ], "rate": 1, "max_concurrent": 8},
  "clients": [
    {"id": "u", "role": "load", "scenario_name": "uac", "transport": "u1", "local_ip": "127.0.0.1", "local_port": 50700, "remote_addr": "127.0.0.1:50660", "total_calls": 1, "rate": 1, "max_concurrent": 1},
    {"id": "t", "role": "load", "scenario_name": "uac", "transport": "t1", "local_ip": "127.0.0.1", "local_port": 50701, "remote_addr": "127.0.0.1:50661", "total_calls": 1, "rate": 1, "max_concurrent": 1}
  ]
}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Parse(ServerSubcommandPrependsFlag([]string{"-config", path}))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.ServerListeners) != 2 {
		t.Fatalf("primary ServerListeners=%d", len(cfg.ServerListeners))
	}
	if cfg.ServerListeners[0].Transport != "u1" || cfg.ServerListeners[1].Transport != "t1" {
		t.Fatalf("listeners transport=%q %q", cfg.ServerListeners[0].Transport, cfg.ServerListeners[1].Transport)
	}
	if len(cfg.JoinedClients) != 2 {
		t.Fatalf("joined=%d", len(cfg.JoinedClients))
	}
	if cfg.JoinedClients[0].Config.Transport != "u1" || cfg.JoinedClients[0].Config.LocalPort != 50700 {
		t.Fatalf("first load t=%q p=%d", cfg.JoinedClients[0].Config.Transport, cfg.JoinedClients[0].Config.LocalPort)
	}
	if cfg.JoinedClients[1].Config.Transport != "t1" || cfg.JoinedClients[1].Config.LocalPort != 50701 {
		t.Fatalf("second load t=%q p=%d", cfg.JoinedClients[1].Config.Transport, cfg.JoinedClients[1].Config.LocalPort)
	}
}

func TestApplyRunSpecListenersSyncsPrimaryBind(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Transport = "u1"
	cfg.LocalIP = "0.0.0.0"
	cfg.LocalPort = 5060
	p9999 := 9999
	spec := runSpec{
		Listeners: []listenerRunSpec{
			{LocalIP: ptr("10.0.0.1"), LocalPort: &p9999},
		},
	}
	if err := applyRunSpec(&cfg, &spec, "."); err != nil {
		t.Fatal(err)
	}
	if len(cfg.ServerListeners) != 1 {
		t.Fatalf("listeners len=%d", len(cfg.ServerListeners))
	}
	if cfg.LocalIP != "10.0.0.1" || cfg.LocalPort != 9999 {
		t.Fatalf("primary bind got %s:%d", cfg.LocalIP, cfg.LocalPort)
	}
	if cfg.ServerListeners[0].Transport != "u1" {
		t.Fatalf("listener transport=%q", cfg.ServerListeners[0].Transport)
	}
}

func TestInferServerFlatManagementRoleAndHeuristics(t *testing.T) {
	dir := t.TempDir()
	mgmtRole := filepath.Join(dir, "mgmt-role.json")
	if err := os.WriteFile(mgmtRole, []byte(`{"role":"server","remote_addr":"10.0.0.1:5060"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	ok, err := InferServerFlatManagement(mgmtRole)
	if err != nil || !ok {
		t.Fatalf("management role: ok=%v err=%v", ok, err)
	}
	loadRole := filepath.Join(dir, "load-role.json")
	if err := os.WriteFile(loadRole, []byte(`{"role":"load","api_addr":":8080"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	ok2, err := InferServerFlatManagement(loadRole)
	if err != nil || ok2 {
		t.Fatalf("load role: ok=%v err=%v", ok2, err)
	}
	serverJSON := filepath.Join(dir, "server-block.json")
	if err := os.WriteFile(serverJSON, []byte(`{"server":{"scenario_name":"management","transport":"u1","local_ip":"127.0.0.1","local_port":5060}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	ok3, err := InferServerFlatManagement(serverJSON)
	if err != nil || !ok3 {
		t.Fatalf("top-level server object: ok=%v err=%v", ok3, err)
	}
}

func TestInferServerFlatManagementAmbiguous(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ambig.json")
	if err := os.WriteFile(path, []byte(`{"scenario_name":"invite_media","remote_addr":"10.0.0.1:5060"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := InferServerFlatManagement(path); err == nil {
		t.Fatal("expected error")
	}
}

func TestInferServerFlatManagementRejectsAliases(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte(`{"aliases":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := InferServerFlatManagement(path); err == nil {
		t.Fatal("expected error")
	}
}

func TestParseImplicitServerRejectsRunProfileFlags(t *testing.T) {
	_, err := Parse([]string{InternalServerSubcommandArgv, "-config", "/tmp/x.json", "-run-alias", "a"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "run profiles must use the root command") {
		t.Fatalf("err=%v", err)
	}
}

func ptr[T any](v T) *T {
	return &v
}
