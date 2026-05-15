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
	fmt.Fprintln(w, "  -config <path>      JSON file with \"aliases\" map (use with -run-alias or -list-aliases)")
	fmt.Fprintln(w, "  -run-alias <name>   apply alias entry from -config before other flags (CLI overrides)")
	fmt.Fprintln(w, "  -list-aliases        print alias names from -config and exit")
	fmt.Fprintln(w, "  -config-server <path>  flat JSON (same keys as one alias, no \"aliases\"); implies -server; mutually exclusive with -config/-run-alias/-list-aliases/-config-client")
	fmt.Fprintln(w, "  -config-client <path>  flat JSON for UAC/load-gen preset; forces non-server after load; mutually exclusive with -config/-run-alias/-list-aliases/-config-server")
	fmt.Fprintln(w, "  See docs/run-profile.md")
}
