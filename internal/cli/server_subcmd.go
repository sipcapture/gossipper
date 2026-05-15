package cli

// InternalServerSubcommandArgv is injected before [Parse] when the user runs
// `gossipper server …`. It is not a user-facing flag; do not rely on it in scripts.
const InternalServerSubcommandArgv = "--gossipper-server-subcommand"

// ServerSubcommandPrependsFlag rewrites argv after the `server` CLI token for
// [cli.Parse]. If the remainder already contains [InternalServerSubcommandArgv],
// it is returned unchanged; otherwise [InternalServerSubcommandArgv] is prepended
// (same runtime defaults as before the standalone -server flag was removed).
func ServerSubcommandPrependsFlag(rest []string) []string {
	if serverRestImpliesServerMode(rest) {
		return rest
	}
	out := make([]string, 0, len(rest)+1)
	out = append(out, InternalServerSubcommandArgv)
	out = append(out, rest...)
	return out
}

func serverRestImpliesServerMode(argv []string) bool {
	for _, a := range argv {
		if a == InternalServerSubcommandArgv {
			return true
		}
	}
	return false
}
