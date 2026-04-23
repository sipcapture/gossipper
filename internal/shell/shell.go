package shell

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/qxip/gossipper/internal/cli"
	"github.com/qxip/gossipper/internal/launcher"
)

// Run starts an interactive line-oriented shell on in/out/errOut.
func Run(in io.Reader, out, errOut io.Writer) error {
	r := bufio.NewReader(in)
	sess := newSession()
	fmt.Fprintln(out, "gossipper shell — type help, hint, or wizard. Ctrl+D or quit to exit.")
	for {
		fmt.Fprint(out, "gossipper> ")
		line, err := r.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				fmt.Fprintln(out, "bye")
				return nil
			}
			return err
		}
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) == 0 {
			continue
		}
		cmd := strings.ToLower(parts[0])
		switch cmd {
		case "quit", "exit", "q":
			fmt.Fprintln(out, "bye")
			return nil
		case "help", "?":
			printHelp(out)
		case "hint", "hints":
			WriteHints(out, sess)
		case "show":
			sess.Show(out)
		case "reset":
			sess.Reset()
			fmt.Fprintln(out, "session cleared")
		case "set":
			if len(parts) < 2 {
				fmt.Fprintln(errOut, "usage: set <flag> [value]")
				printSetCheatsheet(out)
				continue
			}
			flag := parts[1]
			val := strings.TrimSpace(strings.Join(parts[2:], " "))
			if err := sess.Set(flag, val); err != nil {
				fmt.Fprintln(errOut, err)
				continue
			}
			fmt.Fprintf(out, "ok: -%s\n", canonicalFlag(flag))
		case "unset":
			if len(parts) < 2 {
				fmt.Fprintln(errOut, "usage: unset <flag>")
				continue
			}
			sess.Unset(parts[1])
			fmt.Fprintln(out, "ok")
		case "parse", "check":
			cfg, err := cli.Parse(sess.Argv())
			if err != nil {
				fmt.Fprintf(errOut, "parse: %v\n", err)
				fmt.Fprintln(out, "Tip: run hint for suggestions.")
				continue
			}
			if cfg.RTPSend {
				fmt.Fprintln(errOut, "parse: rtp_send mode is not supported from shell")
				continue
			}
			if _, err := launcher.Prepare(cfg); err != nil {
				fmt.Fprintf(errOut, "prepare: %v\n", err)
				fmt.Fprintln(out, "Tip: run hint for suggestions.")
				continue
			}
			fmt.Fprintln(out, "parse ok")
		case "run":
			cfg, err := cli.Parse(sess.Argv())
			if err != nil {
				fmt.Fprintf(errOut, "parse: %v\n", err)
				fmt.Fprintln(out, "Tip: run hint for suggestions.")
				continue
			}
			if cfg.RTPSend {
				fmt.Fprintln(errOut, "run: rtp_send mode is not supported from shell")
				continue
			}
			baseCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			ctx := baseCtx
			cancelTimeout := func() {}
			if cfg.GlobalTimeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(baseCtx, cfg.GlobalTimeout)
				cancelTimeout = cancel
			}
			fmt.Fprintln(out, "running… (Ctrl+C to stop)")
			runErr := launcher.RunSIPScenario(ctx, cfg)
			stop()
			cancelTimeout()
			if runErr != nil && !errors.Is(runErr, context.Canceled) && !(errors.Is(runErr, context.DeadlineExceeded) && cfg.GlobalTimeout > 0) {
				fmt.Fprintf(errOut, "run: %v\n", runErr)
			} else {
				fmt.Fprintln(out, "run finished")
			}
		case "wizard":
			if err := RunWizard(r, out, errOut, sess); err != nil {
				fmt.Fprintf(errOut, "wizard: %v\n", err)
			}
		default:
			fmt.Fprintf(errOut, "unknown command %q (try help)\n", cmd)
		}
	}
}

func printHelp(out io.Writer) {
	fmt.Fprintln(out, "Commands:")
	fmt.Fprintln(out, "  wizard          — guided prompts for a minimal UAC or UAS profile")
	fmt.Fprintln(out, "  hint            — suggest what to set or fix for the current session")
	fmt.Fprintln(out, "  set <k> [v]     — set flag (readable: destination_host + destination_port, builtin_scenario, local_bind_ip, calls_per_second, …; short rsa/sn/i/p/m/r still work)")
	fmt.Fprintln(out, "  set             — no args: short flag cheatsheet")
	fmt.Fprintln(out, "  unset <k>       — remove a flag from the session")
	fmt.Fprintln(out, "  show            — print accumulated argv and values")
	fmt.Fprintln(out, "  parse|check     — validate flags with cli.Parse + launcher.Prepare")
	fmt.Fprintln(out, "  run             — run the SIP scenario (same engine as direct CLI)")
	fmt.Fprintln(out, "  reset           — clear all session flags")
	fmt.Fprintln(out, "  help            — this text")
	fmt.Fprintln(out, "  quit|exit       — leave the shell")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Boolean flags: set trace_msg | set trace_msg false")
	fmt.Fprintln(out, "Lines starting with # are comments.")
}
