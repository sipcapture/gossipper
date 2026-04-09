package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"runtime"
	"runtime/pprof"
	"syscall"

	"github.com/qxip/gossipper/internal/cli"
	"github.com/qxip/gossipper/internal/engine"
	"github.com/qxip/gossipper/internal/launcher"
	templ "github.com/qxip/gossipper/internal/template"
	"github.com/qxip/gossipper/internal/tui"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if shouldPrintVersion(args) {
		PrintVersion()
		return nil
	}
	if shouldRunTUI(args) {
		return tui.Run()
	}

	cfg, err := cli.Parse(args)
	if err != nil {
		return err
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

	prepared, err := launcher.Prepare(cfg)
	if err != nil {
		return err
	}

	app := engine.New(prepared.EngineConfig)
	screenDumpSignals := make(chan os.Signal, 1)
	signal.Notify(screenDumpSignals, syscall.SIGUSR1)
	defer signal.Stop(screenDumpSignals)
	dumpDone := make(chan struct{})
	go func() {
		defer close(dumpDone)
		for {
			select {
			case <-ctx.Done():
				return
			case <-screenDumpSignals:
				app.DumpScreenSnapshot()
			}
		}
	}()

	err = app.Run(ctx)
	stop()
	cancelTimeout()
	<-dumpDone
	if err != nil && !errors.Is(err, context.Canceled) && !(errors.Is(err, context.DeadlineExceeded) && cfg.GlobalTimeout > 0) {
		return err
	}

	if prepared.CLIConfig.SummaryJSON != "" {
		if err := app.Stats().WriteJSON(prepared.CLIConfig.SummaryJSON); err != nil {
			return err
		}
	}

	summary := app.Stats().Snapshot()
	fmt.Println(launcher.SummaryLine(summary))

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

func shouldRunTUI(args []string) bool {
	for index, arg := range args {
		switch arg {
		case "-interactive", "--interactive":
			return true
		case "tui":
			return index == 0
		}
	}
	return false
}
