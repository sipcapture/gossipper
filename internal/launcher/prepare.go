package launcher

import (
	"fmt"

	"github.com/adubovikov/gossipper/internal/cli"
	"github.com/adubovikov/gossipper/internal/engine"
	"github.com/adubovikov/gossipper/internal/scenario"
	"github.com/adubovikov/gossipper/internal/stats"
)

type Prepared struct {
	CLIConfig    cli.Config
	Scenario     scenario.Scenario
	EngineConfig engine.Config
}

func PrepareFromArgs(args []string) (Prepared, error) {
	cfg, err := cli.Parse(args)
	if err != nil {
		return Prepared{}, err
	}
	return Prepare(cfg)
}

func Prepare(cfg cli.Config) (Prepared, error) {
	var (
		sc  scenario.Scenario
		err error
	)
	if cfg.ScenarioFile != "" {
		sc, err = scenario.ParseFile(cfg.ScenarioFile)
	} else {
		sc, err = scenario.LoadNamed(cfg.ScenarioName)
	}
	if err != nil {
		return Prepared{}, err
	}
	if err := NormalizeTransport(&cfg, sc); err != nil {
		return Prepared{}, err
	}
	if err := Validate3PCCRole(cfg, sc); err != nil {
		return Prepared{}, err
	}

	engCfg := engine.Config{
		Scenario:         sc,
		Transport:        cfg.Transport,
		LocalIP:          cfg.LocalIP,
		LocalPort:        cfg.LocalPort,
		RemoteHost:       cfg.RemoteHost,
		RemotePort:       cfg.RemotePort,
		Service:          cfg.Service,
		AuthUsername:     cfg.AuthUsername,
		AuthPassword:     cfg.AuthPassword,
		Rate:             cfg.Rate,
		RateScale:        cfg.RateScale,
		RateIncrease:     cfg.RateIncrease,
		RateIncreaseStep: cfg.RateIncreaseStep,
		RateMax:          cfg.RateMax,
		MaxReconnect:     cfg.MaxReconnect,
		ReconnectSleep:   cfg.ReconnectSleep,
		ReconnectClose:   cfg.ReconnectClose,
		BaseCSeq:         cfg.BaseCSeq,
		TotalCalls:       cfg.TotalCalls,
		MaxConcurrent:    cfg.MaxConcurrent,
		MaxSockets:       cfg.MaxSockets,
		Users:            cfg.Users,
		DefaultPause:     cfg.DefaultPause,
		DefaultRecvTO:    cfg.DefaultRecvTO,
		TraceMessages:    cfg.TraceMessages,
		TraceShortMsg:    cfg.TraceShortMsg,
		TraceCounts:      cfg.TraceCounts,
		MessageFile:      cfg.MessageFile,
		TraceErrors:      cfg.TraceErrors,
		ErrorFile:        cfg.ErrorFile,
		TraceErrorCodes:  cfg.TraceErrorCodes,
		TraceLogs:        cfg.TraceLogs,
		LogFile:          cfg.LogFile,
		TraceStats:       cfg.TraceStats,
		TraceRTT:         cfg.TraceRTT,
		TraceScreen:      cfg.TraceScreen,
		StatsDumpPeriod:  cfg.StatsDumpPeriod,
		RTTDumpFrequency: cfg.RTTDumpFrequency,
		ScreenFile:       cfg.ScreenFile,
		HEPAddr:          cfg.HEPAddr,
		HEPCaptureID:     cfg.HEPCaptureID,
		HEPPassword:      cfg.HEPPassword,
		TLSCertFile:      cfg.TLSCertFile,
		TLSKeyFile:       cfg.TLSKeyFile,
		TLSCAFile:        cfg.TLSCAFile,
		TLSSkipVerify:    cfg.TLSSkipVerify,
		CommandName:      cfg.CommandName,
		CommandPeers:     cfg.CommandPeers,
		UISourceIPs:      append([]string(nil), cfg.UISourceIPs...),
	}

	return Prepared{
		CLIConfig:    cfg,
		Scenario:     sc,
		EngineConfig: engCfg,
	}, nil
}

func NormalizeTransport(cfg *cli.Config, sc scenario.Scenario) error {
	switch cfg.Transport {
	case "s1":
		if sc.Mode != scenario.ModeServer {
			return fmt.Errorf("transport s1 requires a server scenario")
		}
		cfg.Transport = "u1"
	case "sn":
		if sc.Mode != scenario.ModeServer {
			return fmt.Errorf("transport sn requires a server scenario")
		}
		cfg.Transport = "un"
	case "ui":
		if sc.Mode != scenario.ModeClient {
			return fmt.Errorf("transport ui requires a client scenario")
		}
		if len(cfg.UISourceIPs) == 0 || cfg.InjectionFile == "" || cfg.IPField < 0 {
			return fmt.Errorf("transport ui requires inf and ip_field with at least one source IP")
		}
	}
	return nil
}

func Validate3PCCRole(cfg cli.Config, sc scenario.Scenario) error {
	if cfg.CommandRole != "slave" {
		return nil
	}
	seenRecvCmd := false
	for _, cmd := range append(append([]scenario.Command{}, sc.InitCommands...), sc.Commands...) {
		switch cmd.Type {
		case scenario.CommandRecvCmd:
			seenRecvCmd = true
		case scenario.CommandSendCmd:
			if !seenRecvCmd {
				return fmt.Errorf("slave 3PCC scenario must receive via recvCmd before the first sendCmd")
			}
		}
	}
	return nil
}

func SummaryLine(summary stats.Summary) string {
	return fmt.Sprintf(
		"calls=%d success=%d failed=%d cps=%.2f avg_call=%s avg_invite=%s retransmits=%d timeouts=%d rtp_sent=%d rtp_recv=%d rtcp_sr=%d rtcp_rr=%d rtcp_in=%d",
		summary.TotalCalls,
		summary.SuccessCalls,
		summary.FailedCalls,
		summary.CallsPerSecond,
		summary.AverageCallLatency,
		summary.AverageInviteRTT,
		summary.Retransmits,
		summary.Timeouts,
		summary.Media.RTPPacketsSent,
		summary.Media.RTPPacketsReceived,
		summary.Media.RTCPSenderReports,
		summary.Media.RTCPReceiverReports,
		summary.Media.RTCPPacketsReceived,
	)
}
