package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

func wantsSubcommandHelp(rest []string) bool {
	for _, a := range rest {
		switch strings.ToLower(strings.TrimSpace(a)) {
		case "-h", "--help", "help":
			return true
		}
	}
	return false
}

func printTUIHelp(w io.Writer) {
	if w == nil {
		w = os.Stdout
	}
	fmt.Fprintln(w, "Gossipper TUI — full-screen launcher / runtime control")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  gossipper tui            start the interactive UI")
	fmt.Fprintln(w, "  gossipper tui -h         this help")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "No extra flags on this subcommand; SIP/load flags belong on the root command or `gossipper -interactive`.")
	fmt.Fprintln(w, "See docs/tui.md")
}

func printCLIHelp(w io.Writer) {
	if w == nil {
		w = os.Stdout
	}
	fmt.Fprintln(w, "Gossipper CLI / shell — line-oriented interactive shell")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  gossipper cli            start the shell (stdin/stdout)")
	fmt.Fprintln(w, "  gossipper shell          same as cli")
	fmt.Fprintln(w, "  gossipper shell -h       this help")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "No extra flags on this subcommand; scenario flags are configured inside the shell.")
	fmt.Fprintln(w, "See docs/interactive-shell.md")
}
