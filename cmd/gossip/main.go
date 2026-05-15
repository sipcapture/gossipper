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
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strings"
	"syscall"

	"github.com/sipcapture/gossipper/internal/cli"
	"github.com/sipcapture/gossipper/internal/launcher"
	"github.com/sipcapture/gossipper/internal/pcap2scenario"
	"github.com/sipcapture/gossipper/internal/pdf"
	"github.com/sipcapture/gossipper/internal/reporthtml"
	"github.com/sipcapture/gossipper/internal/shell"
	"github.com/sipcapture/gossipper/internal/sipp"
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
	if len(args) > 0 && strings.EqualFold(args[0], "sipp") {
		return sipp.Run(args[1:], runMain)
	}
	return runMain(args)
}

func runMain(args []string) error {
	if len(args) > 0 && args[0] == "auth" {
		return runAuthCommand(args[1:])
	}
	if len(args) > 0 && args[0] == "tui" {
		if wantsSubcommandHelp(args[1:]) {
			printTUIHelp(os.Stdout)
			return nil
		}
		return tui.Run()
	}
	if len(args) > 0 && args[0] == "pcap2scenario" {
		return runPCAP2Scenario(args[1:])
	}
	if len(args) > 0 && args[0] == "report-html" {
		return runReportHTML(args[1:])
	}
	if len(args) > 0 && args[0] == "summary-to-pdf" {
		return runSummaryToPDF(args[1:])
	}
	if len(args) > 0 && args[0] == "profile" {
		return runProfileHelp(args[1:])
	}
	if len(args) > 0 && (args[0] == "shell" || args[0] == "cli") {
		if wantsSubcommandHelp(args[1:]) {
			printCLIHelp(os.Stdout)
			return nil
		}
		return shell.Run(os.Stdin, os.Stdout, os.Stderr)
	}
	serverViaSubcommand := false
	if len(args) > 0 && args[0] == "server" {
		serverViaSubcommand = true
		args = cli.ServerSubcommandPrependsFlag(args[1:])
	}
	if cli.CurrentHelpContext() != cli.HelpContextSipp {
		if serverViaSubcommand || cli.ArgsImplyServerMode(args) {
			cli.SetHelpContext(cli.HelpContextServer)
		}
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

	if interactive && cfg.ServerMode {
		return fmt.Errorf("-server cannot be combined with -interactive")
	}

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
	fs.SetOutput(os.Stdout)
	outDir := fs.String("out", ".", "output directory for generated scenario files")
	sipPort := fs.Int("sip-port", 0, "SIP signalling port (0 = auto-detect)")
	pcapLink := fs.String("pcap-link", "", "datalink for decode: auto (default), ethernet, linux_sll, linux_sll2, raw, ...")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "gossipper pcap2scenario — PCAP to SIP XML scenarios\n\n")
		fmt.Fprintf(fs.Output(), "Usage:\n  gossipper pcap2scenario <file.pcap> [options]\n\nOptions:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() < 1 {
		fs.Usage()
		return fmt.Errorf("usage: gossipper pcap2scenario <file.pcap> [-out <dir>] [-sip-port <port>] [-pcap-link <layer>]")
	}

	return pcap2scenario.Run(fs.Arg(0), *outDir, *sipPort, *pcapLink)
}

// runProfileHelp implements `gossipper profile` (JSON run-profile flags summary only).
func runProfileHelp(args []string) error {
	for _, a := range args {
		switch strings.ToLower(strings.TrimSpace(a)) {
		case "-h", "--help", "help":
			cli.PrintRunProfileHelp(os.Stdout)
			return nil
		}
	}
	if len(args) == 0 {
		cli.PrintRunProfileHelp(os.Stdout)
		return nil
	}
	return fmt.Errorf("gossipper profile: unexpected arguments %v; use gossipper profile -h", args)
}

func runReportHTML(args []string) error {
	fs := flag.NewFlagSet("report-html", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	inPath := fs.String("in", "", "input summary JSON (from gossipper -summary_json)")
	outPath := fs.String("out", "", "output HTML file path")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "usage: gossipper report-html -in summary.json -out report.html\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
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

func runSummaryToPDF(args []string) error {
	fs := flag.NewFlagSet("summary-to-pdf", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	inPath := fs.String("in", "", "input HTML file (e.g. from gossipper -summary_html)")
	outPath := fs.String("out", "", "output PDF path")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "usage: gossipper summary-to-pdf -in report.html -out report.pdf\n")
		fmt.Fprintf(fs.Output(), "Tries embedded chromedp when built with -tags pdf; otherwise uses Chromium/Chrome in PATH (--print-to-pdf).\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if strings.TrimSpace(*inPath) == "" || strings.TrimSpace(*outPath) == "" {
		fs.Usage()
		return fmt.Errorf("summary-to-pdf: -in and -out are required")
	}
	absIn, err := filepath.Abs(strings.TrimSpace(*inPath))
	if err != nil {
		return fmt.Errorf("summary-to-pdf: -in path: %w", err)
	}
	absOut, err := filepath.Abs(strings.TrimSpace(*outPath))
	if err != nil {
		return fmt.Errorf("summary-to-pdf: -out path: %w", err)
	}
	if err := pdf.TryRenderHTMLFileToPDF(absIn, absOut); err == nil {
		return nil
	}
	if !errors.Is(err, pdf.ErrBuiltWithoutPDFTag) {
		fmt.Fprintf(os.Stderr, "summary-to-pdf: embedded renderer: %v (trying external Chromium)\n", err)
	}
	candidates := []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable"}
	var chrome string
	for _, name := range candidates {
		p, err := exec.LookPath(name)
		if err == nil {
			chrome = p
			break
		}
	}
	if chrome == "" {
		return fmt.Errorf("summary-to-pdf: no chromium/google-chrome found in PATH")
	}
	tmpDir, err := os.MkdirTemp("", "gossipper-pdf-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)
	fileURL := "file://" + filepath.ToSlash(absIn)
	cmd := exec.Command(chrome,
		"--headless=new",
		"--disable-gpu",
		"--no-sandbox",
		"--user-data-dir="+tmpDir,
		"--print-to-pdf="+absOut,
		fileURL,
	)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("summary-to-pdf: %w", err)
	}
	return nil
}
