package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ErrListAliases is returned from Parse after successfully printing alias names (-list-aliases).
// The caller should exit with code 0 without treating it as a failure.
var ErrListAliases = errors.New("cli: -list-aliases complete")

// runProfileMeta holds parsed -config / -run-alias / -list-aliases from argv.
type runProfileMeta struct {
	ConfigPath  string
	RunAlias    string
	ListAliases bool
}

// runSpec is one alias entry in a gossipper run profile JSON file.
// Field names are stable snake_case; optional pointers mean "leave Config default".
type runSpec struct {
	ScenarioFile                   *string  `json:"scenario_file,omitempty"`
	ScenarioName                   *string  `json:"scenario_name,omitempty"`
	Service                        *string  `json:"service,omitempty"`
	Transport                      *string  `json:"transport,omitempty"`
	LocalIP                        *string  `json:"local_ip,omitempty"`
	LocalPort                      *int     `json:"local_port,omitempty"`
	RemoteAddr                     *string  `json:"remote_addr,omitempty"`
	AuthUsername                   *string  `json:"auth_username,omitempty"`
	AuthPassword                   *string  `json:"auth_password,omitempty"`
	Rate                           *float64 `json:"rate,omitempty"`
	MaxConcurrent                  *int     `json:"max_concurrent,omitempty"`
	TotalCalls                     *int     `json:"total_calls,omitempty"`
	Users                          *int     `json:"users,omitempty"`
	HEPAddr                        *string  `json:"hep_addr,omitempty"`
	HEPCaptureID                   *uint32  `json:"hep_capture_id,omitempty"`
	HEPPassword                    *string  `json:"hep_password,omitempty"`
	HEPRawRTCP                     *bool    `json:"hep_raw_rtcp,omitempty"`
	HEPHomerLakeRTCP               *bool    `json:"hep_homer_lake_rtcp,omitempty"`
	SendMediaReport                *bool    `json:"send_media_report,omitempty"`
	SummaryJSON                    *string  `json:"summary_json,omitempty"`
	SummaryHTML                    *string  `json:"summary_html,omitempty"`
	RecordWAVDir                   *string  `json:"record_wav_dir,omitempty"`
	RecordWAVDuplex                *bool    `json:"record_wav_duplex,omitempty"`
	CallRecordsJSONL               *string  `json:"call_records_jsonl,omitempty"`
	SipFrom                        *string  `json:"sip_from,omitempty"`
	SipPAI                         *string  `json:"sip_pai,omitempty"`
	SipProvider                    *string  `json:"sip_provider,omitempty"`
	SipExtraHeaders                []string `json:"sip_extra_headers,omitempty"`
	TraceMessages                  *bool    `json:"trace_msg,omitempty"`
	StatPrintPeriod                *string  `json:"stat_period,omitempty"`
	InjectionFile                  *string  `json:"injection_file,omitempty"`
	IPField                        *int     `json:"ip_field,omitempty"`
	HealthMaxRTCPFractionLost      *float64 `json:"health_max_rtcp_fraction_lost,omitempty"`
	HealthMaxRTCPJitterTS          *int     `json:"health_max_rtcp_jitter_ts,omitempty"`
	HealthMinRTPPacketsRecv        *int     `json:"health_min_rtp_packets_recv,omitempty"`
	HealthMinRTPPacketsRecvPerCall *int     `json:"health_min_rtp_packets_recv_per_call,omitempty"`
	LogOTELEndpoint                *string  `json:"log_otel_endpoint,omitempty"`
	LogOTELProto                   *string  `json:"log_otel_proto,omitempty"`
	LogOTELInsecure                *bool    `json:"log_otel_insecure,omitempty"`
	ApiAddr                        *string  `json:"api_addr,omitempty"`
	ApiToken                       *string  `json:"api_token,omitempty"`
	ExtraArgs                      []string `json:"extra_args,omitempty"`
	PCAPLink                       *string  `json:"pcap_link,omitempty"`
}

type runProfileFile struct {
	Aliases map[string]runSpec `json:"aliases"`
}

func printRunProfileAliases(configPath string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("run profile: read config: %w", err)
	}
	var f runProfileFile
	if err := json.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("run profile: %w", err)
	}
	if len(f.Aliases) == 0 {
		fmt.Fprintln(os.Stdout, "(no aliases)")
		return nil
	}
	names := make([]string, 0, len(f.Aliases))
	for name := range f.Aliases {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Fprintln(os.Stdout, n)
	}
	return nil
}

// LoadAndApplyRunProfile loads alias from JSON and applies fields to cfg.
// configDir is the directory of the JSON file (for relative paths).
func LoadAndApplyRunProfile(cfg *Config, configPath, alias string) ([]string, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("run profile: read config: %w", err)
	}
	var f runProfileFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("run profile: %w", err)
	}
	spec, ok := f.Aliases[alias]
	if !ok {
		return nil, fmt.Errorf("run profile: unknown alias %q", alias)
	}
	configDir := filepath.Dir(configPath)
	if err := applyRunSpec(cfg, &spec, configDir); err != nil {
		return nil, err
	}
	return append([]string(nil), spec.ExtraArgs...), nil
}

func parseRunProfileMeta(args []string) ([]string, runProfileMeta, error) {
	var m runProfileMeta
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-list-aliases" || a == "--list-aliases":
			m.ListAliases = true
		case a == "-config" || a == "--config":
			if i+1 >= len(args) {
				return nil, m, errors.New("-config requires a path")
			}
			i++
			m.ConfigPath = args[i]
		case strings.HasPrefix(a, "-config="):
			m.ConfigPath = strings.TrimPrefix(a, "-config=")
		case strings.HasPrefix(a, "--config="):
			m.ConfigPath = strings.TrimPrefix(a, "--config=")
		case a == "-run-alias" || a == "--run-alias":
			if i+1 >= len(args) {
				return nil, m, errors.New("-run-alias requires a name")
			}
			i++
			m.RunAlias = args[i]
		case strings.HasPrefix(a, "-run-alias="):
			m.RunAlias = strings.TrimPrefix(a, "-run-alias=")
		case strings.HasPrefix(a, "--run-alias="):
			m.RunAlias = strings.TrimPrefix(a, "--run-alias=")
		default:
			out = append(out, a)
		}
	}
	return out, m, nil
}

func applyRunSpec(cfg *Config, spec *runSpec, configDir string) error {
	if spec.ScenarioFile != nil {
		p := strings.TrimSpace(*spec.ScenarioFile)
		if p == "" {
			cfg.ScenarioFile = ""
		} else if filepath.IsAbs(p) {
			cfg.ScenarioFile = filepath.Clean(p)
		} else {
			cfg.ScenarioFile = filepath.Clean(filepath.Join(configDir, p))
		}
	}
	if spec.ScenarioName != nil {
		cfg.ScenarioName = strings.TrimSpace(*spec.ScenarioName)
	}
	if spec.Service != nil {
		cfg.Service = strings.TrimSpace(*spec.Service)
	}
	if spec.Transport != nil {
		cfg.Transport = strings.TrimSpace(*spec.Transport)
	}
	if spec.LocalIP != nil {
		cfg.LocalIP = strings.TrimSpace(*spec.LocalIP)
	}
	if spec.LocalPort != nil {
		cfg.LocalPort = *spec.LocalPort
	}
	if spec.RemoteAddr != nil {
		host, port, err := splitHostPort(strings.TrimSpace(*spec.RemoteAddr))
		if err != nil {
			return fmt.Errorf("run profile remote_addr: %w", err)
		}
		cfg.RemoteHost = host
		cfg.RemotePort = port
	}
	if spec.AuthUsername != nil {
		cfg.AuthUsername = strings.TrimSpace(*spec.AuthUsername)
	}
	if spec.AuthPassword != nil {
		cfg.AuthPassword = *spec.AuthPassword
	}
	if spec.Rate != nil {
		cfg.Rate = *spec.Rate
	}
	if spec.MaxConcurrent != nil {
		cfg.MaxConcurrent = *spec.MaxConcurrent
	}
	if spec.TotalCalls != nil {
		cfg.TotalCalls = *spec.TotalCalls
		cfg.TotalCallsSetExplicitly = true
	}
	if spec.Users != nil {
		cfg.Users = *spec.Users
	}
	if spec.HEPAddr != nil {
		cfg.HEPAddr = strings.TrimSpace(*spec.HEPAddr)
	}
	if spec.HEPCaptureID != nil {
		cfg.HEPCaptureID = *spec.HEPCaptureID
	}
	if spec.HEPPassword != nil {
		cfg.HEPPassword = *spec.HEPPassword
	}
	if spec.HEPRawRTCP != nil {
		cfg.HEPRawRTCP = *spec.HEPRawRTCP
	}
	if spec.HEPHomerLakeRTCP != nil {
		cfg.HEPHomerLakeRTCP = *spec.HEPHomerLakeRTCP
	}
	if spec.SendMediaReport != nil {
		cfg.SendMediaReport = *spec.SendMediaReport
	}
	if spec.SummaryJSON != nil {
		cfg.SummaryJSON = strings.TrimSpace(*spec.SummaryJSON)
	}
	if spec.SummaryHTML != nil {
		cfg.SummaryHTML = strings.TrimSpace(*spec.SummaryHTML)
	}
	if spec.RecordWAVDir != nil {
		cfg.RecordWAVDir = strings.TrimSpace(*spec.RecordWAVDir)
	}
	if spec.RecordWAVDuplex != nil {
		cfg.RecordWAVDuplex = *spec.RecordWAVDuplex
	}
	if spec.CallRecordsJSONL != nil {
		cfg.CallRecordsJSONL = strings.TrimSpace(*spec.CallRecordsJSONL)
	}
	if spec.SipFrom != nil {
		cfg.SipFrom = strings.TrimSpace(*spec.SipFrom)
	}
	if spec.SipPAI != nil {
		cfg.SipPAI = strings.TrimSpace(*spec.SipPAI)
	}
	if spec.SipProvider != nil {
		cfg.SipProvider = strings.TrimSpace(*spec.SipProvider)
	}
	if len(spec.SipExtraHeaders) > 0 {
		cfg.SipExtraHeaders = append([]string(nil), spec.SipExtraHeaders...)
	}
	if spec.TraceMessages != nil {
		cfg.TraceMessages = *spec.TraceMessages
	}
	if spec.StatPrintPeriod != nil {
		s := strings.TrimSpace(*spec.StatPrintPeriod)
		if s == "" {
			cfg.StatPrintPeriod = 0
		} else {
			d, err := time.ParseDuration(s)
			if err != nil {
				return fmt.Errorf("run profile stat_period: %w", err)
			}
			cfg.StatPrintPeriod = d
		}
	}
	if spec.InjectionFile != nil {
		p := strings.TrimSpace(*spec.InjectionFile)
		if p == "" {
			cfg.InjectionFile = ""
		} else if filepath.IsAbs(p) {
			cfg.InjectionFile = filepath.Clean(p)
		} else {
			cfg.InjectionFile = filepath.Clean(filepath.Join(configDir, p))
		}
	}
	if spec.IPField != nil {
		cfg.IPField = *spec.IPField
	}
	if spec.HealthMaxRTCPFractionLost != nil {
		cfg.HealthMaxRTCPFractionLost = *spec.HealthMaxRTCPFractionLost
	}
	if spec.HealthMaxRTCPJitterTS != nil {
		cfg.HealthMaxRTCPJitterTS = *spec.HealthMaxRTCPJitterTS
	}
	if spec.HealthMinRTPPacketsRecv != nil {
		cfg.HealthMinRTPPacketsRecv = *spec.HealthMinRTPPacketsRecv
	}
	if spec.HealthMinRTPPacketsRecvPerCall != nil {
		cfg.HealthMinRTPPacketsRecvPerCall = *spec.HealthMinRTPPacketsRecvPerCall
	}
	if spec.LogOTELEndpoint != nil {
		cfg.LogOTELEndpoint = strings.TrimSpace(*spec.LogOTELEndpoint)
	}
	if spec.LogOTELProto != nil {
		cfg.LogOTELProto = strings.TrimSpace(*spec.LogOTELProto)
	}
	if spec.LogOTELInsecure != nil {
		cfg.LogOTELInsecure = *spec.LogOTELInsecure
	}
	if spec.ApiAddr != nil {
		cfg.ApiAddr = strings.TrimSpace(*spec.ApiAddr)
	}
	if spec.ApiToken != nil {
		cfg.ApiToken = *spec.ApiToken
	}
	if spec.PCAPLink != nil {
		cfg.PCAPLinkLayer = strings.TrimSpace(*spec.PCAPLink)
	}
	return nil
}
