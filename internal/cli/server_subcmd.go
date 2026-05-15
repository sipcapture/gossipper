package cli

import "strings"

// ServerSubcommandPrependsFlag rewrites argv after the `server` CLI token for
// [cli.Parse]. If the remainder already implies server mode (-server or
// -config-server), it is returned unchanged; otherwise "-server" is prepended
// (same defaults as gossipper -server).
func ServerSubcommandPrependsFlag(rest []string) []string {
	if serverRestImpliesServerMode(rest) {
		return rest
	}
	out := make([]string, 0, len(rest)+1)
	out = append(out, "-server")
	out = append(out, rest...)
	return out
}

func serverRestImpliesServerMode(argv []string) bool {
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		switch {
		case a == "-server" || a == "--server":
			return true
		case a == "-config-server" || a == "--config-server":
			return true
		case strings.HasPrefix(a, "-config-server="):
			return true
		case strings.HasPrefix(a, "--config-server="):
			return true
		}
	}
	return false
}
