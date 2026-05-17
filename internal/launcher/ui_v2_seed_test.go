package launcher

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sipcapture/gossipper/internal/cli"
	"github.com/sipcapture/gossipper/internal/uistore"
)

func TestSeedProfilesFromConfig_PopulatesEmptyStore(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store, err := uistore.Open(dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	cfg := cli.Config{
		ServerMode:      true,
		ScenarioName:    "management",
		Transport:       "u1",
		LocalIP:         "0.0.0.0",
		LocalPort:       5060,
		MaxConcurrent:   2048,
		ServerProfileID: "management",
		JoinedClients: []cli.JoinedClient{
			{ID: "load-udp", Config: cli.Config{
				Transport:    "u1",
				LocalIP:      "0.0.0.0",
				LocalPort:    15060,
				RemoteHost:   "127.0.0.1",
				RemotePort:   5060,
				Rate:         5,
				ScenarioName: "uac",
			}},
		},
	}

	if err := seedProfilesFromConfig(store, cfg); err != nil {
		t.Fatalf("seed: %v", err)
	}

	servers, err := store.ListServerProfiles()
	if err != nil {
		t.Fatalf("list servers: %v", err)
	}
	if len(servers) != 1 || servers[0].ID != "management" {
		t.Fatalf("want one 'management' server profile, got %+v", servers)
	}
	if len(servers[0].Transports) != 1 || servers[0].Transports[0].Transport != "u1" {
		t.Fatalf("server transports mismatch: %+v", servers[0].Transports)
	}

	clients, err := store.ListClientProfiles()
	if err != nil {
		t.Fatalf("list clients: %v", err)
	}
	if len(clients) != 1 || clients[0].ID != "load-udp" {
		t.Fatalf("want one 'load-udp' client profile, got %+v", clients)
	}
	if clients[0].RemoteIP != "127.0.0.1" || clients[0].RemotePort != 5060 {
		t.Fatalf("client remote mismatch: %+v", clients[0])
	}

	// Files should be on disk.
	for _, p := range []string{
		filepath.Join(store.Layout().ServersDir(), "management.json"),
		filepath.Join(store.Layout().ClientsDir(), "load-udp.json"),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("expected seeded file %s: %v", p, err)
		}
	}

	// Second call must be a no-op (store now non-empty).
	cfg2 := cfg
	cfg2.JoinedClients = nil
	cfg2.ServerProfileID = "should-not-overwrite"
	if err := seedProfilesFromConfig(store, cfg2); err != nil {
		t.Fatalf("seed idempotency: %v", err)
	}
	servers2, _ := store.ListServerProfiles()
	if len(servers2) != 1 || servers2[0].ID != "management" {
		t.Fatalf("seed must not overwrite existing profiles: %+v", servers2)
	}
}

func TestSeedProfilesFromConfig_SkipsWhenAlreadySeeded(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store, err := uistore.Open(dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if _, err := store.PutServerProfile(uistore.ServerProfile{
		ID:         "existing",
		Name:       "existing",
		Transports: []uistore.TransportSpec{{Transport: "u1", LocalPort: 5060, Enabled: true}},
	}, true); err != nil {
		t.Fatalf("seed existing: %v", err)
	}

	cfg := cli.Config{
		ServerMode:      true,
		ServerProfileID: "should-not-be-added",
		Transport:       "u1",
		LocalIP:         "0.0.0.0",
		LocalPort:       5060,
	}
	if err := seedProfilesFromConfig(store, cfg); err != nil {
		t.Fatalf("seed: %v", err)
	}
	servers, _ := store.ListServerProfiles()
	if len(servers) != 1 || servers[0].ID != "existing" {
		t.Fatalf("non-empty store must skip seeding, got %+v", servers)
	}
}
