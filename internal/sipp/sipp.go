// Package sipp implements the `gossipper sipp` entry point: SIPp-style
// command-line scenario flags only (parity with running a scenario from SIPp's CLI).
package sipp

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// ErrRootSubcommandAfterSipp means argv used gossipper-only subcommands after `sipp`.
var ErrRootSubcommandAfterSipp = errors.New("gossipper sipp is only for SIPp-style scenario flags")

// Run handles argv after the first `sipp` token. It prints usage when there are no
// arguments or when the user asks for help. It rejects gossipper-only subcommands
// (tui, shell, server, …): those must be invoked as `gossipper <subcommand>` without `sipp`.
func Run(rest []string, forward func([]string) error) error {
	for len(rest) > 0 && strings.EqualFold(strings.TrimSpace(rest[0]), "sipp") {
		rest = rest[1:]
	}
	if len(rest) == 0 {
		PrintUsage(os.Stdout)
		return nil
	}
	first := strings.TrimSpace(rest[0])
	lf := strings.ToLower(first)
	switch lf {
	case "-h", "--help", "help":
		// Same flag surface as root `gossipper -h` (full list + preamble).
		return forward(append([]string{"-h"}, rest[1:]...))
	case "-interactive", "--interactive":
		return fmt.Errorf("%w; use `gossipper -interactive` or `gossipper tui` (drop `sipp`)", ErrRootSubcommandAfterSipp)
	}
	if hint := rootOnlyGossipperSubcommand(lf); hint != "" {
		return fmt.Errorf("%w; %q belongs on the root command: `gossipper %s`", ErrRootSubcommandAfterSipp, first, hint)
	}
	return forward(rest)
}

func rootOnlyGossipperSubcommand(lf string) string {
	switch lf {
	case "tui":
		return "tui …"
	case "shell", "cli":
		return lf + " …"
	case "server":
		return "server …"
	case "pcap2scenario":
		return "pcap2scenario …"
	case "report-html":
		return "report-html …"
	case "summary-to-pdf":
		return "summary-to-pdf …"
	case "profile":
		return "profile …"
	default:
		return ""
	}
}

// PrintUsage writes SIPp-oriented help. If w is nil, uses stdout.
func PrintUsage(w io.Writer) {
	if w == nil {
		w = os.Stdout
	}
	fmt.Fprintln(w, "Gossipper — SIPp-style CLI entry (same binary as gossipper).")
	fmt.Fprintln(w, "`gossipper sipp` is only for command-line scenario runs (SIPp-like flags).")
	fmt.Fprintln(w, "Use the root command for everything else, e.g. `gossipper tui`, `gossipper cli`, `gossipper server …`.")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  gossipper sipp [flags...]   SIP/XML scenario load (same flags as `gossipper` scenario mode; see docs/compatibility.md)")
	fmt.Fprintln(w, "  gossipper sipp -h           full flag list (same as `gossipper -h`)")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Documentation:")
	fmt.Fprintln(w, "  docs/gossipper-vs-sipp.md — parity overview")
	fmt.Fprintln(w, "  docs/compatibility.md      — XML, keywords, transports, CLI matrix")
	fmt.Fprintln(w, "  docs/statistics-mapping.md — stats vs SIPp counters")
}
