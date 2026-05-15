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

	"github.com/sipcapture/gossipper/mediasink"
)

// ServerListener is one SIP server bind for --server when using the "listeners" config array.
// Transport must be u1, un, t1, tn, l1, or ln (TLS requires tls_cert / tls_key in config).
type ServerListener struct {
	Transport string `json:"transport,omitempty"`
	LocalIP   string `json:"local_ip,omitempty"`
	LocalPort int    `json:"local_port,omitempty"`
}

const (
	DefaultTransport       = "u1"
	DefaultRate            = 1.0
	DefaultTotalCalls      = 1
	DefaultMaxConcurrent   = 1
	DefaultPauseDurationMS = 1000
	DefaultRecvTimeout     = 5 * time.Second

	// Prefix of injection file used to guess comma vs semicolon (SIPp-style) delimiter.
	infDelimiterPeekBytes = 65536
)

type Config struct {
	ScenarioFile string
	ScenarioName string
	Service      string
	Transport    string
	LocalIP      string
	LocalPort    int
	// ServerListeners binds several SIP server sockets in --server when non-empty
	// (UDP u1/un, TCP t1/tn, TLS l1/ln, or mixed; see run profile "listeners").
	ServerListeners  []ServerListener
	RemoteHost       string
	RemotePort       int
	AuthUsername     string
	AuthPassword     string
	Rate             float64
	RateScale        float64
	RateIncrease     float64
	RateIncreaseStep time.Duration
	RateMax          float64
	MaxReconnect     int
	ReconnectSleep   time.Duration
	ReconnectClose   bool
	BaseCSeq         int
	TotalCalls       int
	MaxConcurrent    int
	MaxSockets       int
	Users            int
	DefaultPause     time.Duration
	DefaultRecvTO    time.Duration
	RecvBYEFloorTO   time.Duration // minimum mandatory recv BYE when scenario timeout omitted; 0=off
	GlobalTimeout    time.Duration
	SummaryJSON      string
	SummaryHTML      string
	// ToolVersion is set by the main package (not a CLI flag) for JSON export.
	ToolVersion             string
	HealthMinSuccessRatio   float64
	HealthMaxFailedCalls    int
	HealthMaxTimeouts       int
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
	HEPHomerLakeRTCP        bool
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
	ApiAddr                 string // e.g. :8080 — HTTP API for stats / scenario / control
	ApiToken                string // optional Bearer token for /api/v1
	// ServerMode runs a minimal SIP UAS plus the management HTTP API for systemd / Control UI.
	ServerMode bool
	CPUProfile string
	MemProfile string

	// Standalone RTP sender mode — bypasses SIP scenario engine entirely.
	RTPSend     bool   // -rtp_send
	RTPAddr     string // -rtp_addr  target "host:port"
	RTPPT       int    // -rtp_pt    payload type (0 = PCMU)
	RTPCodec    string // -rtp_codec codec name (e.g. "PCMU/8000")
	RTPFreqMs   int    // -rtp_freq  packet interval ms (default 20)
	RTPDurMs    int    // -rtp_dur   total duration ms (0 = unlimited)
	RTPChannels int    // -rtp_ch    audio channels (default 1)

	// Structured event logging (universal Logger + OTLP).
	LogStdout       bool              // -log_stdout: emit text events to stderr
	LogFileJSONL    string            // -log_file_jsonl path: emit one JSON object per event
	LogOTELEndpoint string            // -log_otel_endpoint host:port (OTLP collector)
	LogOTELProto    string            // -log_otel_proto grpc|http
	LogOTELInsecure bool              // -log_otel_insecure: disable TLS (gRPC) or use http://
	LogOTELHeaders  map[string]string // -log_otel_header key=value (repeatable)
	LogAttrs        map[string]string // -log_attr key=value (repeatable, e.g. self_tag=NYC02)
	LogBufferSize   int               // -log_buffer_size N (ring buffer capacity)
	LogLevel        string            // -log_level info|debug|warn|error

	// PCAPLinkLayer selects PCAP datalink decoding for play_pcap_* (-pcap-link).
	PCAPLinkLayer string

	// SipFrom / SipPAI / SipProvider / SipExtraHeaders drive [trunk_*] keywords in built-in scenarios (see docs/compatibility.md).
	SipFrom         string
	SipPAI          string
	SipProvider     string
	SipExtraHeaders []string

	// RecordWAVDir enables automatic per-call WAV capture (decoded remote G.711); see docs/media-roadmap.md.
	RecordWAVDir    string
	RecordWAVDuplex bool
	// CallRecordsJSONL appends one JSON object per finished call (schema gossipper_call_record_v1).
	CallRecordsJSONL string

	// HealthMaxRTCPFractionLost, when > 0, fails if media.rtcp_max_fraction_lost exceeds this (0..1).
	HealthMaxRTCPFractionLost float64
	HealthMaxRTCPJitterTS     int
	HealthMinRTPPacketsRecv   int
	// HealthMinRTPPacketsRecvPerCall gates on per-call minimum inbound RTP (see media.per_call_min_rtp_packets_received).
	HealthMinRTPPacketsRecvPerCall int
	// MediaRejectSRTP fails rtp_stream start/mic when remote SDP suggests SRTP.
	MediaRejectSRTP bool
	// MediaSRTP enables SDES SRTP (a=crypto inline) for rtp_stream start/mic when the peer offers SRTP.
	MediaSRTP bool
	// TURN (optional): host:port and credentials for ICE typ relay paths.
	TURNServer string
	TURNUser   string
	TURNPass   string
	TURNRealm  string
}

func DefaultConfig() Config {
	return Config{
		ScenarioName:         "uac",
		Service:              "service",
		Transport:            DefaultTransport,
		LocalIP:              "0.0.0.0",
		AuthPassword:         "password",
		Rate:                 DefaultRate,
		RateScale:            1.0,
		BaseCSeq:             1,
		TotalCalls:           DefaultTotalCalls,
		MaxConcurrent:        DefaultMaxConcurrent,
		Users:                1,
		DefaultPause:         DefaultPauseDurationMS * time.Millisecond,
		DefaultRecvTO:        DefaultRecvTimeout,
		StatsDumpPeriod:      time.Second,
		RTTDumpFrequency:     200,
		TLSSkipVerify:        true,
		IPField:              -1,
		RTPCodec:             "PCMU/8000",
		RTPFreqMs:            20,
		RTPChannels:          1,
		LogOTELProto:         "grpc",
		LogBufferSize:        16384,
		LogLevel:             "info",
		HealthMaxFailedCalls: -1,
		HealthMaxTimeouts:    -1,
	}
}

func Parse(args []string) (Config, error) {
	defer resetHelpContext()
	if CurrentHelpContext() == HelpContextUnset {
		SetHelpContext(HelpContextRoot)
	}
	rest, meta, err := parseRunProfileMeta(args)
	if err != nil {
		return Config{}, err
	}
	if (meta.ServerConfigPath != "" || meta.ClientConfigPath != "") && (meta.ConfigPath != "" || meta.RunAlias != "" || meta.ListAliases) {
		return Config{}, errors.New("-config-server and -config-client cannot be combined with -config, -run-alias, or -list-aliases")
	}
	if meta.ServerConfigPath != "" && meta.ClientConfigPath != "" {
		return Config{}, errors.New("-config-server cannot be combined with -config-client")
	}
	if meta.ListAliases {
		if meta.ConfigPath == "" {
			return Config{}, errors.New("-list-aliases requires -config <path>")
		}
		if err := printRunProfileAliases(meta.ConfigPath); err != nil {
			return Config{}, err
		}
		return Config{}, ErrListAliases
	}
	if meta.ConfigPath != "" && meta.RunAlias == "" {
		return Config{}, errors.New("-config requires -run-alias <name> (use -config <path> -list-aliases to list names)")
	}
	if meta.RunAlias != "" && meta.ConfigPath == "" {
		return Config{}, errors.New("-run-alias requires -config <path>")
	}

	normalizedArgs, infIndexFile, infIndexField, err := extractInfIndexArgs(rest)
	if err != nil {
		return Config{}, err
	}

	cfg := DefaultConfig()
	var profileTotalCallsExplicit bool
	if meta.ServerConfigPath != "" {
		extra, err := LoadAndApplyServerConfig(&cfg, meta.ServerConfigPath)
		if err != nil {
			return Config{}, err
		}
		cfg.ServerMode = true
		profileTotalCallsExplicit = cfg.TotalCallsSetExplicitly
		if len(extra) > 0 {
			normalizedArgs = append(append([]string(nil), extra...), normalizedArgs...)
		}
	} else if meta.ClientConfigPath != "" {
		extra, err := LoadAndApplyClientConfig(&cfg, meta.ClientConfigPath)
		if err != nil {
			return Config{}, err
		}
		profileTotalCallsExplicit = cfg.TotalCallsSetExplicitly
		if len(extra) > 0 {
			normalizedArgs = append(append([]string(nil), extra...), normalizedArgs...)
		}
	} else if meta.ConfigPath != "" {
		extra, err := LoadAndApplyRunProfile(&cfg, meta.ConfigPath, meta.RunAlias)
		if err != nil {
			return Config{}, err
		}
		profileTotalCallsExplicit = cfg.TotalCallsSetExplicitly
		if len(extra) > 0 {
			normalizedArgs = append(append([]string(nil), extra...), normalizedArgs...)
		}
	}
	cfg.InfIndexFile = infIndexFile
	cfg.InfIndexField = infIndexField

	fs := flag.NewFlagSet("gossipper", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	fs.Usage = func() {
		switch CurrentHelpContext() {
		case HelpContextSipp:
			PrintSIPPEntrySummary(fs.Output())
			fmt.Fprintln(fs.Output(), "Flags:")
		case HelpContextServer:
			writeServerHelpPreamble(fs.Output())
		default:
			writeHelpPreamble(fs.Output())
		}
		writeFlagDefaultsForHelp(fs, fs.Output())
	}
	fs.StringVar(&cfg.ScenarioFile, "sf", cfg.ScenarioFile, "path to XML scenario file")
	fs.StringVar(&cfg.ScenarioName, "sn", cfg.ScenarioName, "built-in scenario name (uac, uas, management, invite_media, invite_media_early, invite_media_early_180)")
	fs.StringVar(&cfg.Service, "s", cfg.Service, "service name used in templates")
	fs.StringVar(&cfg.Transport, "t", cfg.Transport, "transport: u1/un/ui, t1/tn, l1/ln; client TLS aliases cl/cln; server UDP s1/sn; server TLS sl")
	fs.StringVar(&cfg.LocalIP, "i", cfg.LocalIP, "local IP address")
	fs.IntVar(&cfg.LocalPort, "p", cfg.LocalPort, "local port")
	fs.StringVar(&cfg.InjectionFile, "inf", cfg.InjectionFile, "CSV path: with ui, optional -ip_field selects bind/source IP column; if omitted with ui, bind uses -i like u1/un; with TLS (cl/cln/l1/ln) optional -ip_field for per-row bind IPs; with TCP/UDP client (t1/tn/u1/un) optional -inf without -ip_field for [fieldN] only (bind uses -i)")
	fs.IntVar(&cfg.IPField, "ip_field", cfg.IPField, "zero-based CSV column for bind/source IP (optional with -inf for ui — defaults to -i; optional for TLS cl/cln/l1/ln; not supported with TCP/UDP t1/tn/u1/un)")
	fs.IntVar(&cfg.IPField, "ipfield", cfg.IPField, "alias for -ip_field (SIPp-compatible)")
	fs.StringVar(&cfg.AuthUsername, "au", cfg.AuthUsername, "authorization username for authentication challenges")
	fs.StringVar(&cfg.AuthPassword, "ap", cfg.AuthPassword, "authorization password for authentication challenges")
	fs.StringVar(&cfg.SipFrom, "sip_from", cfg.SipFrom, "SIP From value before ;tag= in built-in UAC scenarios (name-addr or URI); empty = gossip <sip:gossip@local_ip:local_port>")
	fs.StringVar(&cfg.SipPAI, "sip_pai", cfg.SipPAI, "P-Asserted-Identity value only (no header name); empty omits the header")
	fs.StringVar(&cfg.SipProvider, "sip_provider", cfg.SipProvider, "sets X-provider to this token; empty omits")
	fs.Func("sip_extra_header", "repeatable: one extra SIP header line \"Name: value\" after Via on first in-dialog requests in built-in UAC scenarios", func(s string) error {
		cfg.SipExtraHeaders = append(cfg.SipExtraHeaders, strings.TrimSpace(s))
		return nil
	})
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
	fs.StringVar(&cfg.SummaryJSON, "summary_json", cfg.SummaryJSON, "write final stats to JSON file")
	fs.StringVar(&cfg.SummaryHTML, "summary_html", cfg.SummaryHTML, "write final stats to a standalone HTML report (same data as -summary_json; can be used without JSON)")
	fs.Float64Var(&cfg.HealthMinSuccessRatio, "health_min_success_ratio", 0, "when >0 with -summary_json or -summary_html, fail run if success_ratio is lower (e.g. 0.95); exit code 2")
	fs.IntVar(&cfg.HealthMaxFailedCalls, "health_max_failed_calls", -1, "when >=0 with -summary_json or -summary_html, fail if failed_calls exceed this (0 means any failure fails); exit code 2")
	fs.IntVar(&cfg.HealthMaxTimeouts, "health_max_timeouts", -1, "when >=0 with -summary_json or -summary_html, fail if timeouts exceed this; exit code 2")
	fs.Float64Var(&cfg.HealthMaxRTCPFractionLost, "health_max_rtcp_fraction_lost", 0, "when >0 with summary/health output, fail if media rtcp_max_fraction_lost exceeds this (0..1); exit code 2")
	fs.IntVar(&cfg.HealthMaxRTCPJitterTS, "health_max_rtcp_jitter_ts", 0, "when >0 with summary/health output, fail if media rtcp_max_jitter_ts exceeds this; exit code 2")
	fs.IntVar(&cfg.HealthMinRTPPacketsRecv, "health_min_rtp_packets_recv", 0, "when >0 with summary/health output, fail if aggregated rtp_packets_received is below this; exit code 2")
	fs.IntVar(&cfg.HealthMinRTPPacketsRecvPerCall, "health_min_rtp_packets_recv_per_call", 0, "when >0, fail if any call had fewer inbound RTP packets than this (per-call min); exit code 2")
	fs.StringVar(&cfg.RecordWAVDir, "record_wav_dir", "", "auto-record incoming RTP (G.711) to WAV per call in this directory (requires active media)")
	fs.BoolVar(&cfg.RecordWAVDuplex, "record_wav_duplex", false, "with -record_wav_dir, write stereo WAV (L=sent R=received)")
	fs.StringVar(&cfg.CallRecordsJSONL, "call_records_jsonl", "", "append one JSON call record per finished call to this path")
	fs.BoolVar(&cfg.MediaRejectSRTP, "media_reject_srtp", false, "fail rtp_stream start/mic when remote SDP suggests SRTP (RTP/SAVP, a=crypto, a=fingerprint)")
	fs.BoolVar(&cfg.MediaSRTP, "media_srtp", false, "when remote SDP offers SRTP: SDES (a=crypto inline) and/or DTLS-SRTP client (a=fingerprint sha-256); encrypt RTP/SRTCP outbound, decrypt inbound; RTCP stays on RTP port+1 unless peer muxes")
	fs.StringVar(&cfg.TURNServer, "turn_server", "", "TURN/STUN server host:port for ICE relay (typ relay) RTP/RTCP")
	fs.StringVar(&cfg.TURNUser, "turn_user", "", "TURN long-term credential username")
	fs.StringVar(&cfg.TURNPass, "turn_pass", "", "TURN long-term credential password")
	fs.StringVar(&cfg.TURNRealm, "turn_realm", "", "TURN realm (optional; empty uses server default)")
	fs.BoolVar(&cfg.TraceMessages, "trace_msg", cfg.TraceMessages, "trace sent and received SIP messages")
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
	fs.StringVar(&cfg.HEPAddr, "hep_addr", cfg.HEPAddr, "HEP3 collector address host:port for SIP mirroring to Homer")
	fs.StringVar(&cfg.HEPPassword, "hep_password", cfg.HEPPassword, "optional HEP3 auth key")
	fs.StringVar(&cfg.PCAPLinkLayer, "pcap-link", cfg.PCAPLinkLayer, "PCAP datalink for play_pcap_*: auto (default), ethernet, linux_sll, linux_sll2, raw, null, loop, ipv4, ipv6, or numeric DLT")
	fs.BoolVar(&cfg.HEPRawRTCP, "hep_raw_rtcp", cfg.HEPRawRTCP, "send aggregated RTP as binary RTCP SR on HEP type 5 every 5s; ignored when hep_homer_lake_rtcp=true; must stay true for OSS when send_media_report is on unless hep_homer_lake_rtcp is set")
	fs.BoolVar(&cfg.HEPHomerLakeRTCP, "hep_homer_lake_rtcp", cfg.HEPHomerLakeRTCP, "Homer-Lake: HEP type 5 with JSON RTCP SR body every 5s (open-source); takes precedence over hep_raw_rtcp")
	fs.BoolVar(&cfg.SendMediaReport, "send_media_report", cfg.SendMediaReport, "send RTP/RTCP media reports to HEP (built-in: homer-lake JSON or raw SR; short JSON 0x22/0x24/0x64 only with a linked extension)")
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
	fs.StringVar(&remoteAddr, "rsa", "", "remote SIP address host:port (UAC / templates); UAS binds -i/-p (not -rsa)")
	fs.StringVar(&commandPeersFile, "cmd_peers", "", "path to peer map file in name;host:port format")
	fs.StringVar(&masterName, "master", "", "3pcc extended mode: local master instance name")
	fs.StringVar(&slaveName, "slave", "", "3pcc extended mode: local slave instance name")
	fs.StringVar(&slaveCfgFile, "slave_cfg", "", "3pcc extended mode: peer map file in name;host:port format")

	pauseMS := fs.Int("pause_ms", DefaultPauseDurationMS, "default pause duration in milliseconds")
	recvMS := fs.Int("recv_timeout_ms", int(DefaultRecvTimeout/time.Millisecond), "default receive timeout in milliseconds")
	recvByeFloorMS := fs.Int("recv_bye_timeout_ms", 90000, "minimum milliseconds for mandatory <recv request=\"BYE\"/> when scenario omits timeout (0=use only recv_timeout_ms)")
	timeoutGlobalSec := fs.Int("timeout_global", 0, "exit after N seconds of total runtime (SIPp-compatible)")
	hepCaptureID := fs.Uint("hep_capture_id", uint(cfg.HEPCaptureID), "HEP3 capture node ID")
	fs.StringVar(&cfg.PprofAddr, "pprof", "", "pprof HTTP address (e.g. :6060) for live CPU/memory/goroutine profiling")
	fs.StringVar(&cfg.ApiAddr, "api_addr", "", "HTTP listen address for management API (e.g. :8080); GET / serves embedded Control UI when built with Makefile target frontend; API under /api/v1/")
	fs.StringVar(&cfg.ApiToken, "api_token", "", "optional Bearer token required for all /api/v1 requests when set")
	fs.BoolVar(&cfg.ServerMode, "server", cfg.ServerMode, "systemd/long-run: OPTIONS UAS + API (default -api_addr :8080; default -p 5060 when -p omitted; SIP bind is -i/-p or JSON local_ip/local_port; -sn management unless -sf/-sn)")
	fs.StringVar(&cfg.CPUProfile, "cpuprofile", "", "write CPU profile to file at exit")
	fs.StringVar(&cfg.MemProfile, "memprofile", "", "write memory profile to file at exit")

	fs.BoolVar(&cfg.RTPSend, "rtp_send", false, "enable standalone RTP sender mode (no SIP scenario required)")
	fs.StringVar(&cfg.RTPAddr, "rtp_addr", "", "target RTP address host:port for standalone RTP sender")
	fs.IntVar(&cfg.RTPPT, "rtp_pt", 0, "RTP payload type for standalone sender (0 = PCMU; applies after -rtp_codec)")
	fs.StringVar(&cfg.RTPCodec, "rtp_codec", cfg.RTPCodec, "codec name for standalone RTP sender (e.g. PCMU/8000, PCMA/8000, G722/8000)")
	fs.IntVar(&cfg.RTPFreqMs, "rtp_freq", cfg.RTPFreqMs, "packet interval in milliseconds for standalone RTP sender")
	fs.IntVar(&cfg.RTPDurMs, "rtp_dur", 0, "total duration in milliseconds for standalone RTP sender (0 = run until interrupted)")
	fs.IntVar(&cfg.RTPChannels, "rtp_ch", cfg.RTPChannels, "number of audio channels for standalone RTP sender (1 = mono)")

	logAttrs := newKVFlag("log_attr")
	logHeaders := newKVFlag("log_otel_header")
	fs.BoolVar(&cfg.LogStdout, "log_stdout", false, "emit structured events to stderr in text form")
	fs.StringVar(&cfg.LogFileJSONL, "log_file_jsonl", "", "write one JSON event per line to this path")
	fs.StringVar(&cfg.LogOTELEndpoint, "log_otel_endpoint", cfg.LogOTELEndpoint, "OTLP collector endpoint (host:port for gRPC, full URL for HTTP)")
	fs.StringVar(&cfg.LogOTELProto, "log_otel_proto", cfg.LogOTELProto, "OTLP transport: grpc or http")
	fs.BoolVar(&cfg.LogOTELInsecure, "log_otel_insecure", cfg.LogOTELInsecure, "disable TLS for OTLP exporter")
	fs.Var(logHeaders, "log_otel_header", "OTLP header in key=value form (repeatable)")
	fs.Var(logAttrs, "log_attr", "extra resource attribute in key=value form (repeatable, e.g. -log_attr self_tag=NYC02)")
	fs.IntVar(&cfg.LogBufferSize, "log_buffer_size", cfg.LogBufferSize, "ring buffer capacity for the event logger")
	fs.StringVar(&cfg.LogLevel, "log_level", cfg.LogLevel, "minimum event level: debug|info|warn|error")

	if err := fs.Parse(normalizedArgs); err != nil {
		// flag.Parse already printed usage for -h / -help (undefined built-ins).
		return Config{}, err
	}
	providedFlags := make(map[string]struct{})
	fs.Visit(func(f *flag.Flag) {
		providedFlags[f.Name] = struct{}{}
	})
	cfg.TotalCallsSetExplicitly = false
	if _, ok := providedFlags["m"]; ok {
		cfg.TotalCallsSetExplicitly = true
	} else if profileTotalCallsExplicit {
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

	if err := applyServerModeIfEnabled(&cfg, providedFlags); err != nil {
		return Config{}, err
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
	if *recvByeFloorMS < 0 {
		return Config{}, errors.New("recv_bye_timeout_ms must be greater than or equal to zero")
	}
	cfg.RecvBYEFloorTO = time.Duration(*recvByeFloorMS) * time.Millisecond
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
	case "u1", "un", "ui", "t1", "tn", "l1", "ln", "s1", "sn", "sl", "cl", "cln":
	default:
		return Config{}, fmt.Errorf("unsupported transport %q", cfg.Transport)
	}
	if cfg.InjectionFile != "" && cfg.IPField < 0 {
		switch cfg.Transport {
		case "cl", "cln", "l1", "ln", "s1", "sl", "sn", "t1", "tn", "u1", "un", "ui":
			// SIPp-style: -inf without -ip_field does not load bind IPs from CSV; bind uses -i (for ui, see transport ui branch).
		default:
			return Config{}, errors.New("ip_field must be specified when inf is set")
		}
	}
	if cfg.IPField >= 0 && cfg.InjectionFile == "" {
		return Config{}, errors.New("inf must be specified when ip_field is set")
	}
	if cfg.Transport == "ui" {
		if cfg.InjectionFile == "" {
			return Config{}, errors.New("transport ui requires -inf")
		}
		if cfg.IPField >= 0 {
			sourceIPs, err := loadSourceIPsFromInjection(cfg.InjectionFile, cfg.IPField)
			if err != nil {
				return Config{}, err
			}
			cfg.UISourceIPs = sourceIPs
		} else {
			local := strings.TrimSpace(cfg.LocalIP)
			if local == "" {
				return Config{}, errors.New("transport ui without -ip_field requires -i <local bind address>")
			}
			cfg.UISourceIPs = []string{local}
		}
	} else if cfg.InjectionFile != "" || cfg.IPField >= 0 {
		switch cfg.Transport {
		case "cl", "cln", "l1", "ln":
			if cfg.InjectionFile != "" && cfg.IPField >= 0 {
				sourceIPs, err := loadSourceIPsFromInjection(cfg.InjectionFile, cfg.IPField)
				if err != nil {
					return Config{}, err
				}
				cfg.UISourceIPs = sourceIPs
			}
		case "t1", "tn", "u1", "un", "s1", "sn", "sl":
			if cfg.IPField >= 0 {
				return Config{}, errors.New("transport u1/un/t1/tn/s1/sn/sl does not support -ip_field with -inf (use -i for local bind; use -inf without -ip_field for CSV [fieldN] only)")
			}
		default:
			return Config{}, errors.New("inf and ip_field are only supported with transport ui, TLS client (cl/cln/l1/ln), or -inf alone with TCP/UDP client (t1/tn/u1/un)")
		}
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
	if cfg.SendMediaReport && !cfg.HEPHomerLakeRTCP && !cfg.HEPRawRTCP && !mediasink.MediaExporterExtensionRegistered() {
		return Config{}, errors.New("send_media_report requires hep_homer_lake_rtcp or hep_raw_rtcp unless a media exporter extension is registered (short JSON mode)")
	}
	if cfg.MaxSockets > 0 {
		switch cfg.Transport {
		case "un", "tn", "ln", "cln":
		default:
			return Config{}, errors.New("max_socket is only supported with un, tn, or ln transport")
		}
	}
	if cfg.MaxReconnect > 0 || cfg.ReconnectSleep > 0 || cfg.ReconnectClose {
		switch cfg.Transport {
		case "t1", "l1", "sl", "cl":
		default:
			return Config{}, errors.New("max_reconnect/reconnect_sleep/reconnect_close are only supported with t1 or l1 transport")
		}
	}

	cfg.LogAttrs = logAttrs.values
	cfg.LogOTELHeaders = logHeaders.values
	cfg.LogOTELProto = strings.ToLower(strings.TrimSpace(cfg.LogOTELProto))
	cfg.LogLevel = strings.ToLower(strings.TrimSpace(cfg.LogLevel))
	if cfg.LogBufferSize < 0 {
		return Config{}, errors.New("log_buffer_size must be greater than or equal to zero")
	}
	if cfg.LogOTELEndpoint != "" {
		switch cfg.LogOTELProto {
		case "grpc", "http":
		default:
			return Config{}, fmt.Errorf("log_otel_proto must be grpc or http, got %q", cfg.LogOTELProto)
		}
	}
	if cfg.LogLevel != "" {
		if _, ok := levelKnown(cfg.LogLevel); !ok {
			return Config{}, fmt.Errorf("log_level must be debug|info|warn|error, got %q", cfg.LogLevel)
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

	peek := make([]byte, infDelimiterPeekBytes)
	n, _ := file.Read(peek)
	if _, err := file.Seek(0, 0); err != nil {
		return nil, fmt.Errorf("unable to read inf file: %w", err)
	}

	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1
	reader.Comma = sniffInjectionCommaFromPeek(peek[:n])
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
		value = strings.TrimPrefix(value, "\ufeff")
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, fmt.Errorf("inf file %q row %d field %d: empty source IP", path, rowIndex+1, field)
		}
		if isSIPpInjectionFieldKeyword(value) {
			continue
		}
		if net.ParseIP(value) == nil {
			show := value
			if len(show) > 120 {
				show = show[:120] + "…"
			}
			return nil, fmt.Errorf("inf file %q row %d field %d: invalid source IP %q (set -ip_field to the column that contains the IP; SIPp-style ';' files are auto-detected)", path, rowIndex+1, field, show)
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

// isSIPpInjectionFieldKeyword matches SIPp -inf distribution modes that may
// appear in the injection column instead of an IP (first line of CSV).
func isSIPpInjectionFieldKeyword(value string) bool {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "SEQUENTIAL", "RANDOM", "USER":
		return true
	default:
		return false
	}
}

// sniffInjectionCommaFromPeek chooses ',' or ';' as CSV field separator.
// SIPp injection files often use ';'; a single comma-separated field would
// otherwise swallow the whole line as one cell.
func sniffInjectionCommaFromPeek(peek []byte) rune {
	s := string(peek)
	s = strings.TrimPrefix(s, "\ufeff")
	for _, raw := range strings.Split(s, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if isSIPpInjectionFieldKeyword(line) {
			continue
		}
		if !strings.ContainsAny(line, ",;") {
			continue
		}
		if strings.Count(line, ";") > strings.Count(line, ",") {
			return ';'
		}
		return ','
	}
	return ','
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

func applyServerModeIfEnabled(cfg *Config, flagProvided map[string]struct{}) error {
	if !cfg.ServerMode {
		return nil
	}
	if cfg.RTPSend {
		return errors.New("-server is incompatible with -rtp_send")
	}
	if strings.TrimSpace(cfg.ApiAddr) == "" {
		cfg.ApiAddr = ":8080"
	}
	if cfg.ScenarioFile == "" && (cfg.ScenarioName == "" || cfg.ScenarioName == "uac") {
		cfg.ScenarioName = "management"
	}
	if !cfg.TotalCallsSetExplicitly {
		cfg.TotalCallsSetExplicitly = true
		cfg.TotalCalls = 0
	}
	// UAS binds LocalIP:LocalPort (-i/-p). When -p is omitted, default to standard SIP (5060);
	// explicit -p 0 keeps ephemeral bind. (-rsa does not set the listen port.)
	if cfg.LocalPort == 0 {
		if _, ok := flagProvided["p"]; !ok {
			cfg.LocalPort = 5060
		}
	}
	return nil
}

func writeHelpPreamble(w io.Writer) {
	fmt.Fprintln(w, "Gossipper — SIP load generator (https://github.com/sipcapture/gossipper)")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Subcommands (run before any flags):")
	fmt.Fprintln(w, "  gossipper sipp [flags…]      SIPp-style entry; gossipper sipp -h shows a SIPp-oriented flag subset; do not use with tui/cli/server")
	fmt.Fprintln(w, "  gossipper cli                interactive line CLI: set flags, wizard, hint, run")
	fmt.Fprintln(w, "  gossipper tui                full-screen launcher / runtime UI")
	fmt.Fprintln(w, "  gossipper server [flags]     long-run management server (prepends -server; use with -config-server … or same flags as -server)")
	fmt.Fprintln(w, "  gossipper -server            same as `gossipper server` (systemd); SIP bind -i/-p (default -p 5060 if omitted); see examples/gossipper-server.service")
	fmt.Fprintln(w, "  gossipper pcap2scenario ...  PCAP → XML scenarios")
	fmt.Fprintln(w, "  gossipper report-html ...  summary JSON → standalone HTML report")
	fmt.Fprintln(w, "  gossipper summary-to-pdf ...  HTML → PDF (optional -tags pdf or Chromium in PATH)")
	fmt.Fprintln(w, "  gossipper profile            JSON run-profile flags (-config, -run-alias, …); gossipper profile -h")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "See also: docs/cli.md, docs/interactive-shell.md, docs/tui.md")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Flags:")
}

func writeServerHelpPreamble(w io.Writer) {
	fmt.Fprintln(w, "Gossipper — server / management mode (-server or gossipper server)")
	fmt.Fprintln(w, "SIP bind: -i / -p or JSON local_ip / local_port with -config-server.")
	fmt.Fprintln(w, "HTTP API / Control UI: -api_addr, optional -api_token. See examples/gossipper-server.service")
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

// kvFlag is a flag.Value that accumulates repeated -key value=foo arguments
// into a map. Used by -log_attr and -log_otel_header.
type kvFlag struct {
	name   string
	values map[string]string
}

func newKVFlag(name string) *kvFlag {
	return &kvFlag{name: name, values: make(map[string]string)}
}

func (f *kvFlag) String() string {
	if f == nil || len(f.values) == 0 {
		return ""
	}
	parts := make([]string, 0, len(f.values))
	for k, v := range f.values {
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, ",")
}

func (f *kvFlag) Set(raw string) error {
	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			return fmt.Errorf("-%s expects key=value, got %q", f.name, entry)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" {
			return fmt.Errorf("-%s entry has empty key: %q", f.name, entry)
		}
		f.values[key] = value
	}
	return nil
}

// levelKnown reports whether name is a valid log level. Kept here so the
// cli package does not depend on internal/eventlog directly during parse.
func levelKnown(name string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "info":
		return "info", true
	case "debug":
		return "debug", true
	case "warn", "warning":
		return "warn", true
	case "error", "err":
		return "error", true
	default:
		return "", false
	}
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
