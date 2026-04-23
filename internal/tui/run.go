package tui

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"unsafe"
	"time"

	"github.com/qxip/gossipper/internal/cli"
	"github.com/qxip/gossipper/internal/engine"
	"github.com/qxip/gossipper/internal/launcher"
	"github.com/qxip/gossipper/internal/scenario"
	"github.com/qxip/gossipper/internal/stats"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type runtimeState struct {
	mu          sync.Mutex
	screen      string
	running     bool
	status      string
	eng         *engine.Engine
	cancel      context.CancelFunc
	prepared    launcher.Prepared
	currentProf profile
	lastSummary stats.Summary
	prevSummary *stats.Summary
	intervalCPS float64
	lastErr     error
}

func Run() error {
	defaults := cli.DefaultConfig()
	app := tview.NewApplication()
	pages := tview.NewPages()
	state := &runtimeState{screen: "config"}
	profiles := loadProfiles()
	filteredProfiles := filterProfiles(profiles, "client")

	configStatus := tview.NewTextView().
		SetDynamicColors(true).
		SetText("Configure a launch profile and press Start.")

	dashboard := tview.NewTextView().
		SetDynamicColors(true).
		SetWrap(false)

	modeField := styleDropdown(tview.NewDropDown().SetLabel("Mode: "))
	profileField := styleDropdown(tview.NewDropDown().SetLabel("Profile: "))
	transportField := styleDropdown(tview.NewDropDown().SetLabel("Transport: "))
	remoteField := tview.NewInputField().SetLabel("Remote addr: ").SetText("")
	localIPField := tview.NewInputField().SetLabel("Local IP: ").SetText(defaults.LocalIP)
	localPortField := tview.NewInputField().SetLabel("Local port: ").SetText(strconv.Itoa(defaults.LocalPort))
	serviceField := tview.NewInputField().SetLabel("Service: ").SetText(defaults.Service)
	cpsField := tview.NewInputField().SetLabel("CPS: ").SetText(strconv.FormatFloat(defaults.Rate, 'f', -1, 64))
	rateScaleField := tview.NewInputField().SetLabel("Rate scale: ").SetText(strconv.FormatFloat(defaults.RateScale, 'f', -1, 64))
	callsField := tview.NewInputField().SetLabel("Calls (0=∞): ").SetText(strconv.Itoa(defaults.TotalCalls))
	concurrencyField := tview.NewInputField().SetLabel("Max concurrent: ").SetText(strconv.Itoa(defaults.MaxConcurrent))
	usersField := tview.NewInputField().SetLabel("Users: ").SetText(strconv.Itoa(defaults.Users))
	authUserField := tview.NewInputField().SetLabel("Auth user: ").SetText(defaults.AuthUsername)
	authPassField := tview.NewInputField().SetLabel("Auth pass: ").SetText(defaults.AuthPassword)
	customXMLField := tview.NewInputField().SetLabel("Custom XML: ").SetText("")
	hepAddrField := tview.NewInputField().SetLabel("HEP addr: ").SetText("")
	sendMediaReportField := tview.NewCheckbox().SetLabel("send_media_report ")
	hepRawRTCPField := tview.NewCheckbox().SetLabel("hep_raw_rtcp ").SetChecked(true)
	infField := tview.NewInputField().SetLabel("UI inf CSV: ").SetText("")
	ipFieldField := tview.NewInputField().SetLabel("UI ip_field: ").SetText("0")
	traceStatField := tview.NewCheckbox().SetLabel("trace_stat ")
	traceRTTField := tview.NewCheckbox().SetLabel("trace_rtt ")
	traceMsgField := tview.NewCheckbox().SetLabel("trace_msg ")
	traceErrField := tview.NewCheckbox().SetLabel("trace_err ")

	transports := []string{"u1", "un", "ui", "t1", "tn", "l1", "ln", "s1", "sn"}
	modeField.SetOptions([]string{"client", "server"}, func(text string, _ int) {
		filteredProfiles = filterProfiles(profiles, text)
		profileField.SetOptions(profileLabels(filteredProfiles), nil)
		profileField.SetCurrentOption(0)
		transportField.SetCurrentOption(defaultTransportIndex(text, transports))
	})
	modeField.SetCurrentOption(0)

	profileField.SetOptions(profileLabels(filteredProfiles), nil)
	profileField.SetCurrentOption(0)
	transportField.SetOptions(transports, nil)
	transportField.SetCurrentOption(defaultTransportIndex("client", transports))

	form := tview.NewForm().
		AddFormItem(modeField).
		AddFormItem(profileField).
		AddFormItem(transportField).
		AddFormItem(remoteField).
		AddFormItem(localIPField).
		AddFormItem(localPortField).
		AddFormItem(serviceField).
		AddFormItem(cpsField).
		AddFormItem(rateScaleField).
		AddFormItem(callsField).
		AddFormItem(concurrencyField).
		AddFormItem(usersField).
		AddFormItem(authUserField).
		AddFormItem(authPassField).
		AddFormItem(customXMLField).
		AddFormItem(hepAddrField).
		AddFormItem(sendMediaReportField).
		AddFormItem(hepRawRTCPField).
		AddFormItem(infField).
		AddFormItem(ipFieldField).
		AddFormItem(traceStatField).
		AddFormItem(traceRTTField).
		AddFormItem(traceMsgField).
		AddFormItem(traceErrField)
	form.AddButton("Start", func() {
		commitDropdownSelection(modeField, 0)
		commitDropdownSelection(transportField, defaultTransportIndex(currentText(modeField), transports))
		commitDropdownSelection(profileField, 0)

		selectedProfile, err := currentProfile(profileField, filteredProfiles)
		if err != nil {
			configStatus.SetText(fmt.Sprintf("[red]%v", err))
			return
		}
		args, err := buildArgs(
			selectedProfile,
			currentText(modeField),
			currentText(transportField),
			remoteField.GetText(),
			localIPField.GetText(),
			localPortField.GetText(),
			serviceField.GetText(),
			cpsField.GetText(),
			rateScaleField.GetText(),
			callsField.GetText(),
			concurrencyField.GetText(),
			usersField.GetText(),
			authUserField.GetText(),
			authPassField.GetText(),
			customXMLField.GetText(),
			hepAddrField.GetText(),
			sendMediaReportField.IsChecked(),
			hepRawRTCPField.IsChecked(),
			infField.GetText(),
			ipFieldField.GetText(),
			traceStatField.IsChecked(),
			traceRTTField.IsChecked(),
			traceMsgField.IsChecked(),
			traceErrField.IsChecked(),
		)
		if err != nil {
			configStatus.SetText(fmt.Sprintf("[red]%v", err))
			return
		}

		prepared, err := launcher.PrepareFromArgs(args)
		if err != nil {
			configStatus.SetText(fmt.Sprintf("[red]%v", err))
			return
		}

		eng := engine.New(prepared.EngineConfig)
		ctx, cancel := context.WithCancel(context.Background())
		state.start(prepared, selectedProfile, eng, cancel)
		dashboard.SetText(state.renderDashboard())
		pages.SwitchToPage("dashboard")
		app.SetFocus(dashboard)

		go func() {
			err := eng.Run(ctx)
			state.finish(err)
			app.QueueUpdateDraw(func() {
				dashboard.SetText(state.renderDashboard())
			})
		}()
	})
	form.AddButton("Quit", func() {
		app.Stop()
	})
	form.SetBorder(true).SetTitle(" Gossipper TUI ").SetTitleAlign(tview.AlignLeft)

	configLayout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(tview.NewTextView().SetText("[::b]Gossipper interactive launcher"), 1, 0, false).
		AddItem(configStatus, 2, 0, false).
		AddItem(form, 0, 1, true)

	dashboard.SetBorder(true).SetTitle(" Runtime Stats ").SetTitleAlign(tview.AlignLeft)
	dashboardLayout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(dashboard, 0, 1, true)

	pages.AddPage("config", configLayout, true, true)
	pages.AddPage("dashboard", dashboardLayout, true, false)

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
					if state.screen == "dashboard" {
						dashboard.SetText(state.renderDashboard())
					}
				})
			}
		}
	}()

	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if state.currentScreen() != "dashboard" {
			if event.Key() == tcell.KeyEscape {
				app.Stop()
				return nil
			}
			return event
		}

		switch event.Key() {
		case tcell.KeyEscape:
			if !state.isRunning() {
				state.setScreen("config")
				pages.SwitchToPage("config")
				app.SetFocus(form)
				return nil
			}
			return nil
		}

		switch event.Rune() {
		case '+', '=':
			if eng := state.engine(); eng != nil && state.prepared.Scenario.Mode != scenario.ModeServer {
				rate := bumpRate(eng.Rate(), state.rateScale(), +1)
				eng.SetRate(rate)
				state.setStatus(fmt.Sprintf("Target CPS set to %.2f", rate))
				dashboard.SetText(state.renderDashboard())
				return nil
			}
		case '-':
			if eng := state.engine(); eng != nil && state.prepared.Scenario.Mode != scenario.ModeServer {
				rate := bumpRate(eng.Rate(), state.rateScale(), -1)
				eng.SetRate(rate)
				state.setStatus(fmt.Sprintf("Target CPS set to %.2f", rate))
				dashboard.SetText(state.renderDashboard())
				return nil
			}
		case '*':
			if eng := state.engine(); eng != nil && state.prepared.Scenario.Mode != scenario.ModeServer {
				rate := bumpRate(eng.Rate(), state.rateScale(), +10)
				eng.SetRate(rate)
				state.setStatus(fmt.Sprintf("Target CPS set to %.2f", rate))
				dashboard.SetText(state.renderDashboard())
				return nil
			}
		case '/':
			if eng := state.engine(); eng != nil && state.prepared.Scenario.Mode != scenario.ModeServer {
				rate := bumpRate(eng.Rate(), state.rateScale(), -10)
				eng.SetRate(rate)
				state.setStatus(fmt.Sprintf("Target CPS set to %.2f", rate))
				dashboard.SetText(state.renderDashboard())
				return nil
			}
		case 'p', 'P':
			if eng := state.engine(); eng != nil && state.prepared.Scenario.Mode != scenario.ModeServer {
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
				if eng := state.engine(); eng != nil && state.prepared.Scenario.Mode != scenario.ModeServer {
					eng.StopScheduling()
					state.setStatus("Stopping after active calls drain")
				} else if cancel := state.cancelFunc(); cancel != nil {
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
	if err := app.SetRoot(pages, true).Run(); err != nil {
		return err
	}
	return nil
}

func (s *runtimeState) start(prepared launcher.Prepared, prof profile, eng *engine.Engine, cancel context.CancelFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.screen = "dashboard"
	s.running = true
	s.status = "Run started"
	s.eng = eng
	s.cancel = cancel
	s.prepared = prepared
	s.currentProf = prof
	s.prevSummary = nil
	s.intervalCPS = 0
	s.lastErr = nil
	s.lastSummary = stats.Summary{}
}

func (s *runtimeState) finish(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.eng != nil {
		s.lastSummary = s.eng.Stats().Snapshot()
	}
	s.running = false
	s.lastErr = err
	if err != nil && !errors.Is(err, context.Canceled) {
		s.status = "Run failed: " + err.Error()
	} else if errors.Is(err, context.Canceled) {
		s.status = "Run cancelled"
	} else {
		s.status = "Run finished"
	}
}

func (s *runtimeState) refresh() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.eng == nil {
		return
	}
	summary := s.eng.Stats().Snapshot()
	if s.prevSummary != nil {
		interval := summary.FinishedAt.Sub(s.prevSummary.FinishedAt)
		if interval > 0 {
			s.intervalCPS = float64(summary.TotalCalls-s.prevSummary.TotalCalls) / interval.Seconds()
		}
	}
	s.lastSummary = summary
	copySummary := summary
	s.prevSummary = &copySummary
}

func (s *runtimeState) renderDashboard() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	var (
		targetRate float64
		paused     bool
	)
	if s.eng != nil {
		targetRate = s.eng.Rate()
		paused = s.eng.Paused()
	}

	summary := s.lastSummary
	cancelled := failureCount(summary, "cancelled")
	unexpected := failureCount(summary, "unexpected_sip")
	transport := failureCount(summary, "transport_error")
	parse := failureCount(summary, "parse_error")
	scenarioErr := failureCount(summary, "scenario_error")

	stateLabel := "idle"
	switch {
	case s.running && paused:
		stateLabel = "paused"
	case s.running:
		stateLabel = "running"
	case s.lastErr != nil:
		stateLabel = "stopped"
	case s.eng != nil:
		stateLabel = "finished"
	}

	return fmt.Sprintf(`[yellow]Profile:[-] %s
[yellow]Mode:[-] %s   [yellow]Transport:[-] %s   [yellow]State:[-] %s
[yellow]Target CPS:[-] %.2f   [yellow]Measured CPS(avg):[-] %.2f   [yellow]Measured CPS(1s):[-] %.2f

[yellow]Calls:[-] total=%d active=%d success=%d failed=%d
[yellow]Latency:[-] avg_call=%s avg_invite=%s
[yellow]Failures:[-] timeout=%d cancelled=%d unexpected=%d transport=%d parse=%d scenario=%d
[yellow]Media:[-] rtp_sent=%d rtp_recv=%d rtcp_sr=%d rtcp_rr=%d rtcp_in=%d

[yellow]Status:[-] %s

[green]Keys[-]: +/- step CPS, */ change CPS by 10x step, p pause/resume, q stop run, Esc back after finish`,
		s.currentProf.Name,
		s.prepared.Scenario.Mode,
		s.prepared.EngineConfig.Transport,
		stateLabel,
		targetRate,
		summary.CallsPerSecond,
		s.intervalCPS,
		summary.TotalCalls,
		summary.ActiveCalls,
		summary.SuccessCalls,
		summary.FailedCalls,
		summary.AverageCallLatency,
		summary.AverageInviteRTT,
		summary.Timeouts,
		cancelled,
		unexpected,
		transport,
		parse,
		scenarioErr,
		summary.Media.RTPPacketsSent,
		summary.Media.RTPPacketsReceived,
		summary.Media.RTCPSenderReports,
		summary.Media.RTCPReceiverReports,
		summary.Media.RTCPPacketsReceived,
		s.status,
	)
}

func (s *runtimeState) currentScreen() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.screen
}

func (s *runtimeState) setScreen(screen string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.screen = screen
}

func (s *runtimeState) isRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

func (s *runtimeState) engine() *engine.Engine {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.eng
}

func (s *runtimeState) cancelFunc() context.CancelFunc {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cancel
}

func (s *runtimeState) setStatus(status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = status
}

func (s *runtimeState) rateScale() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.prepared.EngineConfig.RateScale <= 0 {
		return 1.0
	}
	return s.prepared.EngineConfig.RateScale
}

func buildArgs(
	prof profile,
	mode string,
	transport string,
	remoteAddr string,
	localIP string,
	localPort string,
	service string,
	cps string,
	rateScale string,
	calls string,
	concurrency string,
	users string,
	authUser string,
	authPass string,
	customXML string,
	hepAddr string,
	sendMediaReport bool,
	hepRawRTCP bool,
	infPath string,
	ipField string,
	traceStat bool,
	traceRTT bool,
	traceMsg bool,
	traceErr bool,
) ([]string, error) {
	var args []string

	switch {
	case prof.Custom:
		customXML = strings.TrimSpace(customXML)
		if customXML == "" {
			return nil, fmt.Errorf("custom XML path is required")
		}
		args = append(args, "-sf", customXML)
	case prof.ScenarioFile != "":
		args = append(args, "-sf", prof.ScenarioFile)
	default:
		args = append(args, "-sn", prof.ScenarioName)
	}

	if mode == "server" && strings.HasPrefix(transport, "u") {
		switch transport {
		case "u1":
			transport = "s1"
		case "un":
			transport = "sn"
		}
	}

	args = append(args,
		"-t", transport,
		"-i", strings.TrimSpace(localIP),
		"-p", defaultString(localPort, "0"),
		"-s", defaultString(service, "service"),
		"-r", defaultString(cps, "1"),
		"-rate_scale", defaultString(rateScale, "1"),
		"-m", defaultString(calls, "1"),
		"-l", defaultString(concurrency, "1"),
		"-users", defaultString(users, "1"),
	)

	if remote := strings.TrimSpace(remoteAddr); remote != "" {
		args = append(args, "-rsa", remote)
	}
	if username := strings.TrimSpace(authUser); username != "" {
		args = append(args, "-au", username)
	}
	if authPass != "" {
		args = append(args, "-ap", authPass)
	}
	if hep := strings.TrimSpace(hepAddr); hep != "" {
		args = append(args, "-hep_addr", hep)
		if sendMediaReport {
			args = append(args, "-send_media_report")
		}
		if hepRawRTCP {
			args = append(args, "-hep_raw_rtcp=true")
		} else {
			args = append(args, "-hep_raw_rtcp=false")
		}
	}
	if transport == "ui" {
		infPath = strings.TrimSpace(infPath)
		if infPath == "" {
			return nil, fmt.Errorf("ui transport requires inf CSV path")
		}
		ipField = strings.TrimSpace(ipField)
		if ipField == "" {
			return nil, fmt.Errorf("ui transport requires ip_field")
		}
		if _, err := strconv.Atoi(ipField); err != nil {
			return nil, fmt.Errorf("ui ip_field must be an integer")
		}
		args = append(args, "-inf", infPath, "-ip_field", ipField)
	}
	if traceStat {
		args = append(args, "-trace_stat")
	}
	if traceRTT {
		args = append(args, "-trace_rtt")
	}
	if traceMsg {
		args = append(args, "-trace_msg")
	}
	if traceErr {
		args = append(args, "-trace_err")
	}
	return args, nil
}

func defaultTransportIndex(mode string, options []string) int {
	want := "u1"
	if mode == "server" {
		want = "s1"
	}
	for i, option := range options {
		if option == want {
			return i
		}
	}
	return 0
}

func currentText(dropdown *tview.DropDown) string {
	_, text := dropdown.GetCurrentOption()
	return text
}

func styleDropdown(dropdown *tview.DropDown) *tview.DropDown {
	if dropdown == nil {
		return nil
	}

	dropdown.SetListStyles(
		tcell.StyleDefault.Foreground(tcell.ColorBlack).Background(tcell.ColorWhite),
		tcell.StyleDefault.Foreground(tcell.ColorBlack).Background(tcell.ColorGreen),
	)
	dropdown.SetFieldStyle(
		tcell.StyleDefault.Foreground(tcell.ColorBlack).Background(tcell.ColorWhite),
	)
	dropdown.SetFocusedStyle(
		tcell.StyleDefault.Foreground(tcell.ColorBlack).Background(tcell.ColorGreen),
	)

	return dropdown
}

func commitDropdownSelection(dropdown *tview.DropDown, fallback int) {
	if dropdown == nil {
		return
	}

	idx, ok := dropdownListCurrentItem(dropdown)
	if !ok {
		return
	}

	count := dropdown.GetOptionCount()
	if idx < 0 || idx >= count {
		if fallback >= 0 && fallback < count {
			dropdown.SetCurrentOption(fallback)
		}
		return
	}

	if current, _ := dropdown.GetCurrentOption(); current == idx {
		return
	}
	dropdown.SetCurrentOption(idx)
}

func dropdownListCurrentItem(dropdown *tview.DropDown) (int, bool) {
	if dropdown == nil {
		return -1, false
	}

	value := reflect.ValueOf(dropdown)
	if value.Kind() != reflect.Ptr || value.IsNil() {
		return -1, false
	}

	elem := value.Elem()
	if !elem.IsValid() {
		return -1, false
	}

	listField := elem.FieldByName("list")
	if !listField.IsValid() || !listField.CanAddr() {
		return -1, false
	}

	listValue := reflect.NewAt(listField.Type(), unsafe.Pointer(listField.UnsafeAddr())).Elem()
	list, ok := listValue.Interface().(*tview.List)
	if !ok || list == nil {
		return -1, false
	}

	return list.GetCurrentItem(), true
}

func currentProfile(dropdown *tview.DropDown, profiles []profile) (profile, error) {
	idx, _ := dropdown.GetCurrentOption()
	if idx < 0 || idx >= len(profiles) {
		return profile{}, fmt.Errorf("selected profile index is out of range: %d", idx)
	}
	return profiles[idx], nil
}

func defaultString(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func bumpRate(current, scale float64, steps int) float64 {
	if scale <= 0 {
		scale = 1.0
	}
	next := current + (scale * float64(steps))
	if next < 0.1 {
		return 0.1
	}
	return next
}

func failureCount(summary stats.Summary, name string) int {
	if summary.FailureClasses == nil {
		return 0
	}
	return summary.FailureClasses[name]
}
