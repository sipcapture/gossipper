package cli

import (
	"flag"
	"fmt"
	"io"
	"reflect"
	"strings"
)

var rootHelpOmitFlags = map[string]struct{}{
	"api_addr":  {},
	"api_token": {},
}

// sippHelpOmitFlags hides Gossipper-only knobs from the SIPp entry help surface.
var sippHelpOmitFlags = map[string]struct{}{
	"api_addr":  {},
	"api_token": {},
	"server":    {},
	// Standalone RTP sender is a separate root entry mode, not SIPp-style scenarios.
	"rtp_send":  {},
	"rtp_addr":  {},
	"rtp_pt":    {},
	"rtp_codec": {},
	"rtp_freq":  {},
	"rtp_dur":   {},
	"rtp_ch":    {},
}

// writeFlagDefaultsForHelp prints flag defaults for the active [HelpContext].
func writeFlagDefaultsForHelp(fs *flag.FlagSet, w io.Writer) {
	switch CurrentHelpContext() {
	case HelpContextRoot:
		writeRootScenarioFlagHint(w)
	case HelpContextServer:
		printFlagDefaultsOmit(fs, w, nil)
	case HelpContextSipp:
		printSippCategorizedFlagHelp(fs, w)
	default:
		printFlagDefaultsOmit(fs, w, rootHelpOmitFlags)
	}
}

// sippHEPFlagOrder is the preferred order for HEP / Homer flags in gossipper sipp -h.
var sippHEPFlagOrder = []string{
	"hep_addr",
	"hep_password",
	"hep_capture_id",
	"hep_raw_rtcp",
	"hep_homer_lake_rtcp",
	"send_media_report",
}

// sippOTLPFlagOrder is the preferred order for structured logging / OTLP flags in gossipper sipp -h.
var sippOTLPFlagOrder = []string{
	"log_stdout",
	"log_file_jsonl",
	"log_otel_endpoint",
	"log_otel_proto",
	"log_otel_insecure",
	"log_otel_header",
	"log_attr",
	"log_buffer_size",
	"log_level",
}

// sippPPROFFlagOrder is the preferred order for profiling flags in gossipper sipp -h.
var sippPPROFFlagOrder = []string{
	"pprof",
	"cpuprofile",
	"memprofile",
}

func sippServiceCategory(name string) (hep, otlp, pprof bool) {
	for _, n := range sippHEPFlagOrder {
		if n == name {
			return true, false, false
		}
	}
	for _, n := range sippOTLPFlagOrder {
		if n == name {
			return false, true, false
		}
	}
	for _, n := range sippPPROFFlagOrder {
		if n == name {
			return false, false, true
		}
	}
	return false, false, false
}

// printSippCategorizedFlagHelp prints flags for gossipper sipp -h in sections:
// HEP, OTLP, PPROF, then SIPP (scenario / transport / tracing / everything else not omitted).
func printSippCategorizedFlagHelp(fs *flag.FlagSet, w io.Writer) {
	byName := make(map[string]*flag.Flag)
	fs.VisitAll(func(fl *flag.Flag) {
		byName[fl.Name] = fl
	})

	var isZeroValueErrs []error
	printSection := func(title string, names []string) {
		var printed bool
		for _, name := range names {
			fl, ok := byName[name]
			if !ok {
				continue
			}
			if _, skip := sippHelpOmitFlags[fl.Name]; skip {
				continue
			}
			if !printed {
				fmt.Fprintf(w, "%s:\n", title)
				printed = true
			}
			isZeroValueErrs = append(isZeroValueErrs, writeSingleFlagDefault(w, fl)...)
		}
		if printed {
			fmt.Fprintln(w)
		}
	}

	printSection("HEP", sippHEPFlagOrder)
	printSection("OTLP", sippOTLPFlagOrder)
	printSection("PPROF", sippPPROFFlagOrder)

	var sippHeader bool
	fs.VisitAll(func(fl *flag.Flag) {
		if _, skip := sippHelpOmitFlags[fl.Name]; skip {
			return
		}
		h, o, p := sippServiceCategory(fl.Name)
		if h || o || p {
			return
		}
		if !sippHeader {
			fmt.Fprintf(w, "SIPP:\n")
			sippHeader = true
		}
		isZeroValueErrs = append(isZeroValueErrs, writeSingleFlagDefault(w, fl)...)
	})

	if len(isZeroValueErrs) > 0 {
		fmt.Fprintln(w)
		for _, err := range isZeroValueErrs {
			fmt.Fprintln(w, err)
		}
	}
}

func writeSingleFlagDefault(w io.Writer, fl *flag.Flag) []error {
	var b strings.Builder
	fmt.Fprintf(&b, "  -%s", fl.Name)
	name, usage := flag.UnquoteUsage(fl)
	if len(name) > 0 {
		b.WriteString(" ")
		b.WriteString(name)
	}
	if b.Len() <= 4 {
		b.WriteString("\t")
	} else {
		b.WriteString("\n    \t")
	}
	b.WriteString(strings.ReplaceAll(usage, "\n", "\n    \t"))

	var isZeroValueErrs []error
	if isZero, err := flagIsZeroValue(fl, fl.DefValue); err != nil {
		isZeroValueErrs = append(isZeroValueErrs, err)
	} else if !isZero {
		if defFormatQuoted(fl.Value) {
			fmt.Fprintf(&b, " (default %q)", fl.DefValue)
		} else {
			fmt.Fprintf(&b, " (default %v)", fl.DefValue)
		}
	}
	fmt.Fprint(w, b.String(), "\n")
	return isZeroValueErrs
}

// printFlagDefaultsOmit mirrors flag.FlagSet.PrintDefaults but skips names in omit (nil = print all).
func printFlagDefaultsOmit(fs *flag.FlagSet, w io.Writer, omit map[string]struct{}) {
	var isZeroValueErrs []error
	fs.VisitAll(func(fl *flag.Flag) {
		if omit != nil {
			if _, skip := omit[fl.Name]; skip {
				return
			}
		}
		isZeroValueErrs = append(isZeroValueErrs, writeSingleFlagDefault(w, fl)...)
	})
	if len(isZeroValueErrs) > 0 {
		fmt.Fprintln(w)
		for _, err := range isZeroValueErrs {
			fmt.Fprintln(w, err)
		}
	}
}

func defFormatQuoted(v flag.Value) bool {
	return strings.Contains(fmt.Sprintf("%T", v), "stringValue")
}

// flagIsZeroValue mirrors flag.isZeroValue (stdlib) for help printing.
func flagIsZeroValue(fl *flag.Flag, value string) (ok bool, err error) {
	typ := reflect.TypeOf(fl.Value)
	var z reflect.Value
	if typ.Kind() == reflect.Pointer {
		z = reflect.New(typ.Elem())
	} else {
		z = reflect.Zero(typ)
	}
	defer func() {
		if e := recover(); e != nil {
			if typ.Kind() == reflect.Pointer {
				typ = typ.Elem()
			}
			err = fmt.Errorf("panic calling String method on zero %v for flag %s: %v", typ, fl.Name, e)
		}
	}()
	return value == z.Interface().(flag.Value).String(), nil
}
