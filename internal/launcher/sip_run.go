package launcher

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/qxip/gossipper/internal/cli"
	"github.com/qxip/gossipper/internal/engine"
)

// RunSIPScenario runs the SIP scenario engine (UAC/UAS or XML) until ctx is cancelled or the scenario completes.
// It prints the final summary line to stdout. Optional periodic stats (stderr) and SIGUSR1 screen dumps follow cli.Config.
func RunSIPScenario(ctx context.Context, cfg cli.Config) error {
	prepared, err := Prepare(cfg)
	if err != nil {
		return err
	}

	// runCtx is cancelled as soon as app.Run returns so SIGUSR1 / stat-print
	// goroutines can exit before we wait on them. Parent ctx still drives app.Run
	// (SIGINT) and is inherited by runCtx.
	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()

	app := engine.New(prepared.EngineConfig)
	screenDumpSignals := make(chan os.Signal, 1)
	signal.Notify(screenDumpSignals, syscall.SIGUSR1)
	defer signal.Stop(screenDumpSignals)
	dumpDone := make(chan struct{})
	go func() {
		defer close(dumpDone)
		for {
			select {
			case <-runCtx.Done():
				return
			case <-screenDumpSignals:
				app.DumpScreenSnapshot()
			}
		}
	}()

	var statWG sync.WaitGroup
	if prepared.CLIConfig.StatPrintPeriod > 0 {
		statWG.Add(1)
		go func() {
			defer statWG.Done()
			ticker := time.NewTicker(prepared.CLIConfig.StatPrintPeriod)
			defer ticker.Stop()
			for {
				select {
				case <-runCtx.Done():
					return
				case <-ticker.C:
					fmt.Fprintln(os.Stderr, SummaryLine(app.Stats().Snapshot()))
				}
			}
		}()
	}

	runErr := app.Run(ctx)
	cancelRun()
	statWG.Wait()
	<-dumpDone

	if runErr != nil && !errors.Is(runErr, context.Canceled) && !(errors.Is(runErr, context.DeadlineExceeded) && cfg.GlobalTimeout > 0) {
		return runErr
	}

	if prepared.CLIConfig.SummaryJSON != "" {
		if err := app.Stats().WriteJSON(prepared.CLIConfig.SummaryJSON); err != nil {
			return err
		}
	}

	summary := app.Stats().Snapshot()
	fmt.Println(SummaryLine(summary))
	return nil
}
