package cli

import (
	"errors"
	"flag"
	"fmt"
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
	ScenarioFile    string
	ScenarioName    string
	Service         string
	Transport       string
	LocalIP         string
	LocalPort       int
	RemoteHost      string
	RemotePort      int
	AuthUsername    string
	AuthPassword    string
	Rate            float64
	TotalCalls      int
	MaxConcurrent   int
	Users           int
	DefaultPause    time.Duration
	DefaultRecvTO   time.Duration
	SummaryJSON     string
	TraceMessages   bool
	TraceShortMsg   bool
	MessageFile     string
	TraceErrors     bool
	ErrorFile       string
	TraceErrorCodes bool
	TraceLogs       bool
	LogFile         string
	TraceStats      bool
	HEPAddr         string
	HEPCaptureID    uint32
	HEPPassword     string
	TLSCertFile     string
	TLSKeyFile      string
	TLSCAFile       string
	TLSSkipVerify   bool
	CommandName     string
	CommandPeers    map[string]string
	CommandRole     string
}

func Parse(args []string) (Config, error) {
	cfg := Config{
		ScenarioName:  "uac",
		Service:       "service",
		Transport:     DefaultTransport,
		LocalIP:       "0.0.0.0",
		AuthPassword:  "password",
		Rate:          DefaultRate,
		TotalCalls:    DefaultTotalCalls,
		MaxConcurrent: DefaultMaxConcurrent,
		Users:         1,
		DefaultPause:  DefaultPauseDurationMS * time.Millisecond,
		DefaultRecvTO: DefaultRecvTimeout,
		TLSSkipVerify: true,
	}

	fs := flag.NewFlagSet("gossipper", flag.ContinueOnError)
	fs.StringVar(&cfg.ScenarioFile, "sf", "", "path to XML scenario file")
	fs.StringVar(&cfg.ScenarioName, "sn", cfg.ScenarioName, "built-in scenario name (uac, uas)")
	fs.StringVar(&cfg.Service, "s", cfg.Service, "service name used in templates")
	fs.StringVar(&cfg.Transport, "t", cfg.Transport, "transport mode: u1, un, t1, tn, l1, ln, s1 or sn")
	fs.StringVar(&cfg.LocalIP, "i", cfg.LocalIP, "local IP address")
	fs.IntVar(&cfg.LocalPort, "p", cfg.LocalPort, "local port")
	fs.StringVar(&cfg.AuthUsername, "au", cfg.AuthUsername, "authorization username for authentication challenges")
	fs.StringVar(&cfg.AuthPassword, "ap", cfg.AuthPassword, "authorization password for authentication challenges")
	fs.Float64Var(&cfg.Rate, "r", cfg.Rate, "calls per second")
	fs.IntVar(&cfg.MaxConcurrent, "l", cfg.MaxConcurrent, "maximum concurrent calls")
	fs.IntVar(&cfg.TotalCalls, "m", cfg.TotalCalls, "total calls to place")
	fs.IntVar(&cfg.Users, "users", cfg.Users, "number of logical users for user-scoped variables")
	fs.StringVar(&cfg.SummaryJSON, "summary_json", "", "write final stats to JSON file")
	fs.BoolVar(&cfg.TraceMessages, "trace_msg", false, "trace sent and received SIP messages")
	fs.BoolVar(&cfg.TraceShortMsg, "trace_shortmsg", false, "trace sent and received messages as compact CSV")
	fs.StringVar(&cfg.MessageFile, "message_file", "", "path to full message trace log file")
	fs.BoolVar(&cfg.TraceErrors, "trace_err", false, "trace unexpected messages and runtime errors")
	fs.StringVar(&cfg.ErrorFile, "error_file", "", "path to error trace log file")
	fs.BoolVar(&cfg.TraceErrorCodes, "trace_error_codes", false, "write unexpected SIP response codes to a compact CSV log")
	fs.BoolVar(&cfg.TraceLogs, "trace_logs", false, "trace action log output to a file")
	fs.StringVar(&cfg.LogFile, "log_file", "", "path to action log trace file")
	fs.BoolVar(&cfg.TraceStats, "trace_stat", false, "trace call statistics")
	fs.StringVar(&cfg.HEPAddr, "hep_addr", "", "HEP3 collector address host:port for SIP mirroring to Homer")
	fs.StringVar(&cfg.HEPPassword, "hep_password", "", "optional HEP3 auth key")
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
	hepCaptureID := fs.Uint("hep_capture_id", 0, "HEP3 capture node ID")

	if err := fs.Parse(args); err != nil {
		return Config{}, err
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
	if cfg.TotalCalls <= 0 {
		return Config{}, errors.New("total calls must be greater than zero")
	}
	if cfg.MaxConcurrent <= 0 {
		return Config{}, errors.New("max concurrent calls must be greater than zero")
	}
	if cfg.Users <= 0 {
		return Config{}, errors.New("users must be greater than zero")
	}

	cfg.DefaultPause = time.Duration(*pauseMS) * time.Millisecond
	cfg.DefaultRecvTO = time.Duration(*recvMS) * time.Millisecond
	cfg.HEPCaptureID = uint32(*hepCaptureID)

	switch cfg.Transport {
	case "u1", "un", "t1", "tn", "l1", "ln", "s1", "sn":
	default:
		return Config{}, fmt.Errorf("unsupported transport %q", cfg.Transport)
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
	if cfg.AuthUsername == "" {
		cfg.AuthUsername = cfg.Service
	}
	if cfg.ErrorFile != "" {
		cfg.TraceErrors = true
	}
	if cfg.LogFile != "" {
		cfg.TraceLogs = true
	}
	if cfg.HEPAddr != "" {
		if _, _, err := splitHostPort(cfg.HEPAddr); err != nil {
			return Config{}, fmt.Errorf("invalid HEP collector address: %w", err)
		}
	}

	return cfg, nil
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
