package launcher

import (
	"strings"
	"testing"
	"time"

	"github.com/adubovikov/gossipper/internal/cli"
	"github.com/adubovikov/gossipper/internal/stats"
)

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
