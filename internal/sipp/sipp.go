// Package sipp implements the `gossipper sipp` entry point: SIPp-oriented CLI surface
// on top of the same engine, shell, and tooling as the root command.
package sipp

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// Run handles argv after the `sipp` token. It prints usage when there are no
// arguments or when the user asks for help; otherwise it forwards to the
// main gossipper dispatcher (same behavior as `gossipper <args>`).
func Run(rest []string, forward func([]string) error) error {
	if len(rest) == 0 {
		PrintUsage(os.Stdout)
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(rest[0])) {
	case "-h", "--help", "help":
		PrintUsage(os.Stdout)
		return nil
	}
	return forward(rest)
}

// PrintUsage writes SIPp-oriented help. If w is nil, uses stdout.
func PrintUsage(w io.Writer) {
	if w == nil {
		w = os.Stdout
	}
	fmt.Fprintln(w, "Gossipper SIPp-style entry (same binary as gossipper).")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  gossipper sipp [flags...]          run SIP/XML scenario (same as gossipper without 'sipp')")
	fmt.Fprintln(w, "  gossipper sipp tui                 interactive launcher / presets")
	fmt.Fprintln(w, "  gossipper sipp shell|cli           line shell (set, run, wizard, …)")
	fmt.Fprintln(w, "  gossipper sipp -interactive        control UI for a prepared run")
	fmt.Fprintln(w, "  gossipper sipp pcap2scenario ...   PCAP → scenario helper")
	fmt.Fprintln(w, "  gossipper sipp report-html ...     summary JSON → HTML")
	fmt.Fprintln(w, "  gossipper sipp summary-to-pdf ...  HTML → PDF")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Documentation:")
	fmt.Fprintln(w, "  docs/gossipper-vs-sipp.md — parity overview")
	fmt.Fprintln(w, "  docs/compatibility.md      — XML, keywords, transports, CLI matrix")
	fmt.Fprintln(w, "  docs/statistics-mapping.md — stats vs SIPp counters")
}
