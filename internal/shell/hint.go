package shell

import (
	"fmt"
	"io"
	"strings"

	"github.com/sipcapture/gossipper/internal/cli"
	"github.com/sipcapture/gossipper/internal/launcher"
	"github.com/sipcapture/gossipper/internal/scenario"
)

// WriteHints analyzes the current session and prints what to set or fix.
func WriteHints(out io.Writer, sess *Session) {
	argv := sess.Argv()
	cfg, err := cli.Parse(argv)
	if err != nil {
		fmt.Fprintln(out, "Hints (flag parse failed):")
		fmt.Fprintf(out, "  • %v\n", err)
		fmt.Fprintln(out, "  • Run wizard for a guided setup, or help for commands.")
		return
	}

	if cfg.RTPSend {
		fmt.Fprintln(out, "Hints: rtp_send is enabled — from the shell use SIP scenarios only; unset rtp_send or use the normal CLI for RTP sender mode.")
		return
	}

	prepared, prepErr := launcher.Prepare(cfg)
	mode := scenario.ModeClient
	if prepErr == nil {
		mode = prepared.Scenario.Mode
	} else {
		if sc, err2 := loadScenarioForHint(cfg); err2 == nil {
			mode = sc.Mode
		}
	}

	var lines []string

	if len(argv) == 0 {
		lines = append(lines, "Session is empty. Quick start: wizard   or at least: set builtin_scenario uac + set destination HOST:PORT + set local_bind_ip THIS_IP")
	} else {
		if mode == scenario.ModeClient {
			if cfg.RemoteHost == "" {
				lines = append(lines, "UAC needs a remote peer:  set destination HOST:PORT   or  set destination_host HOST + set destination_port PORT")
			}
			if cfg.LocalIP == "0.0.0.0" || cfg.LocalIP == "::" {
				lines = append(lines, "Prefer an explicit local IP in Via/Contact:  set local_bind_ip THIS_HOST_IP   (otherwise a UAS may send responses to the wrong place because of Via)")
			}
		}
		if mode == scenario.ModeServer {
			if cfg.LocalIP == "0.0.0.0" || cfg.LocalIP == "::" {
				lines = append(lines, "UAS: bind to a real routable IP:  set local_bind_ip ADDRESS   (not only 0.0.0.0 in Via)")
			}
			t := strings.ToLower(strings.TrimSpace(cfg.Transport))
			if t != "s1" && t != "sn" && t != "l1" && t != "ln" && t != "sl" && t != "cl" && t != "cln" {
				lines = append(lines, "For incoming UDP as a server, consider:  set transport s1  or  set transport sn (TLS UAS: l1, ln, or sl)")
			}
		}
	}

	if prepErr != nil {
		lines = append(lines, fmt.Sprintf("prepare failed: %v — fix flags or run parse for details", prepErr))
	}

	if len(lines) == 0 && prepErr == nil {
		fmt.Fprintln(out, "No obvious issues. Run parse to double-check, then run.")
		return
	}

	fmt.Fprintln(out, "Hints for the current session:")
	for _, ln := range lines {
		fmt.Fprintf(out, "  • %s\n", ln)
	}
}

func loadScenarioForHint(cfg cli.Config) (scenario.Scenario, error) {
	if cfg.ScenarioFile != "" {
		return scenario.ParseFile(cfg.ScenarioFile)
	}
	return scenario.LoadNamed(cfg.ScenarioName)
}

// printSetCheatsheet lists common flags when the user types "set" alone.
func printSetCheatsheet(out io.Writer) {
	fmt.Fprintln(out, "Common flags (cheatsheet, readable names):")
	fmt.Fprintln(out, "  set builtin_scenario uac|uas|invite_media   — built-in scenario (short: sn)")
	fmt.Fprintln(out, "  set destination HOST:PORT      — remote SIP peer / UAC target (short: rsa)")
	fmt.Fprintln(out, "  set destination_host HOST + set destination_port PORT — same as destination (port defaults to 5060 if omitted)")
	fmt.Fprintln(out, "  set local_bind_ip IP           — local IP in SIP (short: i)")
	fmt.Fprintln(out, "  set listen_port 5060           — local UDP/TCP port (short: p)")
	fmt.Fprintln(out, "  set transport u1|cl|l1|s1|sn|sl|… — transport (short: t; UAC TLS: l1/ln or cl/cln; UAS UDP: s1/sn; UAS TLS: l1/ln/sl)")
	fmt.Fprintln(out, "  set total_calls N              — total calls; 0 = unlimited (short: m)")
	fmt.Fprintln(out, "  set calls_per_second R         — UAC rate (short: r)")
	fmt.Fprintln(out, "  set sip_from \"Name <sip:u@h>\"   — built-in UAC From before ;tag= (CLI -sip_from)")
	fmt.Fprintln(out, "  set sip_pai sip:user@domain    — P-Asserted-Identity value (CLI -sip_pai)")
	fmt.Fprintln(out, "  set sip_provider TOKEN         — X-provider (CLI -sip_provider)")
	fmt.Fprintln(out, "  set stat_period 5s             — periodic stats line to stderr")
	fmt.Fprintln(out, "  set trace_msg                  — SIP trace (off: set trace_msg false)")
	fmt.Fprintln(out, "Full flag list: gossipper sipp -h (server: gossipper server -h). Smarter review:  hint")
}
