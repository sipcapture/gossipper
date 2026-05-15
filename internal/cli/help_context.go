package cli

import "sync/atomic"

// HelpContext selects which flag subset / preamble applies for -h output.
type HelpContext int32

const (
	HelpContextUnset  HelpContext = 0
	HelpContextRoot   HelpContext = 1 // gossipper -h (subcommands + where to read flags; no scenario dump)
	HelpContextServer HelpContext = 2 // gossipper server … or internal server marker
	HelpContextSipp   HelpContext = 3 // gossipper sipp -h (SIPp-style entry)
)

var helpContextAtomic atomic.Int32

func SetHelpContext(c HelpContext) {
	helpContextAtomic.Store(int32(c))
}

func CurrentHelpContext() HelpContext {
	return HelpContext(helpContextAtomic.Load())
}

func resetHelpContext() {
	helpContextAtomic.Store(int32(HelpContextUnset))
}

// ArgsImplyServerMode reports whether argv already selects server mode
// ([InternalServerSubcommandArgv] from gossipper server, or server subcommand help context).
func ArgsImplyServerMode(argv []string) bool {
	return serverRestImpliesServerMode(argv)
}
