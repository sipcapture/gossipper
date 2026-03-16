package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
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

	prepared, err := launcher.Prepare(cfg)
	if err != nil {
		return err
	}

	baseCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx := baseCtx
	cancelTimeout := func() {}
	if prepared.CLIConfig.GlobalTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(baseCtx, prepared.CLIConfig.GlobalTimeout)
		cancelTimeout = cancel
	}
	defer cancelTimeout()

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
	if err != nil && !errors.Is(err, context.Canceled) && !(errors.Is(err, context.DeadlineExceeded) && prepared.CLIConfig.GlobalTimeout > 0) {
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
