package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/sipcapture/gossipper/internal/engine"
	"github.com/sipcapture/gossipper/internal/launcher"
	"github.com/sipcapture/gossipper/internal/scenario"
)

// RunControl launches the runtime dashboard directly from a prepared CLI run,
// bypassing the config form. The caller is responsible for signal handling via ctx.
func RunControl(ctx context.Context, prepared launcher.Prepared) error {
	app := tview.NewApplication()
	state := &runtimeState{screen: "dashboard"}

	dashboard := tview.NewTextView().
		SetDynamicColors(true).
		SetWrap(false)

	prof := controlProfile(prepared)
	eng := engine.New(prepared.EngineConfig)
	engCtx, cancel := context.WithCancel(ctx)
	state.start(prepared, prof, eng, cancel)
	dashboard.SetText(state.renderDashboard())

	dashboard.SetBorder(true).SetTitle(" Runtime Stats ").SetTitleAlign(tview.AlignLeft)
	layout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(dashboard, 0, 1, true)

	stopPolling := make(chan struct{})
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stopPolling:
				return
			case <-ticker.C:
				state.refresh()
				app.QueueUpdateDraw(func() {
					dashboard.SetText(state.renderDashboard())
				})
			}
		}
	}()

	go func() {
		err := eng.Run(engCtx)
		state.finish(err)
		app.QueueUpdateDraw(func() {
			dashboard.SetText(state.renderDashboard())
		})
	}()

	// Stop the app when the parent context is cancelled (e.g. SIGINT).
	go func() {
		<-ctx.Done()
		app.Stop()
	}()

	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEscape:
			if !state.isRunning() {
				app.Stop()
				return nil
			}
			return nil
		}

		switch event.Rune() {
		case '+', '=':
			if eng := state.engine(); eng != nil && prepared.Scenario.Mode != scenario.ModeServer {
				rate := bumpRate(eng.Rate(), state.rateScale(), +1)
				eng.SetRate(rate)
				state.setStatus(fmt.Sprintf("Target CPS set to %.2f", rate))
				dashboard.SetText(state.renderDashboard())
				return nil
			}
		case '-':
			if eng := state.engine(); eng != nil && prepared.Scenario.Mode != scenario.ModeServer {
				rate := bumpRate(eng.Rate(), state.rateScale(), -1)
				eng.SetRate(rate)
				state.setStatus(fmt.Sprintf("Target CPS set to %.2f", rate))
				dashboard.SetText(state.renderDashboard())
				return nil
			}
		case '*':
			if eng := state.engine(); eng != nil && prepared.Scenario.Mode != scenario.ModeServer {
				rate := bumpRate(eng.Rate(), state.rateScale(), +10)
				eng.SetRate(rate)
				state.setStatus(fmt.Sprintf("Target CPS set to %.2f", rate))
				dashboard.SetText(state.renderDashboard())
				return nil
			}
		case '/':
			if eng := state.engine(); eng != nil && prepared.Scenario.Mode != scenario.ModeServer {
				rate := bumpRate(eng.Rate(), state.rateScale(), -10)
				eng.SetRate(rate)
				state.setStatus(fmt.Sprintf("Target CPS set to %.2f", rate))
				dashboard.SetText(state.renderDashboard())
				return nil
			}
		case 'l':
			if eng := state.engine(); eng != nil && prepared.Scenario.Mode != scenario.ModeServer {
				n := eng.SetMaxConcurrent(eng.MaxConcurrent() + 1)
				state.setStatus(fmt.Sprintf("Max simultaneous calls set to %d", n))
				dashboard.SetText(state.renderDashboard())
				return nil
			}
		case 'L':
			if eng := state.engine(); eng != nil && prepared.Scenario.Mode != scenario.ModeServer {
				n := eng.SetMaxConcurrent(eng.MaxConcurrent() - 1)
				state.setStatus(fmt.Sprintf("Max simultaneous calls set to %d", n))
				dashboard.SetText(state.renderDashboard())
				return nil
			}
		case '[':
			if eng := state.engine(); eng != nil && prepared.Scenario.Mode != scenario.ModeServer {
				n := eng.SetMaxConcurrent(eng.MaxConcurrent() - 10)
				state.setStatus(fmt.Sprintf("Max simultaneous calls set to %d", n))
				dashboard.SetText(state.renderDashboard())
				return nil
			}
		case ']':
			if eng := state.engine(); eng != nil && prepared.Scenario.Mode != scenario.ModeServer {
				n := eng.SetMaxConcurrent(eng.MaxConcurrent() + 10)
				state.setStatus(fmt.Sprintf("Max simultaneous calls set to %d", n))
				dashboard.SetText(state.renderDashboard())
				return nil
			}
		case 'p', 'P':
			if eng := state.engine(); eng != nil && prepared.Scenario.Mode != scenario.ModeServer {
				if eng.Paused() {
					eng.Resume()
					state.setStatus("Traffic resumed")
				} else {
					eng.Pause()
					state.setStatus("Traffic paused")
				}
				dashboard.SetText(state.renderDashboard())
				return nil
			}
		case 'q', 'Q':
			if state.isRunning() {
				if eng := state.engine(); eng != nil && prepared.Scenario.Mode != scenario.ModeServer {
					eng.StopScheduling()
					state.setStatus("Stopping after active calls drain")
				} else {
					cancel()
					state.setStatus("Stopping server run")
				}
				dashboard.SetText(state.renderDashboard())
				return nil
			}
			app.Stop()
			return nil
		}
		return event
	})

	defer close(stopPolling)
	return app.SetRoot(layout, true).Run()
}

func controlProfile(prepared launcher.Prepared) profile {
	cfg := prepared.CLIConfig
	name := cfg.ScenarioName
	if name == "" && cfg.ScenarioFile != "" {
		name = strings.TrimSuffix(filepath.Base(cfg.ScenarioFile), ".xml")
	}
	if name == "" {
		name = "cli"
	}
	return profile{Name: name}
}
