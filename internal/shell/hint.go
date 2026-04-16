package shell

import (
	"fmt"
	"io"
	"strings"

	"github.com/qxip/gossipper/internal/cli"
	"github.com/qxip/gossipper/internal/launcher"
	"github.com/qxip/gossipper/internal/scenario"
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
		lines = append(lines, "Session is empty. Quick start: wizard   or at least: set sn uac + set rsa IP:5060 + set i LOCAL_IP")
	} else {
		if mode == scenario.ModeClient {
			if cfg.RemoteHost == "" {
				lines = append(lines, "UAC needs a remote peer:  set rsa HOST:PORT   (where to send INVITE)")
			}
			if cfg.LocalIP == "0.0.0.0" || cfg.LocalIP == "::" {
				lines = append(lines, "Prefer an explicit local IP in Via/Contact:  set i THIS_HOST_IP   (otherwise a UAS may send responses to the wrong place because of Via)")
			}
		}
		if mode == scenario.ModeServer {
			if cfg.LocalIP == "0.0.0.0" || cfg.LocalIP == "::" {
				lines = append(lines, "UAS: bind to a real routable IP:  set i ADDRESS   (not only 0.0.0.0 in Via)")
			}
			t := strings.ToLower(strings.TrimSpace(cfg.Transport))
			if t != "s1" && t != "sn" {
				lines = append(lines, "For incoming UDP as a server, consider:  set t s1  or  set t sn")
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
	fmt.Fprintln(out, "Common flags (cheatsheet):")
	fmt.Fprintln(out, "  set sn uac|uas          — built-in scenario name")
	fmt.Fprintln(out, "  set rsa HOST:PORT     — remote SIP peer (UAC); alias: remote")
	fmt.Fprintln(out, "  set i IP              — local IP in SIP; alias: bind")
	fmt.Fprintln(out, "  set p 5060            — local UDP/TCP port")
	fmt.Fprintln(out, "  set t u1|s1|sn|…      — transport (UAS: often s1/sn)")
	fmt.Fprintln(out, "  set m N               — total calls (UAS: how many to accept before exit)")
	fmt.Fprintln(out, "  set r R               — calls per second (UAC)")
	fmt.Fprintln(out, "  set stat_period 5s    — periodic stats line to stderr")
	fmt.Fprintln(out, "  set trace_msg         — SIP trace (off: set trace_msg false)")
	fmt.Fprintln(out, "Full flag list: gossipper -h. Smarter review:  hint")
}
