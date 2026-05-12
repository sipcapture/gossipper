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
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strings"
	"syscall"

	"github.com/sipcapture/gossipper/internal/cli"
	"github.com/sipcapture/gossipper/internal/launcher"
	"github.com/sipcapture/gossipper/internal/pcap2scenario"
	"github.com/sipcapture/gossipper/internal/reporthtml"
	"github.com/sipcapture/gossipper/internal/reportpdf"
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
	if len(args) > 0 && args[0] == "report-pdf" {
		return runReportPDF(args[1:])
	}
	if len(args) > 0 && args[0] == "mic-rtp" {
		return runMicRTP(args[1:])
	}
	if len(args) > 0 && (args[0] == "shell" || args[0] == "cli") {
		return shell.Run(os.Stdin, os.Stdout, os.Stderr)
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

func runReportPDF(args []string) error {
	fs := flag.NewFlagSet("report-pdf", flag.ContinueOnError)
	inPath := fs.String("in", "", "input .html or summary .json")
	outPath := fs.String("out", "", "output .pdf path")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "usage: gossipper report-pdf -in report.html|summary.json -out report.pdf\n")
		fmt.Fprintf(fs.Output(), "Requires Chromium or Google Chrome in PATH (headless --print-to-pdf).\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	in := strings.TrimSpace(*inPath)
	out := strings.TrimSpace(*outPath)
	if in == "" || out == "" {
		fs.Usage()
		return fmt.Errorf("report-pdf: -in and -out are required")
	}
	switch reportpdf.InputKind(in) {
	case "html":
		return reportpdf.FromHTMLFile(in, out)
	case "json":
		return reportpdf.FromSummaryJSON(in, out)
	default:
		return fmt.Errorf("report-pdf: -in must have extension .html, .htm, or .json (got %s)", filepath.Ext(in))
	}
}

func runMicRTP(args []string) error {
	fs := flag.NewFlagSet("mic-rtp", flag.ContinueOnError)
	addr := fs.String("addr", "", "RTP destination host:port (required)")
	codec := fs.String("codec", "PCMU/8000", "codec e.g. PCMU/8000 or PCMA/8000")
	localIP := fs.String("local_ip", "", "optional local bind IP")
	rtpPT := fs.Int("rtp_pt", 0, "override RTP payload type (0 = use codec default)")
	freqMs := fs.Int("freq_ms", 0, "packet interval in ms (0 = codec default)")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "usage: gossipper mic-rtp -addr HOST:PORT [-codec PCMU/8000] [-local_ip IP] [-rtp_pt N] [-freq_ms MS]\n")
		fmt.Fprintf(fs.Output(), "Reads mono s16le PCM from stdin at the codec sample rate (e.g. 8000 Hz).\n")
		fmt.Fprintf(fs.Output(), "Example: parec --format=s16le --rate=8000 --channels=1 | gossipper mic-rtp -addr 127.0.0.1:5004\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*addr) == "" {
		fs.Usage()
		return fmt.Errorf("mic-rtp: -addr is required")
	}
	baseCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return launcher.RunMicRTP(baseCtx, strings.TrimSpace(*addr), strings.TrimSpace(*codec), strings.TrimSpace(*localIP), *rtpPT, *freqMs)
}
