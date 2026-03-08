package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/adubovikov/gossipper/internal/cli"
	"github.com/adubovikov/gossipper/internal/engine"
	"github.com/adubovikov/gossipper/internal/scenario"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg, err := cli.Parse(args)
	if err != nil {
		return err
	}

	var sc scenario.Scenario
	if cfg.ScenarioFile != "" {
		sc, err = scenario.ParseFile(cfg.ScenarioFile)
	} else {
		sc, err = scenario.LoadNamed(cfg.ScenarioName)
	}
	if err != nil {
		return err
	}
	if err := normalizeTransport(&cfg, sc); err != nil {
		return err
	}
	if err := validate3PCCRole(cfg, sc); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	app := engine.New(engine.Config{
		Scenario:        sc,
		Transport:       cfg.Transport,
		LocalIP:         cfg.LocalIP,
		LocalPort:       cfg.LocalPort,
		RemoteHost:      cfg.RemoteHost,
		RemotePort:      cfg.RemotePort,
		Service:         cfg.Service,
		AuthUsername:    cfg.AuthUsername,
		AuthPassword:    cfg.AuthPassword,
		Rate:            cfg.Rate,
		TotalCalls:      cfg.TotalCalls,
		MaxConcurrent:   cfg.MaxConcurrent,
		Users:           cfg.Users,
		DefaultPause:    cfg.DefaultPause,
		DefaultRecvTO:   cfg.DefaultRecvTO,
		TraceMessages:   cfg.TraceMessages,
		TraceShortMsg:   cfg.TraceShortMsg,
		MessageFile:     cfg.MessageFile,
		TraceErrors:     cfg.TraceErrors,
		ErrorFile:       cfg.ErrorFile,
		TraceErrorCodes: cfg.TraceErrorCodes,
		TraceLogs:       cfg.TraceLogs,
		LogFile:         cfg.LogFile,
		TraceStats:      cfg.TraceStats,
		TLSCertFile:     cfg.TLSCertFile,
		TLSKeyFile:      cfg.TLSKeyFile,
		TLSCAFile:       cfg.TLSCAFile,
		TLSSkipVerify:   cfg.TLSSkipVerify,
		CommandName:     cfg.CommandName,
		CommandPeers:    cfg.CommandPeers,
	})

	err = app.Run(ctx)
	if err != nil && !errors.Is(err, context.Canceled) {
		return err
	}

	if cfg.SummaryJSON != "" {
		if err := app.Stats().WriteJSON(cfg.SummaryJSON); err != nil {
			return err
		}
	}

	summary := app.Stats().Snapshot()
	fmt.Printf(
		"calls=%d success=%d failed=%d cps=%.2f avg_call=%s avg_invite=%s retransmits=%d timeouts=%d rtp_sent=%d rtp_recv=%d rtcp_sr=%d rtcp_rr=%d rtcp_in=%d\n",
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

	return nil
}

func normalizeTransport(cfg *cli.Config, sc scenario.Scenario) error {
	switch cfg.Transport {
	case "s1":
		if sc.Mode != scenario.ModeServer {
			return errors.New("transport s1 requires a server scenario")
		}
		cfg.Transport = "u1"
	case "sn":
		if sc.Mode != scenario.ModeServer {
			return errors.New("transport sn requires a server scenario")
		}
		cfg.Transport = "un"
	}
	return nil
}

func validate3PCCRole(cfg cli.Config, sc scenario.Scenario) error {
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
				return errors.New("slave 3PCC scenario must receive via recvCmd before the first sendCmd")
			}
		}
	}
	return nil
}
