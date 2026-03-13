package cli

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestParseAcceptsUITransportWithInjection(t *testing.T) {
	t.Parallel()

	injectionPath := filepath.Join(t.TempDir(), "ips.csv")
	if err := os.WriteFile(injectionPath, []byte("127.0.0.2,alpha\n127.0.0.3,beta\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(injection) error = %v", err)
	}
	cfg, err := Parse([]string{
		"-sn", "uac",
		"-rsa", "127.0.0.1:5060",
		"-t", "ui",
		"-inf", injectionPath,
		"-ip_field", "0",
	})
	if err != nil {
		t.Fatalf("Parse(ui) error = %v", err)
	}
	if cfg.Transport != "ui" {
		t.Fatalf("unexpected transport %q", cfg.Transport)
	}
	if cfg.InjectionFile != injectionPath || cfg.IPField != 0 {
		t.Fatalf("unexpected injection config: file=%q ip_field=%d", cfg.InjectionFile, cfg.IPField)
	}
	if len(cfg.UISourceIPs) != 2 || cfg.UISourceIPs[0] != "127.0.0.2" || cfg.UISourceIPs[1] != "127.0.0.3" {
		t.Fatalf("unexpected ui source IPs: %+v", cfg.UISourceIPs)
	}
}

func TestParseUIPreservesInjectionOrderAndDuplicates(t *testing.T) {
	t.Parallel()

	injectionPath := filepath.Join(t.TempDir(), "ips.csv")
	if err := os.WriteFile(injectionPath, []byte(" 127.0.0.2 \n127.0.0.3\n127.0.0.2\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(injection) error = %v", err)
	}
	cfg, err := Parse([]string{
		"-sn", "uac",
		"-rsa", "127.0.0.1:5060",
		"-t", "ui",
		"-inf", injectionPath,
		"-ip_field", "0",
	})
	if err != nil {
		t.Fatalf("Parse(ui) error = %v", err)
	}
	want := []string{"127.0.0.2", "127.0.0.3", "127.0.0.2"}
	if len(cfg.UISourceIPs) != len(want) {
		t.Fatalf("expected %d ui source ips, got %+v", len(want), cfg.UISourceIPs)
	}
	for i := range want {
		if cfg.UISourceIPs[i] != want[i] {
			t.Fatalf("expected ui source ip[%d]=%q, got %q", i, want[i], cfg.UISourceIPs[i])
		}
	}
}

func TestParseUIHandlesLargeInjectionFile(t *testing.T) {
	t.Parallel()

	injectionPath := filepath.Join(t.TempDir(), "ips.csv")
	const rows = 4096
	var builder strings.Builder
	for i := 0; i < rows; i++ {
		if i%2 == 0 {
			builder.WriteString("127.0.0.2\n")
		} else {
			builder.WriteString("127.0.0.3\n")
		}
	}
	if err := os.WriteFile(injectionPath, []byte(builder.String()), 0o644); err != nil {
		t.Fatalf("WriteFile(injection) error = %v", err)
	}

	cfg, err := Parse([]string{
		"-sn", "uac",
		"-rsa", "127.0.0.1:5060",
		"-t", "ui",
		"-inf", injectionPath,
		"-ip_field", "0",
	})
	if err != nil {
		t.Fatalf("Parse(ui large inf) error = %v", err)
	}
	if len(cfg.UISourceIPs) != rows {
		t.Fatalf("expected %d ui source ips, got %d", rows, len(cfg.UISourceIPs))
	}
	if cfg.UISourceIPs[0] != "127.0.0.2" || cfg.UISourceIPs[1] != "127.0.0.3" || cfg.UISourceIPs[rows-1] != "127.0.0.3" {
		t.Fatalf("unexpected ui source IP ordering: first=%q second=%q last=%q", cfg.UISourceIPs[0], cfg.UISourceIPs[1], cfg.UISourceIPs[rows-1])
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

func TestParseEnablesTraceScreenForScreenFile(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]string{
		"-sn", "uac",
		"-rsa", "127.0.0.1:5060",
		"-screen_file", filepath.Join(t.TempDir(), "screen.log"),
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if !cfg.TraceScreen {
		t.Fatal("expected screen_file to enable trace_screen")
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

func TestParseAcceptsTraceRTT(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]string{
		"-sn", "uac",
		"-rsa", "127.0.0.1:5060",
		"-trace_rtt",
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if !cfg.TraceRTT {
		t.Fatal("expected trace_rtt to be enabled")
	}
}

func TestParseAcceptsTraceCounts(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]string{
		"-sn", "uac",
		"-rsa", "127.0.0.1:5060",
		"-trace_counts",
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if !cfg.TraceCounts {
		t.Fatal("expected trace_counts to be enabled")
	}
}

func TestParseAcceptsTraceScreen(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]string{
		"-sn", "uac",
		"-rsa", "127.0.0.1:5060",
		"-trace_screen",
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if !cfg.TraceScreen {
		t.Fatal("expected trace_screen to be enabled")
	}
}

func TestParseAcceptsRTTDumpFrequency(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]string{
		"-sn", "uac",
		"-rsa", "127.0.0.1:5060",
		"-rtt_freq", "50",
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.RTTDumpFrequency != 50 {
		t.Fatalf("expected rtt dump frequency 50, got %d", cfg.RTTDumpFrequency)
	}
}

func TestParseAcceptsStatsDumpFrequency(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]string{
		"-sn", "uac",
		"-rsa", "127.0.0.1:5060",
		"-fd", "2",
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.StatsDumpPeriod != 2*time.Second {
		t.Fatalf("expected stats dump period 2s, got %v", cfg.StatsDumpPeriod)
	}
}

func TestParseAcceptsTimeoutGlobal(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]string{
		"-sn", "uac",
		"-rsa", "127.0.0.1:5060",
		"-timeout_global", "3",
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.GlobalTimeout != 3*time.Second {
		t.Fatalf("expected timeout_global 3s, got %v", cfg.GlobalTimeout)
	}
}

func TestParseRejectsInvalidStatsDumpFrequency(t *testing.T) {
	t.Parallel()

	if _, err := Parse([]string{
		"-sn", "uac",
		"-rsa", "127.0.0.1:5060",
		"-fd", "0",
	}); err == nil {
		t.Fatal("expected Parse() to reject fd <= 0")
	}
}

func TestParseRejectsInvalidRTTDumpFrequency(t *testing.T) {
	t.Parallel()

	if _, err := Parse([]string{
		"-sn", "uac",
		"-rsa", "127.0.0.1:5060",
		"-rtt_freq", "0",
	}); err == nil {
		t.Fatal("expected Parse() to reject rtt_freq <= 0")
	}
}

func TestParseRejectsInvalidTimeoutGlobal(t *testing.T) {
	t.Parallel()

	if _, err := Parse([]string{
		"-sn", "uac",
		"-rsa", "127.0.0.1:5060",
		"-timeout_global", "-1",
	}); err == nil {
		t.Fatal("expected Parse() to reject timeout_global < 0")
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

func TestParseAcceptsBaseCSeq(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]string{
		"-sn", "uac",
		"-rsa", "127.0.0.1:5060",
		"-base_cseq", "42",
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.BaseCSeq != 42 {
		t.Fatalf("expected base_cseq 42, got %d", cfg.BaseCSeq)
	}
}

func TestParseAcceptsRatePeriod(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]string{
		"-sn", "uac",
		"-rsa", "127.0.0.1:5060",
		"-r", "7",
		"-rp", "2000",
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if math.Abs(cfg.Rate-3.5) > 0.000001 {
		t.Fatalf("expected effective cps 3.5, got %f", cfg.Rate)
	}
}

func TestParseAcceptsRateScale(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]string{
		"-sn", "uac",
		"-rsa", "127.0.0.1:5060",
		"-rate_scale", "2.5",
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.RateScale != 2.5 {
		t.Fatalf("expected rate_scale 2.5, got %f", cfg.RateScale)
	}
}

func TestParseAcceptsRateRampFlags(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]string{
		"-sn", "uac",
		"-rsa", "127.0.0.1:5060",
		"-rate_increase", "0.5",
		"-rate_interval", "250",
		"-rate_max", "30",
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.RateIncrease != 0.5 {
		t.Fatalf("expected rate_increase 0.5, got %f", cfg.RateIncrease)
	}
	if cfg.RateIncreaseStep != 250*time.Millisecond {
		t.Fatalf("expected rate_interval 250ms, got %v", cfg.RateIncreaseStep)
	}
	if cfg.RateMax != 30 {
		t.Fatalf("expected rate_max 30, got %f", cfg.RateMax)
	}
}

func TestParseAcceptsReconnectFlags(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]string{
		"-sn", "uac",
		"-rsa", "127.0.0.1:5060",
		"-max_reconnect", "3",
		"-reconnect_sleep", "150",
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.MaxReconnect != 3 {
		t.Fatalf("expected max_reconnect 3, got %d", cfg.MaxReconnect)
	}
	if cfg.ReconnectSleep != 150*time.Millisecond {
		t.Fatalf("expected reconnect_sleep 150ms, got %v", cfg.ReconnectSleep)
	}
}

func TestParseAcceptsReconnectClose(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]string{
		"-sn", "uac",
		"-rsa", "127.0.0.1:5060",
		"-reconnect_close",
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if !cfg.ReconnectClose {
		t.Fatal("expected reconnect_close to be enabled")
	}
}

func TestParseAcceptsMaxSocket(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]string{
		"-sn", "uac",
		"-rsa", "127.0.0.1:5060",
		"-max_socket", "32",
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.MaxSockets != 32 {
		t.Fatalf("expected max_socket 32, got %d", cfg.MaxSockets)
	}
}

func TestParseAcceptsInfIndex(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]string{
		"-infindex", "users.csv", "0",
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.InfIndexFile != "users.csv" {
		t.Fatalf("expected infindex file users.csv, got %q", cfg.InfIndexFile)
	}
	if cfg.InfIndexField != 0 {
		t.Fatalf("expected infindex field 0, got %d", cfg.InfIndexField)
	}
}

func TestParseAcceptsInfIndexCompactSyntax(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]string{
		"-infindex", "users.csv,1",
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.InfIndexFile != "users.csv" {
		t.Fatalf("expected infindex file users.csv, got %q", cfg.InfIndexFile)
	}
	if cfg.InfIndexField != 1 {
		t.Fatalf("expected infindex field 1, got %d", cfg.InfIndexField)
	}
}

func TestParseRejectsInvalidBaseCSeq(t *testing.T) {
	t.Parallel()

	if _, err := Parse([]string{
		"-sn", "uac",
		"-rsa", "127.0.0.1:5060",
		"-base_cseq", "0",
	}); err == nil {
		t.Fatal("expected Parse() to reject base_cseq <= 0")
	}
}

func TestParseRejectsInvalidRatePeriod(t *testing.T) {
	t.Parallel()

	if _, err := Parse([]string{
		"-sn", "uac",
		"-rsa", "127.0.0.1:5060",
		"-rp", "0",
	}); err == nil {
		t.Fatal("expected Parse() to reject rp <= 0")
	}
}

func TestParseRejectsInvalidRateScale(t *testing.T) {
	t.Parallel()

	if _, err := Parse([]string{
		"-sn", "uac",
		"-rsa", "127.0.0.1:5060",
		"-rate_scale", "0",
	}); err == nil {
		t.Fatal("expected Parse() to reject rate_scale <= 0")
	}
}

func TestParseRejectsInvalidRateInterval(t *testing.T) {
	t.Parallel()

	if _, err := Parse([]string{
		"-sn", "uac",
		"-rsa", "127.0.0.1:5060",
		"-rate_interval", "0",
	}); err == nil {
		t.Fatal("expected Parse() to reject rate_interval <= 0")
	}
}

func TestParseRejectsInvalidRateMax(t *testing.T) {
	t.Parallel()

	if _, err := Parse([]string{
		"-sn", "uac",
		"-rsa", "127.0.0.1:5060",
		"-rate_max", "-1",
	}); err == nil {
		t.Fatal("expected Parse() to reject rate_max < 0")
	}
}

func TestParseRejectsInvalidMaxSocket(t *testing.T) {
	t.Parallel()

	if _, err := Parse([]string{
		"-sn", "uac",
		"-rsa", "127.0.0.1:5060",
		"-max_socket", "-1",
	}); err == nil {
		t.Fatal("expected Parse() to reject max_socket < 0")
	}
}

func TestParseRejectsInvalidMaxReconnect(t *testing.T) {
	t.Parallel()

	if _, err := Parse([]string{
		"-sn", "uac",
		"-rsa", "127.0.0.1:5060",
		"-max_reconnect", "-1",
	}); err == nil {
		t.Fatal("expected Parse() to reject max_reconnect < 0")
	}
}

func TestParseRejectsUITransportWithoutInjection(t *testing.T) {
	t.Parallel()

	if _, err := Parse([]string{
		"-sn", "uac",
		"-rsa", "127.0.0.1:5060",
		"-t", "ui",
	}); err == nil {
		t.Fatal("expected Parse() to reject ui transport without inf/ip_field")
	}
}

func TestParseRejectsInfWithoutIPField(t *testing.T) {
	t.Parallel()

	injectionPath := filepath.Join(t.TempDir(), "ips.csv")
	if err := os.WriteFile(injectionPath, []byte("127.0.0.2\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(injection) error = %v", err)
	}
	if _, err := Parse([]string{
		"-sn", "uac",
		"-rsa", "127.0.0.1:5060",
		"-inf", injectionPath,
	}); err == nil {
		t.Fatal("expected Parse() to reject inf without ip_field")
	}
}

func TestParseRejectsIPFieldWithoutInf(t *testing.T) {
	t.Parallel()

	if _, err := Parse([]string{
		"-sn", "uac",
		"-rsa", "127.0.0.1:5060",
		"-ip_field", "0",
	}); err == nil {
		t.Fatal("expected Parse() to reject ip_field without inf")
	}
}

func TestParseRejectsInfOnNonUITransport(t *testing.T) {
	t.Parallel()

	injectionPath := filepath.Join(t.TempDir(), "ips.csv")
	if err := os.WriteFile(injectionPath, []byte("127.0.0.2\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(injection) error = %v", err)
	}
	if _, err := Parse([]string{
		"-sn", "uac",
		"-rsa", "127.0.0.1:5060",
		"-t", "u1",
		"-inf", injectionPath,
		"-ip_field", "0",
	}); err == nil {
		t.Fatal("expected Parse() to reject inf/ip_field when transport is not ui")
	}
}

func TestParseRejectsInvalidIPInInjection(t *testing.T) {
	t.Parallel()

	injectionPath := filepath.Join(t.TempDir(), "ips.csv")
	if err := os.WriteFile(injectionPath, []byte("not-an-ip\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(injection) error = %v", err)
	}
	if _, err := Parse([]string{
		"-sn", "uac",
		"-rsa", "127.0.0.1:5060",
		"-t", "ui",
		"-inf", injectionPath,
		"-ip_field", "0",
	}); err == nil {
		t.Fatal("expected Parse() to reject invalid source IP in inf file")
	} else {
		if !strings.Contains(err.Error(), "inf file") || !strings.Contains(err.Error(), "row 1") || !strings.Contains(err.Error(), "invalid source IP") {
			t.Fatalf("expected detailed inf row error, got %v", err)
		}
	}
}

func TestParseRejectsOutOfRangeIPFieldWithDetailedInfError(t *testing.T) {
	t.Parallel()

	injectionPath := filepath.Join(t.TempDir(), "ips.csv")
	if err := os.WriteFile(injectionPath, []byte("127.0.0.2\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(injection) error = %v", err)
	}
	if _, err := Parse([]string{
		"-sn", "uac",
		"-rsa", "127.0.0.1:5060",
		"-t", "ui",
		"-inf", injectionPath,
		"-ip_field", "1",
	}); err == nil {
		t.Fatal("expected Parse() to reject out-of-range ip_field for inf row")
	} else {
		if !strings.Contains(err.Error(), "inf file") || !strings.Contains(err.Error(), "row 1") || !strings.Contains(err.Error(), "out of range") {
			t.Fatalf("expected detailed ip_field out-of-range error, got %v", err)
		}
	}
}

func TestParseRejectsEmptyInfFile(t *testing.T) {
	t.Parallel()

	injectionPath := filepath.Join(t.TempDir(), "ips.csv")
	if err := os.WriteFile(injectionPath, []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile(injection) error = %v", err)
	}
	if _, err := Parse([]string{
		"-sn", "uac",
		"-rsa", "127.0.0.1:5060",
		"-t", "ui",
		"-inf", injectionPath,
		"-ip_field", "0",
	}); err == nil {
		t.Fatal("expected Parse() to reject empty inf file")
	} else {
		if !strings.Contains(err.Error(), "does not contain any source IP rows") {
			t.Fatalf("expected empty inf file error, got %v", err)
		}
	}
}

func TestParseRejectsEmptySourceIPCell(t *testing.T) {
	t.Parallel()

	injectionPath := filepath.Join(t.TempDir(), "ips.csv")
	if err := os.WriteFile(injectionPath, []byte("   \n"), 0o644); err != nil {
		t.Fatalf("WriteFile(injection) error = %v", err)
	}
	if _, err := Parse([]string{
		"-sn", "uac",
		"-rsa", "127.0.0.1:5060",
		"-t", "ui",
		"-inf", injectionPath,
		"-ip_field", "0",
	}); err == nil {
		t.Fatal("expected Parse() to reject empty source IP cell")
	} else {
		if !strings.Contains(err.Error(), "empty source IP") {
			t.Fatalf("expected empty source IP error, got %v", err)
		}
	}
}

func TestParseAcceptsIPv6SourceIPsFromInf(t *testing.T) {
	t.Parallel()

	injectionPath := filepath.Join(t.TempDir(), "ips.csv")
	if err := os.WriteFile(injectionPath, []byte("2001:db8::10\n2001:db8::11\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(injection) error = %v", err)
	}
	cfg, err := Parse([]string{
		"-sn", "uac",
		"-rsa", "127.0.0.1:5060",
		"-t", "ui",
		"-inf", injectionPath,
		"-ip_field", "0",
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(cfg.UISourceIPs) != 2 || cfg.UISourceIPs[0] != "2001:db8::10" || cfg.UISourceIPs[1] != "2001:db8::11" {
		t.Fatalf("unexpected IPv6 ui source IPs: %+v", cfg.UISourceIPs)
	}
}

func TestParseRejectsMalformedCSVInInf(t *testing.T) {
	t.Parallel()

	injectionPath := filepath.Join(t.TempDir(), "ips.csv")
	if err := os.WriteFile(injectionPath, []byte("\"127.0.0.2\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(injection) error = %v", err)
	}
	if _, err := Parse([]string{
		"-sn", "uac",
		"-rsa", "127.0.0.1:5060",
		"-t", "ui",
		"-inf", injectionPath,
		"-ip_field", "0",
	}); err == nil {
		t.Fatal("expected Parse() to reject malformed CSV inf file")
	} else {
		if !strings.Contains(err.Error(), "unable to parse inf file") {
			t.Fatalf("expected CSV parse error, got %v", err)
		}
	}
}

func TestParseRejectsInfIndexWithoutField(t *testing.T) {
	t.Parallel()

	if _, err := Parse([]string{
		"-infindex", "users.csv",
	}); err == nil {
		t.Fatal("expected Parse() to reject infindex without field")
	}
}

func TestParseRejectsInvalidInfIndexField(t *testing.T) {
	t.Parallel()

	if _, err := Parse([]string{
		"-infindex", "users.csv", "-1",
	}); err == nil {
		t.Fatal("expected Parse() to reject infindex field < 0")
	}
}

func TestParseRejectsInvalidReconnectSleep(t *testing.T) {
	t.Parallel()

	if _, err := Parse([]string{
		"-sn", "uac",
		"-rsa", "127.0.0.1:5060",
		"-reconnect_sleep", "-1",
	}); err == nil {
		t.Fatal("expected Parse() to reject reconnect_sleep < 0")
	}
}
