package cli

import (
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultTransport       = "u1"
	DefaultRate            = 1.0
	DefaultTotalCalls      = 1
	DefaultMaxConcurrent   = 1
	DefaultPauseDurationMS = 1000
	DefaultRecvTimeout     = 5 * time.Second
)

type Config struct {
	ScenarioFile            string
	ScenarioName            string
	Service                 string
	Transport               string
	LocalIP                 string
	LocalPort               int
	RemoteHost              string
	RemotePort              int
	AuthUsername            string
	AuthPassword            string
	Rate                    float64
	RateScale               float64
	RateIncrease            float64
	RateIncreaseStep        time.Duration
	RateMax                 float64
	MaxReconnect            int
	ReconnectSleep          time.Duration
	ReconnectClose          bool
	BaseCSeq                int
	TotalCalls              int
	MaxConcurrent           int
	MaxSockets              int
	Users                   int
	DefaultPause            time.Duration
	DefaultRecvTO           time.Duration
	GlobalTimeout           time.Duration
	SummaryJSON             string
	TraceMessages           bool
	TraceShortMsg           bool
	TraceCounts             bool
	MessageFile             string
	TraceErrors             bool
	ErrorFile               string
	TraceErrorCodes         bool
	TraceLogs               bool
	LogFile                 string
	TraceStats              bool
	TraceRTT                bool
	TraceScreen             bool
	StatsDumpPeriod         time.Duration
	StatPrintPeriod         time.Duration // periodic SummaryLine to stderr (UAC/UAS); 0 = off
	RTTDumpFrequency        int
	ScreenFile              string
	HEPAddr                 string
	HEPCaptureID            uint32
	HEPPassword             string
	HEPRawRTCP              bool
	SendMediaReport         bool
	TLSCertFile             string
	TLSKeyFile              string
	TLSCAFile               string
	TLSSkipVerify           bool
	CommandName             string
	CommandPeers            map[string]string
	CommandRole             string
	InfIndexFile            string
	InfIndexField           int
	InjectionFile           string
	IPField                 int
	UISourceIPs             []string
	TotalCallsSetExplicitly bool
	PprofAddr               string
	CPUProfile              string
	MemProfile              string

	// Standalone RTP sender mode — bypasses SIP scenario engine entirely.
	RTPSend     bool   // -rtp_send
	RTPAddr     string // -rtp_addr  target "host:port"
	RTPPT       int    // -rtp_pt    payload type (0 = PCMU)
	RTPCodec    string // -rtp_codec codec name (e.g. "PCMU/8000")
	RTPFreqMs   int    // -rtp_freq  packet interval ms (default 20)
	RTPDurMs    int    // -rtp_dur   total duration ms (0 = unlimited)
	RTPChannels int    // -rtp_ch    audio channels (default 1)
}

func DefaultConfig() Config {
	return Config{
		ScenarioName:     "uac",
		Service:          "service",
		Transport:        DefaultTransport,
		LocalIP:          "0.0.0.0",
		AuthPassword:     "password",
		Rate:             DefaultRate,
		RateScale:        1.0,
		BaseCSeq:         1,
		TotalCalls:       DefaultTotalCalls,
		MaxConcurrent:    DefaultMaxConcurrent,
		Users:            1,
		DefaultPause:     DefaultPauseDurationMS * time.Millisecond,
		DefaultRecvTO:    DefaultRecvTimeout,
		StatsDumpPeriod:  time.Second,
		RTTDumpFrequency: 200,
		TLSSkipVerify:    true,
		IPField:          -1,
		RTPCodec:         "PCMU/8000",
		RTPFreqMs:        20,
		RTPChannels:      1,
	}
}

func Parse(args []string) (Config, error) {
	cfg := DefaultConfig()
	normalizedArgs, infIndexFile, infIndexField, err := extractInfIndexArgs(args)
	if err != nil {
		return Config{}, err
	}
	cfg.InfIndexFile = infIndexFile
	cfg.InfIndexField = infIndexField

	fs := flag.NewFlagSet("gossipper", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	fs.Usage = func() {
		writeHelpPreamble(fs.Output())
		fs.PrintDefaults()
	}
	fs.StringVar(&cfg.ScenarioFile, "sf", "", "path to XML scenario file")
	fs.StringVar(&cfg.ScenarioName, "sn", cfg.ScenarioName, "built-in scenario name (uac, uas)")
	fs.StringVar(&cfg.Service, "s", cfg.Service, "service name used in templates")
	fs.StringVar(&cfg.Transport, "t", cfg.Transport, "transport mode: u1, un, ui, t1, tn, l1, ln, s1 or sn")
	fs.StringVar(&cfg.LocalIP, "i", cfg.LocalIP, "local IP address")
	fs.IntVar(&cfg.LocalPort, "p", cfg.LocalPort, "local port")
	fs.StringVar(&cfg.InjectionFile, "inf", "", "CSV injection file for ui transport source IP selection")
	fs.IntVar(&cfg.IPField, "ip_field", cfg.IPField, "zero-based CSV field index that contains source IP for ui transport")
	fs.IntVar(&cfg.IPField, "ipfield", cfg.IPField, "alias for -ip_field (SIPp-compatible)")
	fs.StringVar(&cfg.AuthUsername, "au", cfg.AuthUsername, "authorization username for authentication challenges")
	fs.StringVar(&cfg.AuthPassword, "ap", cfg.AuthPassword, "authorization password for authentication challenges")
	fs.Float64Var(&cfg.Rate, "r", cfg.Rate, "calls per second")
	fs.Float64Var(&cfg.RateScale, "rate_scale", cfg.RateScale, "interactive rate control step scale (SIPp-compatible)")
	fs.Float64Var(&cfg.RateIncrease, "rate_increase", 0, "change target cps by this amount every -rate_interval milliseconds")
	fs.Float64Var(&cfg.RateMax, "rate_max", 0, "maximum target cps when using -rate_increase (0 disables cap)")
	ratePeriodMS := fs.Int("rp", 1000, "rate period in milliseconds for -r (SIPp-compatible: n calls every rp ms)")
	rateIntervalMS := fs.Int("rate_interval", 1000, "rate adjustment interval in milliseconds for -rate_increase")
	maxReconnect := fs.Int("max_reconnect", 0, "retry count for reconnecting shared TCP/TLS client transport")
	reconnectSleepMS := fs.Int("reconnect_sleep", 0, "sleep in milliseconds between shared TCP/TLS reconnect attempts")
	reconnectClose := fs.Bool("reconnect_close", false, "close active calls on shared TCP/TLS transport reconnect event")
	fs.IntVar(&cfg.BaseCSeq, "base_cseq", cfg.BaseCSeq, "base CSeq value used by [cseq]")
	fs.IntVar(&cfg.MaxConcurrent, "l", cfg.MaxConcurrent, "maximum concurrent calls")
	fs.IntVar(&cfg.MaxSockets, "max_socket", 0, "maximum number of simultaneously open call sockets (per-call transports)")
	fs.IntVar(&cfg.TotalCalls, "m", cfg.TotalCalls, "total calls to place (0 = unlimited until SIGINT or -timeout_global; stress/long-run)")
	fs.IntVar(&cfg.Users, "users", cfg.Users, "number of logical users for user-scoped variables")
	fs.StringVar(&cfg.SummaryJSON, "summary_json", "", "write final stats to JSON file")
	fs.BoolVar(&cfg.TraceMessages, "trace_msg", false, "trace sent and received SIP messages")
	fs.BoolVar(&cfg.TraceShortMsg, "trace_shortmsg", false, "trace sent and received messages as compact CSV")
	fs.BoolVar(&cfg.TraceCounts, "trace_counts", false, "write periodic SIP message counters as CSV")
	fs.StringVar(&cfg.MessageFile, "message_file", "", "path to full message trace log file")
	fs.BoolVar(&cfg.TraceErrors, "trace_err", false, "trace unexpected messages and runtime errors")
	fs.StringVar(&cfg.ErrorFile, "error_file", "", "path to error trace log file")
	fs.BoolVar(&cfg.TraceErrorCodes, "trace_error_codes", false, "write unexpected SIP response codes to a compact CSV log")
	fs.BoolVar(&cfg.TraceLogs, "trace_logs", false, "trace action log output to a file")
	fs.StringVar(&cfg.LogFile, "log_file", "", "path to action log trace file")
	fs.BoolVar(&cfg.TraceStats, "trace_stat", false, "trace call statistics")
	fs.BoolVar(&cfg.TraceRTT, "trace_rtt", false, "write RTD samples to a compact CSV log")
	fs.BoolVar(&cfg.TraceScreen, "trace_screen", false, "write periodic non-interactive runtime screen snapshots")
	statsDumpFrequency := fs.Int("fd", 1, "statistics dump frequency in seconds (SIPp-compatible for -trace_stat)")
	fs.DurationVar(&cfg.StatPrintPeriod, "stat_period", 0, "print running stats summary line to stderr every interval (e.g. 5s, 1m30s); 0 disables (works for UAC and UAS)")
	rttDumpFrequency := fs.Int("rtt_freq", 200, "dump RTD samples every N completed calls when -trace_rtt is enabled")
	fs.StringVar(&cfg.ScreenFile, "screen_file", "", "path to runtime screen trace log file")
	fs.StringVar(&cfg.HEPAddr, "hep_addr", "", "HEP3 collector address host:port for SIP mirroring to Homer")
	fs.StringVar(&cfg.HEPPassword, "hep_password", "", "optional HEP3 auth key")
	fs.BoolVar(&cfg.HEPRawRTCP, "hep_raw_rtcp", true, "send RTP/RTCP as raw HEP (type 5); set false to send JSON reports like hepagent (RTP type 35, RTCP type 37)")
	fs.BoolVar(&cfg.SendMediaReport, "send_media_report", false, "send RTP/RTCP media reports to HEP: JSON type35/37+DTMF type100 every 10s (hep_raw_rtcp=false) or raw RTCP SR type5 every 5s (hep_raw_rtcp=true)")
	fs.StringVar(&cfg.TLSCertFile, "tls_cert", "", "TLS certificate file for server mode or mutual TLS")
	fs.StringVar(&cfg.TLSKeyFile, "tls_key", "", "TLS private key file")
	fs.StringVar(&cfg.TLSCAFile, "tls_ca", "", "TLS CA bundle file")
	fs.BoolVar(&cfg.TLSSkipVerify, "tls_skip_verify", cfg.TLSSkipVerify, "skip TLS certificate verification")
	fs.StringVar(&cfg.CommandName, "cmd_name", "", "instance name for external sendCmd/recvCmd transport")

	var remoteAddr string
	var commandPeersFile string
	var masterName string
	var slaveName string
	var slaveCfgFile string
	fs.StringVar(&remoteAddr, "rsa", "", "remote SIP address host:port")
	fs.StringVar(&commandPeersFile, "cmd_peers", "", "path to peer map file in name;host:port format")
	fs.StringVar(&masterName, "master", "", "3pcc extended mode: local master instance name")
	fs.StringVar(&slaveName, "slave", "", "3pcc extended mode: local slave instance name")
	fs.StringVar(&slaveCfgFile, "slave_cfg", "", "3pcc extended mode: peer map file in name;host:port format")

	pauseMS := fs.Int("pause_ms", DefaultPauseDurationMS, "default pause duration in milliseconds")
	recvMS := fs.Int("recv_timeout_ms", int(DefaultRecvTimeout/time.Millisecond), "default receive timeout in milliseconds")
	timeoutGlobalSec := fs.Int("timeout_global", 0, "exit after N seconds of total runtime (SIPp-compatible)")
	hepCaptureID := fs.Uint("hep_capture_id", 0, "HEP3 capture node ID")
	fs.StringVar(&cfg.PprofAddr, "pprof", "", "pprof HTTP address (e.g. :6060) for live CPU/memory/goroutine profiling")
	fs.StringVar(&cfg.CPUProfile, "cpuprofile", "", "write CPU profile to file at exit")
	fs.StringVar(&cfg.MemProfile, "memprofile", "", "write memory profile to file at exit")

	fs.BoolVar(&cfg.RTPSend, "rtp_send", false, "enable standalone RTP sender mode (no SIP scenario required)")
	fs.StringVar(&cfg.RTPAddr, "rtp_addr", "", "target RTP address host:port for standalone RTP sender")
	fs.IntVar(&cfg.RTPPT, "rtp_pt", 0, "RTP payload type for standalone sender (0 = PCMU; applies after -rtp_codec)")
	fs.StringVar(&cfg.RTPCodec, "rtp_codec", cfg.RTPCodec, "codec name for standalone RTP sender (e.g. PCMU/8000, PCMA/8000, G722/8000)")
	fs.IntVar(&cfg.RTPFreqMs, "rtp_freq", cfg.RTPFreqMs, "packet interval in milliseconds for standalone RTP sender")
	fs.IntVar(&cfg.RTPDurMs, "rtp_dur", 0, "total duration in milliseconds for standalone RTP sender (0 = run until interrupted)")
	fs.IntVar(&cfg.RTPChannels, "rtp_ch", cfg.RTPChannels, "number of audio channels for standalone RTP sender (1 = mono)")

	if err := fs.Parse(normalizedArgs); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fs.Usage()
		}
		return Config{}, err
	}
	providedFlags := make(map[string]struct{})
	fs.Visit(func(f *flag.Flag) {
		providedFlags[f.Name] = struct{}{}
	})
	cfg.TotalCallsSetExplicitly = false
	if _, ok := providedFlags["m"]; ok {
		cfg.TotalCallsSetExplicitly = true
	}

	if remoteAddr == "" && fs.NArg() > 0 {
		remoteAddr = fs.Arg(0)
	}

	if remoteAddr != "" {
		host, port, err := splitHostPort(remoteAddr)
		if err != nil {
			return Config{}, fmt.Errorf("invalid remote address: %w", err)
		}
		cfg.RemoteHost = host
		cfg.RemotePort = port
	}

	if cfg.Rate <= 0 {
		return Config{}, errors.New("rate must be greater than zero")
	}
	if cfg.RateScale <= 0 {
		return Config{}, errors.New("rate_scale must be greater than zero")
	}
	if *rateIntervalMS <= 0 {
		return Config{}, errors.New("rate_interval must be greater than zero")
	}
	if cfg.RateMax < 0 {
		return Config{}, errors.New("rate_max must be greater than or equal to zero")
	}
	if *maxReconnect < 0 {
		return Config{}, errors.New("max_reconnect must be greater than or equal to zero")
	}
	if *reconnectSleepMS < 0 {
		return Config{}, errors.New("reconnect_sleep must be greater than or equal to zero")
	}
	if cfg.TotalCalls < 0 {
		return Config{}, errors.New("total calls must be greater than or equal to zero")
	}
	if cfg.TotalCalls == 0 && !cfg.TotalCallsSetExplicitly {
		return Config{}, errors.New("total calls must be greater than zero (pass -m 0 explicitly for unlimited stress mode)")
	}
	if cfg.MaxConcurrent <= 0 {
		return Config{}, errors.New("max concurrent calls must be greater than zero")
	}
	if cfg.MaxSockets < 0 {
		return Config{}, errors.New("max_socket must be greater than or equal to zero")
	}
	if cfg.Users <= 0 {
		return Config{}, errors.New("users must be greater than zero")
	}
	if *ratePeriodMS <= 0 {
		return Config{}, errors.New("rp must be greater than zero")
	}
	if cfg.BaseCSeq <= 0 {
		return Config{}, errors.New("base_cseq must be greater than zero")
	}
	if *statsDumpFrequency <= 0 {
		return Config{}, errors.New("fd must be greater than zero")
	}
	if *rttDumpFrequency <= 0 {
		return Config{}, errors.New("rtt_freq must be greater than zero")
	}
	if *timeoutGlobalSec < 0 {
		return Config{}, errors.New("timeout_global must be greater than or equal to zero")
	}
	if cfg.InfIndexField < 0 {
		return Config{}, errors.New("infindex field must be greater than or equal to zero")
	}
	if cfg.IPField < -1 {
		return Config{}, errors.New("ip_field must be greater than or equal to -1")
	}

	cfg.DefaultPause = time.Duration(*pauseMS) * time.Millisecond
	cfg.DefaultRecvTO = time.Duration(*recvMS) * time.Millisecond
	cfg.GlobalTimeout = time.Duration(*timeoutGlobalSec) * time.Second
	cfg.Rate = cfg.Rate * (1000.0 / float64(*ratePeriodMS))
	cfg.RateIncreaseStep = time.Duration(*rateIntervalMS) * time.Millisecond
	cfg.MaxReconnect = *maxReconnect
	cfg.ReconnectSleep = time.Duration(*reconnectSleepMS) * time.Millisecond
	cfg.ReconnectClose = *reconnectClose
	cfg.StatsDumpPeriod = time.Duration(*statsDumpFrequency) * time.Second
	cfg.RTTDumpFrequency = *rttDumpFrequency
	cfg.HEPCaptureID = uint32(*hepCaptureID)
	if cfg.StatPrintPeriod < 0 {
		return Config{}, errors.New("stat_period must be greater than or equal to zero")
	}

	cfg.Transport = strings.ToLower(strings.TrimSpace(cfg.Transport))
	cfg.InjectionFile = strings.TrimSpace(cfg.InjectionFile)

	switch cfg.Transport {
	case "u1", "un", "ui", "t1", "tn", "l1", "ln", "s1", "sn":
	default:
		return Config{}, fmt.Errorf("unsupported transport %q", cfg.Transport)
	}
	if cfg.InjectionFile != "" && cfg.IPField < 0 {
		return Config{}, errors.New("ip_field must be specified when inf is set")
	}
	if cfg.IPField >= 0 && cfg.InjectionFile == "" {
		return Config{}, errors.New("inf must be specified when ip_field is set")
	}
	if cfg.Transport == "ui" {
		if cfg.InjectionFile == "" || cfg.IPField < 0 {
			return Config{}, errors.New("transport ui requires both inf and ip_field")
		}
		sourceIPs, err := loadSourceIPsFromInjection(cfg.InjectionFile, cfg.IPField)
		if err != nil {
			return Config{}, err
		}
		cfg.UISourceIPs = sourceIPs
	} else if cfg.InjectionFile != "" || cfg.IPField >= 0 {
		return Config{}, errors.New("inf and ip_field are only supported with transport ui")
	}

	if cfg.ScenarioFile == "" && cfg.ScenarioName == "" {
		return Config{}, errors.New("either -sf or -sn must be specified")
	}
	if cfg.CommandName != "" && (masterName != "" || slaveName != "") {
		return Config{}, errors.New("-cmd_name is not compatible with -master or -slave")
	}
	if masterName != "" && slaveName != "" {
		return Config{}, errors.New("-master and -slave are mutually exclusive")
	}
	if commandPeersFile != "" && slaveCfgFile != "" && commandPeersFile != slaveCfgFile {
		return Config{}, errors.New("-cmd_peers is not compatible with a different -slave_cfg value")
	}
	if commandPeersFile == "" {
		commandPeersFile = slaveCfgFile
	}
	switch {
	case masterName != "":
		cfg.CommandName = masterName
		cfg.CommandRole = "master"
	case slaveName != "":
		cfg.CommandName = slaveName
		cfg.CommandRole = "slave"
	}
	if cfg.CommandName != "" || commandPeersFile != "" {
		if cfg.CommandName == "" || commandPeersFile == "" {
			return Config{}, errors.New("command transport requires both local name and peer map file")
		}
		peers, err := parseCommandPeersFile(commandPeersFile)
		if err != nil {
			return Config{}, err
		}
		if _, ok := peers[cfg.CommandName]; !ok {
			return Config{}, fmt.Errorf("command peer map does not contain local instance %q", cfg.CommandName)
		}
		cfg.CommandPeers = peers
	}
	if cfg.MessageFile != "" {
		cfg.TraceMessages = true
	}
	if _, ok := providedFlags["fd"]; ok {
		cfg.TraceStats = true
	}
	if _, ok := providedFlags["rtt_freq"]; ok {
		cfg.TraceRTT = true
	}
	if cfg.AuthUsername == "" {
		cfg.AuthUsername = cfg.Service
	}
	if cfg.ErrorFile != "" {
		cfg.TraceErrors = true
	}
	if cfg.LogFile != "" {
		cfg.TraceLogs = true
	}
	if cfg.ScreenFile != "" {
		cfg.TraceScreen = true
	}
	if cfg.HEPAddr != "" {
		if _, _, err := splitHostPort(cfg.HEPAddr); err != nil {
			return Config{}, fmt.Errorf("invalid HEP collector address: %w", err)
		}
	}
	if cfg.MaxSockets > 0 {
		switch cfg.Transport {
		case "un", "tn", "ln":
		default:
			return Config{}, errors.New("max_socket is only supported with un, tn, or ln transport")
		}
	}
	if cfg.MaxReconnect > 0 || cfg.ReconnectSleep > 0 || cfg.ReconnectClose {
		switch cfg.Transport {
		case "t1", "l1":
		default:
			return Config{}, errors.New("max_reconnect/reconnect_sleep/reconnect_close are only supported with t1 or l1 transport")
		}
	}

	if cfg.RTPSend {
		if cfg.RTPAddr == "" {
			return Config{}, errors.New("-rtp_addr is required for standalone RTP sender")
		}
		if cfg.RTPFreqMs <= 0 {
			return Config{}, errors.New("rtp_freq must be greater than zero")
		}
		if cfg.RTPChannels <= 0 {
			return Config{}, errors.New("rtp_ch must be greater than zero")
		}
		if cfg.RTPDurMs < 0 {
			return Config{}, errors.New("rtp_dur must be greater than or equal to zero")
		}
	}

	return cfg, nil
}

func loadSourceIPsFromInjection(path string, field int) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("unable to open inf file: %w", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1
	sourceIPs := make([]string, 0, 64)
	for rowIndex := 0; ; rowIndex++ {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("unable to parse inf file: %w", err)
		}
		if field >= len(record) {
			return nil, fmt.Errorf("inf file %q row %d: ip_field %d is out of range", path, rowIndex+1, field)
		}
		if isIgnorableInfRow(record) {
			continue
		}
		value := strings.TrimSpace(record[field])
		if value == "" {
			return nil, fmt.Errorf("inf file %q row %d field %d: empty source IP", path, rowIndex+1, field)
		}
		if net.ParseIP(value) == nil {
			return nil, fmt.Errorf("inf file %q row %d field %d: invalid source IP %q", path, rowIndex+1, field, value)
		}
		sourceIPs = append(sourceIPs, value)
	}
	if len(sourceIPs) == 0 {
		return nil, fmt.Errorf("inf file %q: does not contain any source IP rows", path)
	}
	return sourceIPs, nil
}

func isIgnorableInfRow(record []string) bool {
	if len(record) == 0 {
		return true
	}
	if strings.HasPrefix(strings.TrimSpace(record[0]), "#") {
		return true
	}
	for _, cell := range record {
		if strings.TrimSpace(cell) != "" {
			return false
		}
	}
	return true
}

func extractInfIndexArgs(args []string) ([]string, string, int, error) {
	normalized := make([]string, 0, len(args))
	var (
		fileName string
		field    int
		seen     bool
	)

	for idx := 0; idx < len(args); idx++ {
		if args[idx] != "-infindex" {
			normalized = append(normalized, args[idx])
			continue
		}
		if seen {
			return nil, "", 0, errors.New("infindex may only be specified once")
		}
		seen = true
		if idx+1 >= len(args) {
			return nil, "", 0, errors.New("infindex requires file and field")
		}

		next := strings.TrimSpace(args[idx+1])
		if csvFile, csvField, ok := strings.Cut(next, ","); ok {
			parsedField, err := strconv.Atoi(strings.TrimSpace(csvField))
			if err != nil {
				return nil, "", 0, fmt.Errorf("invalid infindex field: %w", err)
			}
			fileName = strings.TrimSpace(csvFile)
			field = parsedField
			idx += 1
			continue
		}
		if idx+2 >= len(args) {
			return nil, "", 0, errors.New("infindex requires file and field")
		}
		parsedField, err := strconv.Atoi(strings.TrimSpace(args[idx+2]))
		if err != nil {
			return nil, "", 0, fmt.Errorf("invalid infindex field: %w", err)
		}
		fileName = next
		field = parsedField
		idx += 2
	}

	if strings.TrimSpace(fileName) == "" {
		return normalized, "", 0, nil
	}
	if field < 0 {
		return nil, "", 0, errors.New("infindex field must be greater than or equal to zero")
	}
	return normalized, fileName, field, nil
}

func writeHelpPreamble(w io.Writer) {
	fmt.Fprintln(w, "Gossipper — SIP load generator (https://github.com/QXIP/gossipper)")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Subcommands (run before any flags):")
	fmt.Fprintln(w, "  gossipper shell              interactive line shell: set flags, wizard, hint, run")
	fmt.Fprintln(w, "  gossipper cli                alias for shell")
	fmt.Fprintln(w, "  gossipper tui                full-screen launcher / runtime UI")
	fmt.Fprintln(w, "  gossipper -interactive       same as tui")
	fmt.Fprintln(w, "  gossipper pcap2scenario ...  PCAP → XML scenarios")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "See also: docs/interactive-shell.md, docs/tui.md")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Flags:")
}

func splitHostPort(input string) (string, int, error) {
	if strings.Count(input, ":") == 1 && !strings.HasPrefix(input, "[") {
		host, portStr, ok := strings.Cut(input, ":")
		if !ok {
			return "", 0, fmt.Errorf("expected host:port")
		}
		port, err := strconv.Atoi(portStr)
		if err != nil {
			return "", 0, err
		}
		return host, port, nil
	}

	host, portStr, err := net.SplitHostPort(input)
	if err != nil {
		return "", 0, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return "", 0, err
	}
	return host, port, nil
}

func parseCommandPeersFile(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	peers := make(map[string]string)
	for lineNumber, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, addr, ok := strings.Cut(line, ";")
		if !ok {
			return nil, fmt.Errorf("invalid cmd peer entry at line %d", lineNumber+1)
		}
		name = strings.TrimSpace(name)
		addr = strings.TrimSpace(addr)
		if name == "" || addr == "" {
			return nil, fmt.Errorf("invalid cmd peer entry at line %d", lineNumber+1)
		}
		if _, _, err := splitHostPort(addr); err != nil {
			return nil, fmt.Errorf("invalid cmd peer address at line %d: %w", lineNumber+1, err)
		}
		peers[name] = addr
	}
	if len(peers) == 0 {
		return nil, errors.New("cmd peer map is empty")
	}
	return peers, nil
}
