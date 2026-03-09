package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/adubovikov/gossipper/internal/engine"
	"github.com/adubovikov/gossipper/internal/launcher"
	"github.com/adubovikov/gossipper/internal/tui"
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

	prepared, err := launcher.PrepareFromArgs(args)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	app := engine.New(prepared.EngineConfig)

	err = app.Run(ctx)
	if err != nil && !errors.Is(err, context.Canceled) {
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
