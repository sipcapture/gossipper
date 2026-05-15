package cli

import (
	"fmt"
	"io"
	"os"
)

// PrintSIPPEntrySummary prints the short SIPp-entry blurb (no main flag list).
func PrintSIPPEntrySummary(w io.Writer) {
	if w == nil {
		w = os.Stdout
	}
	fmt.Fprintln(w, "Gossipper — SIPp-style CLI entry (same binary as gossipper).")
	fmt.Fprintln(w, "`gossipper sipp` is only for command-line scenario runs (SIPp-like flags).")
	fmt.Fprintln(w, "Use the root command for everything else, e.g. `gossipper tui`, `gossipper cli`, `gossipper server …`.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  gossipper sipp [flags…]   SIP/XML scenario load (see docs/compatibility.md)")
	fmt.Fprintln(w, "  gossipper sipp -h         scenario flags grouped as HEP, OTLP, PPROF, SIPP (see below)")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Documentation:")
	fmt.Fprintln(w, "  docs/gossipper-vs-sipp.md — parity overview")
	fmt.Fprintln(w, "  docs/compatibility.md      — XML, keywords, transports, CLI matrix")
	fmt.Fprintln(w, "  docs/statistics-mapping.md — stats vs SIPp counters")
}
