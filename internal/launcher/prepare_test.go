package launcher

import (
	"strings"
	"testing"
	"time"

	"github.com/sipcapture/gossipper/internal/cli"
	"github.com/sipcapture/gossipper/internal/stats"
)

func TestPreparePropagatesServerUDPListeners(t *testing.T) {
	t.Parallel()

	cfg := cli.DefaultConfig()
	cfg.ScenarioName = "uas"
	cfg.Transport = "u1"
	cfg.LocalIP = "127.0.0.1"
	cfg.LocalPort = 35060
	cfg.ServerListeners = []cli.ServerListener{
		{Transport: "u1", LocalIP: "127.0.0.1", LocalPort: 35060},
		{Transport: "un", LocalIP: "127.0.0.1", LocalPort: 35061},
	}

	prepared, err := Prepare(cfg)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if len(prepared.EngineConfig.ServerListeners) != 2 {
		t.Fatalf("engine listeners: %+v", prepared.EngineConfig.ServerListeners)
	}
	if prepared.EngineConfig.ServerListeners[1].Transport != "un" {
		t.Fatalf("second transport=%q", prepared.EngineConfig.ServerListeners[1].Transport)
	}
	if prepared.CLIConfig.LocalPort != 35060 {
		t.Fatalf("expected primary port from first listener, got %d", prepared.CLIConfig.LocalPort)
	}
}

func TestPrepareRejectsServerListenersForClientScenario(t *testing.T) {
	t.Parallel()

	cfg := cli.DefaultConfig()
	cfg.ScenarioName = "uac"
	cfg.RemoteHost = "127.0.0.1"
	cfg.RemotePort = 5060
	cfg.ServerListeners = []cli.ServerListener{
		{Transport: "u1", LocalIP: "127.0.0.1", LocalPort: 35060},
	}

	_, err := Prepare(cfg)
	if err == nil || !strings.Contains(err.Error(), "listeners in config are only supported for server scenarios") {
		t.Fatalf("expected error, got %v", err)
	}
}

func TestPrepareRejectsTLSListenerWithoutCert(t *testing.T) {
	t.Parallel()

	cfg := cli.DefaultConfig()
	cfg.ScenarioName = "uas"
	cfg.ServerListeners = []cli.ServerListener{
		{Transport: "l1", LocalIP: "127.0.0.1", LocalPort: 39070},
	}

	_, err := Prepare(cfg)
	if err == nil || !strings.Contains(err.Error(), "tls_cert") {
		t.Fatalf("expected tls cert error, got %v", err)
	}
}

func TestPreparePropagatesMixedServerListeners(t *testing.T) {
	t.Parallel()

	cfg := cli.DefaultConfig()
	cfg.ScenarioName = "uas"
	cfg.Transport = "u1"
	cfg.LocalIP = "127.0.0.1"
	cfg.LocalPort = 5060
	cfg.ServerListeners = []cli.ServerListener{
		{Transport: "u1", LocalIP: "127.0.0.1", LocalPort: 39060},
		{Transport: "t1", LocalIP: "127.0.0.1", LocalPort: 39061},
	}

	prepared, err := Prepare(cfg)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if len(prepared.EngineConfig.ServerListeners) != 2 {
		t.Fatalf("engine listeners: %+v", prepared.EngineConfig.ServerListeners)
	}
	if prepared.EngineConfig.ServerListeners[1].Transport != "t1" {
		t.Fatalf("second transport=%q", prepared.EngineConfig.ServerListeners[1].Transport)
	}
}

func TestPrepareNormalizesServerTransportAlias(t *testing.T) {
	t.Parallel()

	cfg := cli.DefaultConfig()
	cfg.ScenarioName = "uas"
	cfg.Transport = "s1"

	prepared, err := Prepare(cfg)
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if prepared.CLIConfig.Transport != "u1" {
		t.Fatalf("expected CLI transport to normalize to u1, got %q", prepared.CLIConfig.Transport)
	}
	if prepared.EngineConfig.Transport != "u1" {
		t.Fatalf("expected engine transport to normalize to u1, got %q", prepared.EngineConfig.Transport)
	}
}

func TestPrepareRejectsServerAliasForClientScenario(t *testing.T) {
	t.Parallel()

	cfg := cli.DefaultConfig()
	cfg.ScenarioName = "uac"
	cfg.Transport = "s1"

	_, err := Prepare(cfg)
	if err == nil || !strings.Contains(err.Error(), "transport s1 requires a server scenario") {
		t.Fatalf("expected transport validation error, got %v", err)
	}
}

func TestPrepareNormalizesTLSServerTransportAlias(t *testing.T) {
	t.Parallel()

	cfg := cli.DefaultConfig()
	cfg.ScenarioName = "uas"
	cfg.Transport = "sl"

	prepared, err := Prepare(cfg)
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if prepared.CLIConfig.Transport != "l1" || prepared.EngineConfig.Transport != "l1" {
		t.Fatalf("expected transport normalized to l1, cli=%q engine=%q",
			prepared.CLIConfig.Transport, prepared.EngineConfig.Transport)
	}
}

func TestPrepareRejectsInjectionForNonUIServer(t *testing.T) {
	t.Parallel()

	cfg := cli.DefaultConfig()
	cfg.ScenarioName = "uas"
	cfg.Transport = "sl"
	cfg.InjectionFile = "dummy.csv"
	cfg.IPField = 0
	cfg.UISourceIPs = []string{"127.0.0.2"}

	_, err := Prepare(cfg)
	if err == nil || !strings.Contains(err.Error(), "injection") {
		t.Fatalf("expected injection rejection for non-ui server, got %v", err)
	}
}

func TestPrepareRejectsTLSServerAliasForClientScenario(t *testing.T) {
	t.Parallel()

	cfg := cli.DefaultConfig()
	cfg.ScenarioName = "uac"
	cfg.Transport = "sl"

	_, err := Prepare(cfg)
	if err == nil || !strings.Contains(err.Error(), "transport sl requires a server scenario") {
		t.Fatalf("expected transport validation error, got %v", err)
	}
}

func TestPrepareNormalizesClientTLSAliases(t *testing.T) {
	t.Parallel()

	cfg := cli.DefaultConfig()
	cfg.ScenarioName = "uac"
	cfg.RemoteHost = "127.0.0.1"
	cfg.RemotePort = 5061
	cfg.Transport = "cl"

	prepared, err := Prepare(cfg)
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if prepared.CLIConfig.Transport != "l1" || prepared.EngineConfig.Transport != "l1" {
		t.Fatalf("expected cl normalized to l1, cli=%q engine=%q",
			prepared.CLIConfig.Transport, prepared.EngineConfig.Transport)
	}

	cfg.Transport = "cln"
	prepared, err = Prepare(cfg)
	if err != nil {
		t.Fatalf("Prepare(cln) error = %v", err)
	}
	if prepared.CLIConfig.Transport != "ln" || prepared.EngineConfig.Transport != "ln" {
		t.Fatalf("expected cln normalized to ln, cli=%q engine=%q",
			prepared.CLIConfig.Transport, prepared.EngineConfig.Transport)
	}
}

func TestPrepareRejectsClientTLSAliasForServerScenario(t *testing.T) {
	t.Parallel()

	cfg := cli.DefaultConfig()
	cfg.ScenarioName = "uas"
	cfg.Transport = "cl"

	_, err := Prepare(cfg)
	if err == nil || !strings.Contains(err.Error(), "transport cl requires a client scenario") {
		t.Fatalf("expected transport validation error, got %v", err)
	}

	cfg.Transport = "cln"
	_, err = Prepare(cfg)
	if err == nil || !strings.Contains(err.Error(), "transport cln requires a client scenario") {
		t.Fatalf("expected transport validation error for cln, got %v", err)
	}
}

func TestPrepareAcceptsUITransportForServerScenario(t *testing.T) {
	t.Parallel()

	cfg := cli.DefaultConfig()
	cfg.ScenarioName = "uas"
	cfg.Transport = "ui"
	cfg.InjectionFile = "dummy.csv"
	cfg.IPField = 0
	cfg.UISourceIPs = []string{"127.0.0.2"}

	prepared, err := Prepare(cfg)
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if prepared.EngineConfig.Transport != "ui" {
		t.Fatalf("expected ui transport for server scenario, got %q", prepared.EngineConfig.Transport)
	}
}

func TestPrepareNormalizesUppercaseUITransport(t *testing.T) {
	t.Parallel()

	cfg := cli.DefaultConfig()
	cfg.ScenarioName = "uac"
	cfg.Transport = "UI"
	cfg.InjectionFile = "dummy.csv"
	cfg.IPField = 0
	cfg.UISourceIPs = []string{"127.0.0.2"}
	cfg.RemoteHost = "127.0.0.1"
	cfg.RemotePort = 5060

	prepared, err := Prepare(cfg)
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if prepared.CLIConfig.Transport != "ui" || prepared.EngineConfig.Transport != "ui" {
		t.Fatalf("expected normalized ui transport, cli=%q engine=%q", prepared.CLIConfig.Transport, prepared.EngineConfig.Transport)
	}
}

func TestPrepareRejectsUITransportWithoutResolvedSourceIPs(t *testing.T) {
	t.Parallel()

	cfg := cli.DefaultConfig()
	cfg.ScenarioName = "uac"
	cfg.Transport = "ui"
	cfg.InjectionFile = "dummy.csv"
	cfg.IPField = 0
	cfg.UISourceIPs = nil

	_, err := Prepare(cfg)
	if err == nil || !strings.Contains(err.Error(), "transport ui requires at least one bind/source IP") {
		t.Fatalf("expected ui source ip validation error, got %v", err)
	}
}

func TestPreparePropagatesUISourceIPs(t *testing.T) {
	t.Parallel()

	cfg := cli.DefaultConfig()
	cfg.ScenarioName = "uac"
	cfg.Transport = "ui"
	cfg.InjectionFile = "dummy.csv"
	cfg.IPField = 0
	cfg.UISourceIPs = []string{"127.0.0.2", "127.0.0.3"}
	cfg.RemoteHost = "127.0.0.1"
	cfg.RemotePort = 5060

	prepared, err := Prepare(cfg)
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if len(prepared.EngineConfig.UISourceIPs) != 2 {
		t.Fatalf("expected 2 ui source ips, got %+v", prepared.EngineConfig.UISourceIPs)
	}
	if prepared.EngineConfig.UISourceIPs[0] != "127.0.0.2" || prepared.EngineConfig.UISourceIPs[1] != "127.0.0.3" {
		t.Fatalf("unexpected ui source ips %+v", prepared.EngineConfig.UISourceIPs)
	}
}

func TestPreparePropagatesReconnectSettings(t *testing.T) {
	t.Parallel()

	cfg := cli.DefaultConfig()
	cfg.ScenarioName = "uac"
	cfg.RemoteHost = "127.0.0.1"
	cfg.RemotePort = 5060
	cfg.MaxReconnect = 4
	cfg.ReconnectSleep = 120 * time.Millisecond
	cfg.ReconnectClose = true

	prepared, err := Prepare(cfg)
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if prepared.EngineConfig.MaxReconnect != 4 {
		t.Fatalf("expected max reconnect 4, got %d", prepared.EngineConfig.MaxReconnect)
	}
	if prepared.EngineConfig.ReconnectSleep != 120*time.Millisecond {
		t.Fatalf("expected reconnect sleep 120ms, got %v", prepared.EngineConfig.ReconnectSleep)
	}
	if !prepared.EngineConfig.ReconnectClose {
		t.Fatal("expected reconnect_close to propagate")
	}
}

func TestPrepareUnlimitedCallsFromExplicitZero(t *testing.T) {
	t.Parallel()

	cfg := cli.DefaultConfig()
	cfg.ScenarioName = "uac"
	cfg.RemoteHost = "127.0.0.1"
	cfg.RemotePort = 5060
	cfg.TotalCalls = 0
	cfg.TotalCallsSetExplicitly = true

	prepared, err := Prepare(cfg)
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if !prepared.EngineConfig.UnlimitedCalls {
		t.Fatal("expected EngineConfig.UnlimitedCalls")
	}
	if prepared.EngineConfig.TotalCalls != 0 {
		t.Fatalf("expected TotalCalls 0 for unlimited, got %d", prepared.EngineConfig.TotalCalls)
	}
}

func TestSummaryLineFormatsKeyCounters(t *testing.T) {
	t.Parallel()

	text := SummaryLine(stats.Summary{
		TotalCalls:         10,
		SuccessCalls:       8,
		FailedCalls:        2,
		CallsPerSecond:     4.5,
		AverageCallLatency: 2 * time.Second,
		AverageInviteRTT:   150 * time.Millisecond,
		Timeouts:           1,
	})
	for _, want := range []string{"calls=10", "success=8", "failed=2", "cps=4.50", "timeouts=1"} {
		if !strings.Contains(text, want) {
			t.Fatalf("SummaryLine() missing %q in %q", want, text)
		}
	}
}
