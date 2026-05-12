package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"runtime"
	"runtime/pprof"
	"strings"
	"syscall"

	"github.com/sipcapture/gossipper/internal/backfill"
	"github.com/sipcapture/gossipper/internal/cli"
	"github.com/sipcapture/gossipper/internal/launcher"
	"github.com/sipcapture/gossipper/internal/pcap2scenario"
	"github.com/sipcapture/gossipper/internal/reporthtml"
	"github.com/sipcapture/gossipper/internal/shell"
	"github.com/sipcapture/gossipper/internal/stats"
	templ "github.com/sipcapture/gossipper/internal/template"
	"github.com/sipcapture/gossipper/internal/tui"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, launcher.ErrHealthCheckFailed) {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if shouldPrintVersion(args) {
		PrintVersion()
		return nil
	}
	if len(args) > 0 && args[0] == "tui" {
		return tui.Run()
	}
	if len(args) > 0 && args[0] == "pcap2scenario" {
		return runPCAP2Scenario(args[1:])
	}
	if len(args) > 0 && args[0] == "report-html" {
		return runReportHTML(args[1:])
	}
	if len(args) > 0 && (args[0] == "shell" || args[0] == "cli") {
		return shell.Run(os.Stdin, os.Stdout, os.Stderr)
	}
	if len(args) > 0 && args[0] == "backfill" {
		return runBackfill(args[1:])
	}

	interactive := false
	if shouldRunInteractive(args) {
		interactive = true
		args = stripFlag(args, "-interactive", "--interactive")
	}

	cfg, err := cli.Parse(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		if errors.Is(err, cli.ErrListAliases) {
			return nil
		}
		return err
	}
	cfg.ToolVersion = GetShortVersionString()

	if interactive {
		prepared, err := launcher.Prepare(cfg)
		if err != nil {
			return err
		}
		baseCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return tui.RunControl(baseCtx, prepared)
	}
	if cfg.InfIndexFile != "" {
		indexPath, entries, err := templ.GenerateCSVIndex("", cfg.InfIndexFile, cfg.InfIndexField)
		if err != nil {
			return err
		}
		fmt.Printf("infindex generated: %s (entries=%d)\n", indexPath, entries)
		return nil
	}

	// Profiling setup is common to all run modes.
	if cfg.PprofAddr != "" {
		go func() {
			fmt.Fprintf(os.Stderr, "pprof: listening on %s (e.g. go tool pprof http://localhost%s/debug/pprof/profile?seconds=30)\n", cfg.PprofAddr, cfg.PprofAddr)
			_ = http.ListenAndServe(cfg.PprofAddr, nil)
		}()
	}
	if cfg.CPUProfile != "" {
		f, err := os.Create(cfg.CPUProfile)
		if err != nil {
			return fmt.Errorf("cpuprofile: %w", err)
		}
		if err := pprof.StartCPUProfile(f); err != nil {
			f.Close()
			return fmt.Errorf("cpuprofile: %w", err)
		}
		defer func() {
			pprof.StopCPUProfile()
			_ = f.Close()
		}()
	}
	if cfg.MemProfile != "" {
		defer func() {
			f, err := os.Create(cfg.MemProfile)
			if err != nil {
				fmt.Fprintf(os.Stderr, "memprofile: %v\n", err)
				return
			}
			defer f.Close()
			runtime.GC()
			if err := pprof.WriteHeapProfile(f); err != nil {
				fmt.Fprintf(os.Stderr, "memprofile: %v\n", err)
			}
		}()
	}

	baseCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx := baseCtx
	cancelTimeout := func() {}
	if cfg.GlobalTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(baseCtx, cfg.GlobalTimeout)
		cancelTimeout = cancel
	}
	defer cancelTimeout()

	// Standalone RTP sender — bypasses SIP scenario engine entirely.
	if cfg.RTPSend {
		err := launcher.RunRTPSender(ctx, cfg)
		stop()
		cancelTimeout()
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return nil
	}

	err = launcher.RunSIPScenario(ctx, cfg)
	stop()
	cancelTimeout()
	if err != nil && !errors.Is(err, context.Canceled) && !(errors.Is(err, context.DeadlineExceeded) && cfg.GlobalTimeout > 0) {
		return err
	}
	return nil
}

func shouldPrintVersion(args []string) bool {
	for _, arg := range args {
		switch arg {
		case "-version", "--version":
			return true
		}
	}
	return false
}

func shouldRunInteractive(args []string) bool {
	for _, arg := range args {
		if arg == "-interactive" || arg == "--interactive" {
			return true
		}
	}
	return false
}

func stripFlag(args []string, flags ...string) []string {
	result := make([]string, 0, len(args))
	for _, arg := range args {
		skip := false
		for _, f := range flags {
			if arg == f {
				skip = true
				break
			}
		}
		if !skip {
			result = append(result, arg)
		}
	}
	return result
}

// runPCAP2Scenario implements the `gossipper pcap2scenario` sub-command.
func runPCAP2Scenario(args []string) error {
	fs := flag.NewFlagSet("pcap2scenario", flag.ContinueOnError)
	outDir := fs.String("out", ".", "output directory for generated scenario files")
	sipPort := fs.Int("sip-port", 0, "SIP signalling port (0 = auto-detect)")
	pcapLink := fs.String("pcap-link", "", "datalink for decode: auto (default), ethernet, linux_sll, linux_sll2, raw, ...")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: gossipper pcap2scenario <file.pcap> [-out <dir>] [-sip-port <port>] [-pcap-link <layer>]")
	}

	return pcap2scenario.Run(fs.Arg(0), *outDir, *sipPort, *pcapLink)
}

func runReportHTML(args []string) error {
	fs := flag.NewFlagSet("report-html", flag.ContinueOnError)
	inPath := fs.String("in", "", "input summary JSON (from gossipper -summary_json)")
	outPath := fs.String("out", "", "output HTML file path")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "usage: gossipper report-html -in summary.json -out report.html\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*inPath) == "" || strings.TrimSpace(*outPath) == "" {
		fs.Usage()
		return fmt.Errorf("report-html: -in and -out are required")
	}
	raw, err := os.ReadFile(*inPath)
	if err != nil {
		return fmt.Errorf("report-html: read -in: %w", err)
	}
	var s stats.Summary
	if err := json.Unmarshal(raw, &s); err != nil {
		return fmt.Errorf("report-html: parse JSON: %w", err)
	}
	if err := reporthtml.WriteFile(strings.TrimSpace(*outPath), s); err != nil {
		return fmt.Errorf("report-html: write -out: %w", err)
	}
	return nil
}

func runBackfill(args []string) error {
	fs := flag.NewFlagSet("backfill", flag.ContinueOnError)
	cfg := backfill.DefaultConfig()

	durationStr := fs.String("duration", "24h", "how far back to generate data (e.g. 30d, 7d12h, 2h30m)")
	cps := fs.Float64("cps", cfg.CPS, "simulated calls per second")
	metricsInterval := fs.String("metrics_interval", "60s", "interval between aggregate metric snapshots")
	hepAddr := fs.String("hep_addr", "", "HEP collector address (host:port)")
	hepCaptureID := fs.Uint("hep_capture_id", 9999, "HEP capture agent ID")
	hepPassword := fs.String("hep_password", "", "HEP authentication key")
	sbcLogFile := fs.String("sbc_log_file", "", "path for SBC CDR log output (JSON-Lines)")
	sbcMetricsFile := fs.String("sbc_metrics_file", "", "path for SBC metrics output (JSON-Lines)")
	srcIP := fs.String("src_ip", cfg.SrcIP, "source IP for synthetic SIP messages")
	dstIP := fs.String("dst_ip", cfg.DstIP, "destination IP for synthetic SIP messages")
	srcPort := fs.Int("src_port", cfg.SrcPort, "source port")
	dstPort := fs.Int("dst_port", cfg.DstPort, "destination port")
	callDurMin := fs.String("call_duration_min", "10s", "minimum simulated call duration")
	callDurMax := fs.String("call_duration_max", "120s", "maximum simulated call duration")
	failRatio := fs.Float64("fail_ratio", cfg.FailRatio, "fraction of calls that fail (0.0-1.0)")
	noProgress := fs.Bool("no_progress", false, "suppress progress output")

	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: gossipper backfill [OPTIONS]\n\n")
		fmt.Fprintf(fs.Output(), "Generate historical SIP call data (HEP packets, SBC logs, metrics)\n")
		fmt.Fprintf(fs.Output(), "by walking time backwards from now. No real SIP traffic is sent.\n\n")
		fmt.Fprintf(fs.Output(), "Examples:\n")
		fmt.Fprintf(fs.Output(), "  gossipper backfill -duration 30d -cps 2 -hep_addr 127.0.0.1:9060\n")
		fmt.Fprintf(fs.Output(), "  gossipper backfill -duration 7d -cps 5 -sbc_log_file calls.jsonl -sbc_metrics_file metrics.jsonl\n\n")
		fmt.Fprintf(fs.Output(), "Options:\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return err
	}

	dur, err := backfill.ParseDuration(*durationStr)
	if err != nil {
		return fmt.Errorf("backfill: invalid -duration: %w", err)
	}
	cfg.Duration = dur
	cfg.CPS = *cps

	if mi, err := backfill.ParseDuration(*metricsInterval); err == nil {
		cfg.MetricsInterval = mi
	}
	cfg.HEPAddr = *hepAddr
	cfg.HEPCaptureID = uint32(*hepCaptureID)
	cfg.HEPPassword = *hepPassword
	cfg.SBCLogFile = *sbcLogFile
	cfg.SBCMetricsFile = *sbcMetricsFile
	cfg.SrcIP = *srcIP
	cfg.DstIP = *dstIP
	cfg.SrcPort = *srcPort
	cfg.DstPort = *dstPort
	cfg.FailRatio = *failRatio
	cfg.Progress = !*noProgress

	if cdMin, err := backfill.ParseDuration(*callDurMin); err == nil {
		cfg.CallDurationMin = cdMin
	}
	if cdMax, err := backfill.ParseDuration(*callDurMax); err == nil {
		cfg.CallDurationMax = cdMax
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	_, err = backfill.Run(ctx, cfg)
	return err
}
