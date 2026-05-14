package launcher

import (
	"fmt"
	"strings"

	"github.com/sipcapture/gossipper/internal/cli"
	"github.com/sipcapture/gossipper/internal/engine"
	"github.com/sipcapture/gossipper/internal/scenario"
	"github.com/sipcapture/gossipper/internal/stats"
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
	if cfg.InjectionFile != "" && sc.Mode == scenario.ModeServer && cfg.Transport != "ui" {
		return Prepared{}, fmt.Errorf("injection (-inf / -ip_field) is only supported for server transport ui")
	}
	if err := Validate3PCCRole(cfg, sc); err != nil {
		return Prepared{}, err
	}

	totalCalls := cfg.TotalCalls
	unlimited := cfg.TotalCallsSetExplicitly && cfg.TotalCalls == 0
	if sc.Mode == scenario.ModeServer && totalCalls == cli.DefaultTotalCalls && !cfg.TotalCallsSetExplicitly {
		// UAS defaults to 1 call, which rejects all but the first incoming call.
		// For server mode, default to accepting many calls (like SIPp UAS).
		// Do not override when user explicitly passed -m (e.g. -m 1 in tests).
		totalCalls = 10_000_000
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
		TotalCalls:       totalCalls,
		UnlimitedCalls:   unlimited,
		MaxConcurrent:    cfg.MaxConcurrent,
		MaxSockets:       cfg.MaxSockets,
		Users:            cfg.Users,
		DefaultPause:     cfg.DefaultPause,
		DefaultRecvTO:    cfg.DefaultRecvTO,
		RecvBYEFloorTO:   cfg.RecvBYEFloorTO,
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
		HEPRawRTCP:       cfg.HEPRawRTCP,
		HEPHomerLakeRTCP: cfg.HEPHomerLakeRTCP,
		SendMediaReport:  cfg.SendMediaReport,
		TLSCertFile:      cfg.TLSCertFile,
		TLSKeyFile:       cfg.TLSKeyFile,
		TLSCAFile:        cfg.TLSCAFile,
		TLSSkipVerify:    cfg.TLSSkipVerify,
		CommandName:      cfg.CommandName,
		CommandPeers:     cfg.CommandPeers,
		UISourceIPs:      append([]string(nil), cfg.UISourceIPs...),
		InjectionFile:    cfg.InjectionFile,
		Role:             roleFromScenario(sc),
		PCAPLinkLayer:    cfg.PCAPLinkLayer,
		SipFrom:          cfg.SipFrom,
		SipPAI:           cfg.SipPAI,
		SipProvider:      cfg.SipProvider,
		SipExtraHeaders:  append([]string(nil), cfg.SipExtraHeaders...),
		RecordWAVDir:     cfg.RecordWAVDir,
		RecordWAVDuplex:  cfg.RecordWAVDuplex,
		CallRecordsJSONL: cfg.CallRecordsJSONL,
		MediaRejectSRTP:  cfg.MediaRejectSRTP,
		MediaSRTP:        cfg.MediaSRTP,
	}

	return Prepared{
		CLIConfig:    cfg,
		Scenario:     sc,
		EngineConfig: engCfg,
	}, nil
}

func NormalizeTransport(cfg *cli.Config, sc scenario.Scenario) error {
	cfg.Transport = strings.ToLower(strings.TrimSpace(cfg.Transport))

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
	case "sl":
		if sc.Mode != scenario.ModeServer {
			return fmt.Errorf("transport sl requires a server scenario")
		}
		cfg.Transport = "l1"
	case "cl":
		if sc.Mode != scenario.ModeClient {
			return fmt.Errorf("transport cl requires a client scenario")
		}
		cfg.Transport = "l1"
	case "cln":
		if sc.Mode != scenario.ModeClient {
			return fmt.Errorf("transport cln requires a client scenario")
		}
		cfg.Transport = "ln"
	case "ui":
		if cfg.InjectionFile == "" {
			return fmt.Errorf("transport ui requires -inf")
		}
		if len(cfg.UISourceIPs) == 0 {
			return fmt.Errorf("transport ui requires at least one bind/source IP (set -ip_field or -i when omitting -ip_field)")
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
