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
	"github.com/sipcapture/gossipper/internal/settingsauth"
	"github.com/sipcapture/gossipper/internal/stats"
)

func runSIPScenarioMulti(ctx context.Context, cfg cli.Config) error {
	preparedPrimary, err := Prepare(cfg)
	if err != nil {
		return err
	}
	preparedJoin := make([]Prepared, 0, len(cfg.JoinedClients))
	for _, j := range cfg.JoinedClients {
		p, err := Prepare(j.Config)
		if err != nil {
			return err
		}
		preparedJoin = append(preparedJoin, p)
	}

	allPrepared := append([]Prepared{preparedPrimary}, preparedJoin...)
	labels := make([]string, len(allPrepared))
	labels[0] = strings.TrimSpace(cfg.ServerProfileID)
	if labels[0] == "" {
		labels[0] = "primary"
	}
	staticExtraIDs := make([]string, len(cfg.JoinedClients))
	for i := range cfg.JoinedClients {
		labels[i+1] = cfg.JoinedClients[i].ID
		staticExtraIDs[i] = cfg.JoinedClients[i].ID
	}

	var closeLogs []func() error
	for i := range allPrepared {
		logger, closeLog, err := BuildEventLogger(allPrepared[i].CLIConfig, allPrepared[i].Scenario)
		if err != nil {
			for _, cl := range closeLogs {
				_ = cl()
			}
			return err
		}
		closeLogs = append(closeLogs, closeLog)
		allPrepared[i].EngineConfig.Log = logger
	}
	defer func() {
		for j := len(closeLogs) - 1; j >= 0; j-- {
			_ = closeLogs[j]()
		}
	}()

	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()

	var settingsAuth *settingsauth.Auth

	apps := make([]*engine.Engine, len(allPrepared))
	for i := range allPrepared {
		apps[i] = engine.New(allPrepared[i].EngineConfig)
	}

	var loadCoord *LoadCoordinator
	if preparedPrimary.CLIConfig.ApiAddr != "" && preparedPrimary.CLIConfig.ServerMode {
		reserved := []string{labels[0]}
		loadCoord = NewLoadCoordinator(ctx, preparedPrimary.CLIConfig, staticExtraIDs, reserved)
	}

	if preparedPrimary.CLIConfig.ApiAddr != "" {
		var extra []*engine.Engine
		var extraIDs []string
		for i := 1; i < len(apps); i++ {
			extra = append(extra, apps[i])
			extraIDs = append(extraIDs, labels[i])
		}
		apiCfg := api.ServerConfig{
			Engine:         apps[0],
			ExtraEngines:   extra,
			ExtraIDs:       extraIDs,
			StatsPrimaryID: cfg.ServerProfileID,
			CLI:            preparedPrimary.CLIConfig,
			Token:          preparedPrimary.CLIConfig.ApiToken,
			EnableLegacyV1: LegacyV1Enabled(preparedPrimary.CLIConfig),
			ValidateScenario: func(sc scenario.Scenario) error {
				return ValidateScenario(preparedPrimary.CLIConfig, sc)
			},
		}
		if loadCoord != nil {
			apiCfg.LiveExtras = loadCoord.SnapshotDynamic
			apiCfg.AddLoadClient = func(_ context.Context, wantID string, body []byte) (string, error) {
				id, _, err := loadCoord.Add(wantID, body)
				return id, err
			}
			apiCfg.RemoveLoadClient = func(_ context.Context, id string) error {
				return loadCoord.Remove(id)
			}
		}
		if preparedPrimary.CLIConfig.Auth.InternalEnabled() {
			sa, err := settingsauth.Open(preparedPrimary.CLIConfig.Auth.SQLitePath, preparedPrimary.CLIConfig.Auth.JWTSecret)
			if err != nil {
				return fmt.Errorf("settings auth: %w", err)
			}
			settingsAuth = sa
			apiCfg.SettingsAuth = sa
		}
		uiBundle, err := openUIBundle(preparedPrimary.CLIConfig)
		if err != nil {
			return err
		}
		if uiBundle != nil {
			defer func() { _ = uiBundle.Close() }()
			apiCfg.UIStore = uiBundle.Store
			apiCfg.JobsRegistry = uiBundle.Registry
			apiCfg.Version = preparedPrimary.CLIConfig.ToolVersion
		}
		apSrv := api.New(apiCfg)
		warnIfNoV2(preparedPrimary.CLIConfig, apSrv)
		go func() {
			mounted := apiMountSummary(apSrv)
			if api.HasEmbeddedControlUI() {
				fmt.Fprintf(os.Stderr, "api: listening on http://%s/ (Control UI), mounts: %s\n", preparedPrimary.CLIConfig.ApiAddr, mounted)
			} else {
				fmt.Fprintf(os.Stderr, "api: listening on http://%s/, mounts: %s (run `make frontend` to embed Control UI at /)\n", preparedPrimary.CLIConfig.ApiAddr, mounted)
			}
			if err := api.StartListenAndServe(runCtx, preparedPrimary.CLIConfig.ApiAddr, apSrv.Handler()); err != nil {
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
				for _, app := range apps {
					app.DumpScreenSnapshot()
				}
				if loadCoord != nil {
					de, _ := loadCoord.SnapshotDynamic()
					for _, app := range de {
						if app != nil {
							app.DumpScreenSnapshot()
						}
					}
				}
			}
		}
	}()

	var statWG sync.WaitGroup
	if preparedPrimary.CLIConfig.StatPrintPeriod > 0 {
		statWG.Add(1)
		go func() {
			defer statWG.Done()
			ticker := time.NewTicker(preparedPrimary.CLIConfig.StatPrintPeriod)
			defer ticker.Stop()
			for {
				select {
				case <-runCtx.Done():
					return
				case <-ticker.C:
					for i, app := range apps {
						fmt.Fprintf(os.Stderr, "[%s] %s\n", labels[i], SummaryLine(app.Stats().Snapshot()))
					}
					if loadCoord != nil {
						de, did := loadCoord.SnapshotDynamic()
						for i, app := range de {
							if app == nil {
								continue
							}
							id := ""
							if i < len(did) {
								id = did[i]
							}
							fmt.Fprintf(os.Stderr, "[%s] %s\n", id, SummaryLine(app.Stats().Snapshot()))
						}
					}
				}
			}
		}()
	}

	var runWG sync.WaitGroup
	runErrs := make([]error, len(apps))
	for i := range apps {
		i := i
		app := apps[i]
		runWG.Add(1)
		go func() {
			defer runWG.Done()
			runErrs[i] = app.Run(ctx)
		}()
	}
	runWG.Wait()
	if loadCoord != nil {
		loadCoord.Wait()
	}
	cancelRun()
	if settingsAuth != nil {
		_ = settingsAuth.Close()
	}
	statWG.Wait()
	<-dumpDone

	var runErr error
	for _, e := range runErrs {
		if e != nil && !errors.Is(e, context.Canceled) && !(errors.Is(e, context.DeadlineExceeded) && cfg.GlobalTimeout > 0) {
			runErr = e
			break
		}
	}
	if runErr != nil {
		return runErr
	}

	opts := &stats.SummaryWriteOptions{
		ToolVersion: preparedPrimary.CLIConfig.ToolVersion,
		Health: stats.HealthConfig{
			MinSuccessRatio:                preparedPrimary.CLIConfig.HealthMinSuccessRatio,
			MaxFailedCalls:                 preparedPrimary.CLIConfig.HealthMaxFailedCalls,
			MaxTimeouts:                    preparedPrimary.CLIConfig.HealthMaxTimeouts,
			HealthMaxRTCPFractionLost:      preparedPrimary.CLIConfig.HealthMaxRTCPFractionLost,
			HealthMaxRTCPJitterTS:          preparedPrimary.CLIConfig.HealthMaxRTCPJitterTS,
			HealthMinRTPPacketsRecv:        preparedPrimary.CLIConfig.HealthMinRTPPacketsRecv,
			HealthMinRTPPacketsRecvPerCall: preparedPrimary.CLIConfig.HealthMinRTPPacketsRecvPerCall,
		},
	}
	writeJSON := preparedPrimary.CLIConfig.SummaryJSON != ""
	writeHTML := preparedPrimary.CLIConfig.SummaryHTML != ""
	if writeJSON || writeHTML || opts.Health.Active() {
		final := apps[0].Stats().FinalizeSummary(opts.ToolVersion, opts.Health)
		if writeJSON {
			if err := stats.WriteSummaryJSONFile(preparedPrimary.CLIConfig.SummaryJSON, final); err != nil {
				return err
			}
		}
		if writeHTML {
			if err := reporthtml.WriteFile(preparedPrimary.CLIConfig.SummaryHTML, final); err != nil {
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

	for i, app := range apps {
		fmt.Printf("[%s] %s\n", labels[i], SummaryLine(app.Stats().Snapshot()))
	}
	if loadCoord != nil {
		de, did := loadCoord.SnapshotDynamic()
		for i, app := range de {
			if app == nil {
				continue
			}
			id := ""
			if i < len(did) {
				id = did[i]
			}
			fmt.Printf("[%s] %s\n", id, SummaryLine(app.Stats().Snapshot()))
		}
	}
	return nil
}
