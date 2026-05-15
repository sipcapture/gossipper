package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const maxCompositeProfiles = 16

// compositeFlatTopReserved are keys that never participate in shallow-merge defaults
// for the server object and each client profile.
var compositeFlatTopReserved = map[string]struct{}{
	"workloads": {},
	"aliases":   {},
	"server":    {},
	"clients":   {},
	"client":    {},
}

func globalBaseForCompositeFlat(top map[string]json.RawMessage) map[string]json.RawMessage {
	global := make(map[string]json.RawMessage, len(top))
	for k, v := range top {
		if _, skip := compositeFlatTopReserved[k]; skip {
			continue
		}
		global[k] = v
	}
	return global
}

func deleteCompositeNoiseKeys(m map[string]json.RawMessage) {
	for k := range compositeFlatTopReserved {
		delete(m, k)
	}
}

// compositeProfileRawEntries builds merged JSON blobs: first the management server profile,
// then each optional client profile. isComposite is true only when top-level "server" is present.
func compositeProfileRawEntries(top map[string]json.RawMessage, global map[string]json.RawMessage) (entries []json.RawMessage, isComposite bool, err error) {
	_, hasServer := top["server"]
	_, hasClients := top["clients"]
	_, hasClient := top["client"]

	if hasClients && hasClient {
		return nil, false, errors.New(`flat config: use either "clients" (array) or "client" (object or array), not both`)
	}
	if hasClients || hasClient {
		if !hasServer {
			return nil, false, errors.New(`flat config: "clients" / "client" requires a top-level "server" object`)
		}
	}

	if !hasServer {
		return nil, false, nil
	}

	var serverObj map[string]json.RawMessage
	if err := json.Unmarshal(top["server"], &serverObj); err != nil {
		return nil, false, fmt.Errorf("flat config server: %w", err)
	}
	mergedServer := shallowMergeRawJSON(global, serverObj)
	deleteCompositeNoiseKeys(mergedServer)
	first, err := json.Marshal(mergedServer)
	if err != nil {
		return nil, false, err
	}
	out := []json.RawMessage{first}

	appendClient := func(raw json.RawMessage) error {
		var cm map[string]json.RawMessage
		if err := json.Unmarshal(raw, &cm); err != nil {
			return fmt.Errorf("flat config clients: %w", err)
		}
		merged := shallowMergeRawJSON(global, cm)
		deleteCompositeNoiseKeys(merged)
		b, err := json.Marshal(merged)
		if err != nil {
			return err
		}
		out = append(out, b)
		return nil
	}

	if hasClients {
		var arr []json.RawMessage
		if err := json.Unmarshal(top["clients"], &arr); err != nil {
			return nil, false, fmt.Errorf("flat config clients: %w", err)
		}
		for i, cr := range arr {
			if err := appendClient(cr); err != nil {
				return nil, false, fmt.Errorf("flat config clients[%d]: %w", i, err)
			}
		}
	}
	if hasClient {
		raw := top["client"]
		raw = json.RawMessage(bytes.TrimSpace([]byte(raw)))
		if len(raw) > 0 && raw[0] == '[' {
			var arr []json.RawMessage
			if err := json.Unmarshal(raw, &arr); err != nil {
				return nil, false, fmt.Errorf("flat config client array: %w", err)
			}
			for i, cr := range arr {
				if err := appendClient(cr); err != nil {
					return nil, false, fmt.Errorf("flat config client[%d]: %w", i, err)
				}
			}
		} else {
			if err := appendClient(raw); err != nil {
				return nil, false, fmt.Errorf("flat config client: %w", err)
			}
		}
	}

	if len(out) > maxCompositeProfiles {
		return nil, false, fmt.Errorf("flat config: at most %d profiles (server + clients) allowed", maxCompositeProfiles)
	}
	return out, true, nil
}

// TryLoadCompositeFlatJSON loads flat JSON with top-level "server": { ... } and optional
// "clients": [ ... ] / "client": { } | [ ... ]. Root keys (api_addr, hep_*, …) shallow-merge
// into each profile; reserved keys server, clients, client, workloads, aliases never merge in.
// Returns ok=false for legacy single-object files (no top-level "server").
func TryLoadCompositeFlatJSON(configPath string, data []byte) (primary Config, joined []JoinedClient, extraArgs [][]string, ok bool, err error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return Config{}, nil, nil, false, err
	}
	if _, has := top["aliases"]; has {
		return Config{}, nil, nil, false, errors.New(`flat config: file contains "aliases" (run profile layout); use gossipper -config <path> -run-alias <name>`)
	}
	if _, legacy := top["workloads"]; legacy {
		return Config{}, nil, nil, false, errors.New(`flat config: "workloads" is no longer supported; use "server" and "clients" (see docs/run-profile.md)`)
	}
	global := globalBaseForCompositeFlat(top)
	profileEntries, isComposite, err := compositeProfileRawEntries(top, global)
	if err != nil {
		return Config{}, nil, nil, false, err
	}
	if !isComposite {
		return Config{}, nil, nil, false, nil
	}

	primary, joined, extraArgs, err = buildCompositeProfiles(configPath, global, profileEntries)
	if err != nil {
		return Config{}, nil, nil, false, err
	}
	return primary, joined, extraArgs, true, nil
}

func buildCompositeProfiles(configPath string, global map[string]json.RawMessage, profileEntries []json.RawMessage) (primary Config, joined []JoinedClient, extraArgs [][]string, err error) {
	configDir := filepath.Dir(configPath)
	type built struct {
		id    string
		cfg   Config
		extra []string
	}
	builtList := make([]built, 0, len(profileEntries))
	for i, entryRaw := range profileEntries {
		var entry map[string]json.RawMessage
		if err := json.Unmarshal(entryRaw, &entry); err != nil {
			return Config{}, nil, nil, fmt.Errorf("flat config composite[%d]: %w", i, err)
		}
		id := fmt.Sprintf("w%d", i)
		if raw, ok := entry["id"]; ok {
			var s string
			if err := json.Unmarshal(raw, &s); err != nil {
				return Config{}, nil, nil, fmt.Errorf("flat config composite[%d].id: %w", i, err)
			}
			s = strings.TrimSpace(s)
			if s != "" {
				id = s
			}
		}
		merged := shallowMergeRawJSON(global, entry)
		deleteCompositeNoiseKeys(merged)
		mergedBytes, err := json.Marshal(merged)
		if err != nil {
			return Config{}, nil, nil, err
		}
		cfg := DefaultConfig()
		var topCheck map[string]json.RawMessage
		if err := json.Unmarshal(mergedBytes, &topCheck); err != nil {
			return Config{}, nil, nil, err
		}
		if _, has := topCheck["aliases"]; has {
			return Config{}, nil, nil, fmt.Errorf("flat config composite[%d]: must not contain aliases", i)
		}
		var spec runSpec
		if err := json.Unmarshal(mergedBytes, &spec); err != nil {
			return Config{}, nil, nil, fmt.Errorf("flat config composite[%d]: %w", i, err)
		}
		if err := applyRunSpec(&cfg, &spec, configDir); err != nil {
			return Config{}, nil, nil, fmt.Errorf("flat config composite[%d]: %w", i, err)
		}
		mgmt, err := inferServerFlatManagementFromJSON(mergedBytes)
		if err != nil {
			return Config{}, nil, nil, err
		}
		if mgmt {
			cfg.ServerMode = true
		} else {
			cfg.ServerMode = false
		}
		if err := applyServerModeIfEnabled(&cfg, map[string]struct{}{}); err != nil {
			return Config{}, nil, nil, fmt.Errorf("flat config composite[%d]: %w", i, err)
		}
		ex := append([]string(nil), spec.ExtraArgs...)
		builtList = append(builtList, built{id: id, cfg: cfg, extra: ex})
	}

	sort.SliceStable(builtList, func(i, j int) bool {
		if builtList[i].cfg.ServerMode == builtList[j].cfg.ServerMode {
			return i < j
		}
		return builtList[i].cfg.ServerMode && !builtList[j].cfg.ServerMode
	})

	seenID := map[string]struct{}{}
	for _, b := range builtList {
		if _, dup := seenID[b.id]; dup {
			return Config{}, nil, nil, fmt.Errorf("flat config composite: duplicate id %q", b.id)
		}
		seenID[b.id] = struct{}{}
	}

	primary = builtList[0].cfg
	if !primary.ServerMode {
		return Config{}, nil, nil, errors.New("flat config composite: server profile must be management (listener); check role, listeners, api_addr, or scenario_name")
	}
	primary.ServerProfileID = builtList[0].id
	extras := [][]string{builtList[0].extra}
	for i := 1; i < len(builtList); i++ {
		c := builtList[i].cfg
		c.ApiAddr = ""
		c.ApiToken = ""
		c.Auth = AuthConfig{}
		joined = append(joined, JoinedClient{ID: builtList[i].id, Config: c})
		extras = append(extras, builtList[i].extra)
	}

	if err := validateJoinedClientBinds(primary, joined); err != nil {
		return Config{}, nil, nil, err
	}

	return primary, joined, extras, nil
}

func shallowMergeRawJSON(base, override map[string]json.RawMessage) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(base)+len(override))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range override {
		if k == "comment" {
			continue
		}
		out[k] = v
	}
	return out
}

func validateJoinedClientBinds(primary Config, joined []JoinedClient) error {
	type bindKey struct {
		t  string
		ip string
		p  int
	}
	seen := map[bindKey]struct{}{}
	add := func(c Config) error {
		if len(c.ServerListeners) > 0 {
			for _, ln := range c.ServerListeners {
				k := bindKey{t: strings.TrimSpace(ln.Transport), ip: strings.TrimSpace(ln.LocalIP), p: ln.LocalPort}
				if _, ok := seen[k]; ok {
					return fmt.Errorf("duplicate SIP bind %s %s:%d across profiles", k.t, k.ip, k.p)
				}
				seen[k] = struct{}{}
			}
			return nil
		}
		if c.ServerMode {
			k := bindKey{t: strings.TrimSpace(c.Transport), ip: strings.TrimSpace(c.LocalIP), p: c.LocalPort}
			if _, ok := seen[k]; ok {
				return fmt.Errorf("duplicate SIP bind %s %s:%d across profiles", k.t, k.ip, k.p)
			}
			seen[k] = struct{}{}
		}
		return nil
	}
	if err := add(primary); err != nil {
		return err
	}
	for _, j := range joined {
		if err := add(j.Config); err != nil {
			return err
		}
	}
	return nil
}

// LoadServerFlatOrComposite reads a gossipper server -config JSON file: either legacy single-object
// flat layout, or composite layout with top-level "server" (+ optional "clients").
func LoadServerFlatOrComposite(configPath string) (cfg Config, joined []JoinedClient, extra []string, err error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return Config{}, nil, nil, fmt.Errorf("flat config: read file: %w", err)
	}
	primary, joined, extrasList, ok, err := TryLoadCompositeFlatJSON(configPath, data)
	if err != nil {
		return Config{}, nil, nil, err
	}
	if ok {
		var flat []string
		for _, e := range extrasList {
			flat = append(flat, e...)
		}
		return primary, joined, flat, nil
	}
	management, err := InferServerFlatManagement(configPath)
	if err != nil {
		return Config{}, nil, nil, err
	}
	cfg = DefaultConfig()
	if management {
		extra, err = LoadAndApplyServerConfig(&cfg, configPath)
		if err != nil {
			return Config{}, nil, nil, err
		}
		cfg.ServerMode = true
	} else {
		extra, err = LoadAndApplyClientConfig(&cfg, configPath)
		if err != nil {
			return Config{}, nil, nil, err
		}
	}
	return cfg, nil, extra, nil
}
