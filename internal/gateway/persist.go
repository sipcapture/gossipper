package gateway

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const persistFileName = "gateway.json"

type persistFile struct {
	Profiles []Config `json:"profiles"`
}

// PersistPath is {uiDataDir}/gateway.json.
func PersistPath(uiDataDir string) string {
	return filepath.Join(uiDataDir, persistFileName)
}

// DecodePersist parses gateway.json: either {profiles:[...]} or a legacy single Config.
// Profiles that omit "enabled" default to on (legacy / CLI seed).
func DecodePersist(data []byte) ([]Config, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, nil
	}
	var probe struct {
		Profiles json.RawMessage `json:"profiles"`
	}
	if err := json.Unmarshal(data, &probe); err == nil && len(bytes.TrimSpace(probe.Profiles)) > 0 && probe.Profiles[0] == '[' {
		var raws []json.RawMessage
		if err := json.Unmarshal(probe.Profiles, &raws); err != nil {
			return nil, err
		}
		out := make([]Config, 0, len(raws))
		for _, raw := range raws {
			cfg, err := decodeProfile(raw, false)
			if err != nil {
				return nil, err
			}
			out = append(out, cfg)
		}
		return out, nil
	}
	cfg, err := decodeProfile(data, true)
	if err != nil {
		return nil, err
	}
	return []Config{cfg}, nil
}

// DecodeConfig unmarshals one profile. Omitted "enabled" defaults to true.
func DecodeConfig(raw []byte) (Config, error) {
	return decodeProfile(raw, false)
}

func decodeProfile(raw []byte, legacyObject bool) (Config, error) {
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, err
	}
	if !bytes.Contains(raw, []byte(`"enabled"`)) {
		cfg.Enabled = true
	}
	if legacyObject && cfg.ID == "" && cfg.Name == "" {
		cfg.Name = "Gateway"
	}
	EnsureIdentity(&cfg)
	return cfg, nil
}

// LoadPersist reads gateway.json. Missing file is not an error (ok=false).
func LoadPersist(uiDataDir string) (profiles []Config, ok bool, err error) {
	path := PersistPath(uiDataDir)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}
	profiles, err = DecodePersist(data)
	if err != nil {
		return nil, false, err
	}
	return profiles, true, nil
}

// SavePersist writes {profiles:[...]} with 0600 (contains passwords).
func SavePersist(uiDataDir string, profiles []Config) error {
	if strings.TrimSpace(uiDataDir) == "" {
		return errors.New("gateway: ui_data_dir is required to persist")
	}
	out := make([]Config, 0, len(profiles))
	for i := range profiles {
		cfg := profiles[i]
		EnsureIdentity(&cfg)
		out = append(out, cfg)
	}
	if err := os.MkdirAll(uiDataDir, 0o750); err != nil {
		return err
	}
	data, err := json.MarshalIndent(persistFile{Profiles: out}, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := PersistPath(uiDataDir) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, PersistPath(uiDataDir))
}

// SeedPersistIfMissing writes seed when the persist file does not exist.
func SeedPersistIfMissing(uiDataDir string, seed []Config) error {
	if strings.TrimSpace(uiDataDir) == "" {
		return nil
	}
	_, ok, err := LoadPersist(uiDataDir)
	if err != nil || ok {
		return err
	}
	cleaned := make([]Config, 0, len(seed))
	for i := range seed {
		cfg := seed[i]
		EnsureIdentity(&cfg)
		if cfg.Domain == "" && cfg.Addr == "" && cfg.Username == "" && cfg.Name == "Gateway" && !cfg.Register {
			continue
		}
		cleaned = append(cleaned, cfg)
	}
	if len(cleaned) == 0 {
		return nil
	}
	return SavePersist(uiDataDir, cleaned)
}
