package gateway

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDecodePersistLegacyObject(t *testing.T) {
	t.Parallel()
	got, err := DecodePersist([]byte(`{"domain":"pbx.local","addr":"10.0.0.1:5060","username":"1001","password":"s","register":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len=%d", len(got))
	}
	if !got[0].Enabled || got[0].Name != "Gateway" || got[0].ID == "" {
		t.Fatalf("legacy=%+v", got[0])
	}
}

func TestDecodePersistProfilesOmittedEnabled(t *testing.T) {
	t.Parallel()
	got, err := DecodePersist([]byte(`{"profiles":[{"id":"gw-a","name":"Desk","domain":"pbx.local","addr":"10.0.0.1:5060","username":"1001"},{"id":"gw-b","name":"Hunt","enabled":false,"domain":"pbx.local","addr":"10.0.0.1:5060","username":"1002"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len=%d", len(got))
	}
	if !got[0].Enabled || got[1].Enabled {
		t.Fatalf("enabled a=%v b=%v", got[0].Enabled, got[1].Enabled)
	}
}

func TestDecodePersistArmedScenarioID(t *testing.T) {
	t.Parallel()
	got, err := DecodePersist([]byte(`{"profiles":[{"id":"gw-a","name":"Desk","enabled":true,"domain":"pbx.local","addr":"10.0.0.1:5060","username":"160","armed_scenario_id":"fake_ringing_uas"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ArmedScenarioID != "fake_ringing_uas" {
		t.Fatalf("got %+v", got)
	}
}

func TestDecodePersistOriginateDefaults(t *testing.T) {
	t.Parallel()
	got, err := DecodePersist([]byte(`{"profiles":[{"id":"gw-a","name":"Desk","enabled":true,"domain":"pbx.local","addr":"10.0.0.1:5060","username":"160","originate_scenario_id":"short_call","originate_to":"100","originate_calls":2}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].OriginateScenarioID != "short_call" || got[0].OriginateTo != "100" || got[0].OriginateCalls != 2 {
		t.Fatalf("got %+v", got)
	}
}

func TestSavePersistMode0600(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := SavePersist(dir, []Config{{Domain: "pbx.local", Addr: "10.0.0.1:5060", Username: "1001", Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(filepath.Join(dir, persistFileName))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%o", st.Mode().Perm())
	}
}
