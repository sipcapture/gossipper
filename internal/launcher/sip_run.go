package launcher

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/sipcapture/gossipper/internal/api"
	"github.com/sipcapture/gossipper/internal/cli"
	"github.com/sipcapture/gossipper/internal/engine"
	"github.com/sipcapture/gossipper/internal/reporthtml"
	"github.com/sipcapture/gossipper/internal/scenario"
	"github.com/sipcapture/gossipper/internal/stats"
)

// RunSIPScenario runs the SIP scenario engine (UAC/UAS or XML) until ctx is cancelled or the scenario completes.
// It prints the final summary line to stdout. Optional periodic stats (stderr) and SIGUSR1 screen dumps follow cli.Config.
func RunSIPScenario(ctx context.Context, cfg cli.Config) error {
	prepared, err := Prepare(cfg)
	if err != nil {
		return err
	}

	logger, closeLog, err := BuildEventLogger(prepared.CLIConfig, prepared.Scenario)
	if err != nil {
		return err
	}
	defer func() { _ = closeLog() }()
	prepared.EngineConfig.Log = logger

	// runCtx is cancelled as soon as app.Run returns so SIGUSR1 / stat-print
	// goroutines can exit before we wait on them. Parent ctx still drives app.Run
	// (SIGINT) and is inherited by runCtx.
	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()

	app := engine.New(prepared.EngineConfig)
	if prepared.CLIConfig.ApiAddr != "" {
		apSrv := api.New(api.ServerConfig{
			Engine: app,
			CLI:    prepared.CLIConfig,
			Token:  prepared.CLIConfig.ApiToken,
			ValidateScenario: func(sc scenario.Scenario) error {
				return ValidateScenario(prepared.CLIConfig, sc)
			},
		})
		go func() {
			if api.HasEmbeddedControlUI() {
				fmt.Fprintf(os.Stderr, "api: listening on http://%s/ (Control UI) and http://%s/api/v1/health\n", prepared.CLIConfig.ApiAddr, prepared.CLIConfig.ApiAddr)
			} else {
				fmt.Fprintf(os.Stderr, "api: listening on http://%s/api/v1/health (run `make frontend` to embed Control UI at /)\n", prepared.CLIConfig.ApiAddr)
			}
			if err := api.StartListenAndServe(runCtx, prepared.CLIConfig.ApiAddr, apSrv.Handler()); err != nil {
				fmt.Fprintf(os.Stderr, "api: %v\n", err)
			}
		}()
	}
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

	opts := &stats.SummaryWriteOptions{
		ToolVersion: prepared.CLIConfig.ToolVersion,
		Health: stats.HealthConfig{
			MinSuccessRatio:                prepared.CLIConfig.HealthMinSuccessRatio,
			MaxFailedCalls:                 prepared.CLIConfig.HealthMaxFailedCalls,
			MaxTimeouts:                    prepared.CLIConfig.HealthMaxTimeouts,
			HealthMaxRTCPFractionLost:      prepared.CLIConfig.HealthMaxRTCPFractionLost,
			HealthMaxRTCPJitterTS:          prepared.CLIConfig.HealthMaxRTCPJitterTS,
			HealthMinRTPPacketsRecv:        prepared.CLIConfig.HealthMinRTPPacketsRecv,
			HealthMinRTPPacketsRecvPerCall: prepared.CLIConfig.HealthMinRTPPacketsRecvPerCall,
		},
	}
	writeJSON := prepared.CLIConfig.SummaryJSON != ""
	writeHTML := prepared.CLIConfig.SummaryHTML != ""
	if writeJSON || writeHTML || opts.Health.Active() {
		final := app.Stats().FinalizeSummary(opts.ToolVersion, opts.Health)
		if writeJSON {
			if err := stats.WriteSummaryJSONFile(prepared.CLIConfig.SummaryJSON, final); err != nil {
				return err
			}
		}
		if writeHTML {
			if err := reporthtml.WriteFile(prepared.CLIConfig.SummaryHTML, final); err != nil {
				return err
			}
		}
		if opts.Health.Active() && final.Health != nil && !final.Health.Pass {
			msg := strings.Join(final.Health.Reasons, "; ")
			if msg == "" {
				return ErrHealthCheckFailed
			}
			return fmt.Errorf("%w: %s", ErrHealthCheckFailed, msg)
		}
	}

	summary := app.Stats().Snapshot()
	fmt.Println(SummaryLine(summary))
	return nil
}
