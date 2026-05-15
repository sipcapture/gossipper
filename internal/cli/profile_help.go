package cli

import (
	"fmt"
	"io"
	"os"
)

// PrintRunProfileHelp writes the run-profile / JSON preset flags summary (for gossipper profile -h).
func PrintRunProfileHelp(w io.Writer) {
	if w == nil {
		w = os.Stdout
	}
	fmt.Fprintln(w, "Run profile (optional JSON presets):")
	fmt.Fprintln(w, `  -config <path>      JSON file with "aliases" map (use with -run-alias or -list-aliases) on the root command`)
	fmt.Fprintln(w, "  -run-alias <name>   apply alias entry from -config before other flags (CLI overrides)")
	fmt.Fprintln(w, "  -list-aliases        print alias names from -config and exit")
	fmt.Fprintln(w, "Flat JSON (no \"aliases\" wrapper) is loaded via:")
	fmt.Fprintln(w, "  gossipper server -config <path>   infers management vs load (UAC) preset; removed: -config-server / -config-client")
	fmt.Fprintln(w, "  See docs/run-profile.md")
}
