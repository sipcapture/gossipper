package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strings"
)

const maxClientSnippetBytes = 1 << 20

// forbiddenClientSnippetKeys are top-level JSON keys rejected for POST /api/v1/clients snippets
// (they belong to composite layout or management HTTP listener).
var forbiddenClientSnippetKeys = map[string]struct{}{
	"aliases": {}, "workloads": {}, "server": {}, "clients": {}, "client": {},
	"listeners": {}, "api_addr": {}, "api_token": {}, "ui_data_dir": {}, "legacy_api_v1": {}, "auth": {},
}

// ApplyClientSnippetFromJSON builds a load (UAC) Config from a JSON object (same snake_case keys as flat
// client presets / run profile alias body). Shared telemetry (HEP, OTLP, tool version, …) is copied from parent.
func ApplyClientSnippetFromJSON(parent Config, data []byte, configDir string) (Config, error) {
	if len(data) > maxClientSnippetBytes {
		return Config{}, errors.New("client snippet too large")
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return Config{}, err
	}
	for k := range top {
		lk := strings.ToLower(strings.TrimSpace(k))
		if _, bad := forbiddenClientSnippetKeys[lk]; bad {
			return Config{}, fmt.Errorf("field %q is not allowed in dynamic client snippet", k)
		}
	}
	cfg := DefaultConfig()
	copyTelemetryFromManagementParent(&cfg, parent)
	var spec runSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		return Config{}, err
	}
	if err := applyRunSpec(&cfg, &spec, configDir); err != nil {
		return Config{}, err
	}
	cfg.ServerMode = false
	cfg.ApiAddr = ""
	cfg.ApiToken = ""
	cfg.ServerListeners = nil
	cfg.JoinedClients = nil
	cfg.ServerProfileID = ""
	return cfg, nil
}

func copyTelemetryFromManagementParent(dst *Config, src Config) {
	dst.ToolVersion = src.ToolVersion
	dst.HEPAddr = src.HEPAddr
	dst.HEPCaptureID = src.HEPCaptureID
	dst.HEPPassword = src.HEPPassword
	dst.HEPRawRTCP = src.HEPRawRTCP
	dst.HEPHomerLakeRTCP = src.HEPHomerLakeRTCP
	dst.SendMediaReport = src.SendMediaReport
	dst.LogStdout = src.LogStdout
	dst.LogFileJSONL = src.LogFileJSONL
	dst.LogOTELEndpoint = src.LogOTELEndpoint
	dst.LogOTELProto = src.LogOTELProto
	dst.LogOTELInsecure = src.LogOTELInsecure
	dst.LogLevel = src.LogLevel
	dst.LogBufferSize = src.LogBufferSize
	dst.GlobalTimeout = src.GlobalTimeout
	if src.LogAttrs != nil {
		dst.LogAttrs = maps.Clone(src.LogAttrs)
	}
	if src.LogOTELHeaders != nil {
		dst.LogOTELHeaders = maps.Clone(src.LogOTELHeaders)
	}
}
