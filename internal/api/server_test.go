package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sipcapture/gossipper/internal/cli"
	"github.com/sipcapture/gossipper/internal/engine"
	"github.com/sipcapture/gossipper/internal/scenario"
)

func mustScenario(t *testing.T, xml string) scenario.Scenario {
	t.Helper()
	sc, err := scenario.ParseString(xml)
	if err != nil {
		t.Fatal(err)
	}
	return sc
}

func TestAPIHealthAndStats(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<scenario name="t">
  <send><![CDATA[OPTIONS sip:[service]@[remote_ip]:[remote_port] SIP/2.0
Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=z9hG4bK1
From: <sip:a@[local_ip]:[local_port]>;tag=1
To: <sip:[service]@[remote_ip]:[remote_port]>
Call-ID: [call_id]
CSeq: 1 OPTIONS
Content-Length: 0

]]></send>
  <recv response="200" optional="true"/>
</scenario>`
	sc := mustScenario(t, xml)
	eng := engine.New(engine.Config{
		Scenario:      sc,
		Transport:     "u1",
		LocalIP:       "127.0.0.1",
		LocalPort:     5060,
		RemoteHost:    "127.0.0.1",
		RemotePort:    9,
		DefaultRecvTO: 1,
	})
	srv := New(ServerConfig{Engine: eng, CLI: cli.DefaultConfig(), ValidateScenario: func(scenario.Scenario) error { return nil }})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	res, err := ts.Client().Get(ts.URL + "/api/v1/health")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("health: %s", res.Status)
	}

	res2, err := ts.Client().Get(ts.URL + "/api/v1/stats")
	if err != nil {
		t.Fatal(err)
	}
	defer res2.Body.Close()
	if res2.StatusCode != http.StatusOK {
		t.Fatalf("stats: %s", res2.Status)
	}
	var snap map[string]any
	if err := json.NewDecoder(res2.Body).Decode(&snap); err != nil {
		t.Fatal(err)
	}
	if _, ok := snap["total_calls"]; !ok {
		t.Fatalf("missing total_calls: %#v", snap)
	}
}

func TestAPIAuthToken(t *testing.T) {
	sc := mustScenario(t, `<?xml version="1.0"?><scenario name="t"><send><![CDATA[OPTIONS sip:x SIP/2.0

]]></send><recv response="200" optional="true"/></scenario>`)
	eng := engine.New(engine.Config{Scenario: sc, Transport: "u1", LocalIP: "127.0.0.1", LocalPort: 1, RemoteHost: "127.0.0.1", RemotePort: 9})
	srv := New(ServerConfig{Engine: eng, CLI: cli.DefaultConfig(), Token: "secret", ValidateScenario: func(scenario.Scenario) error { return nil }})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	res, err := ts.Client().Get(ts.URL + "/api/v1/health")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401 without token, got %d", res.StatusCode)
	}

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/health", nil)
	req.Header.Set("Authorization", "Bearer secret")
	res2, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res2.Body.Close()
	if res2.StatusCode != http.StatusOK {
		t.Fatalf("want 200 with token, got %d", res2.StatusCode)
	}
}

func TestAPIScenarioPutApply(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "scen.xml")
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<scenario name="t">
  <send><![CDATA[OPTIONS sip:[service]@[remote_ip]:[remote_port] SIP/2.0
Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=z9hG4bK1
From: <sip:a@[local_ip]:[local_port]>;tag=1
To: <sip:[service]@[remote_ip]:[remote_port]>
Call-ID: [call_id]
CSeq: 1 OPTIONS
Content-Length: 0

]]></send>
  <recv response="200" optional="true"/>
</scenario>`
	if err := os.WriteFile(path, []byte(xml), 0o644); err != nil {
		t.Fatal(err)
	}
	sc, err := scenario.ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	eng := engine.New(engine.Config{
		Scenario:      sc,
		Transport:     "u1",
		LocalIP:       "127.0.0.1",
		LocalPort:     5060,
		RemoteHost:    "127.0.0.1",
		RemotePort:    9,
		DefaultRecvTO: 1,
	})
	cfg := cli.DefaultConfig()
	cfg.ScenarioFile = path
	cfg.Transport = "u1"
	cfg.ScenarioName = ""
	srv := New(ServerConfig{Engine: eng, CLI: cfg, ValidateScenario: func(scenario.Scenario) error { return nil }})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	res, err := ts.Client().Get(ts.URL + "/api/v1/scenario")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("get scenario: %s", res.Status)
	}
	var got map[string]any
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got["xml"].(string), "OPTIONS") {
		t.Fatalf("unexpected xml: %v", got["xml"])
	}

	newXML := strings.Replace(xml, `<scenario name="t">`, `<scenario name="t2">`, 1)
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/v1/scenario?apply=true", strings.NewReader(newXML))
	req.Header.Set("Content-Type", "application/xml")
	res2, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res2.Body.Close()
	if res2.StatusCode != http.StatusOK {
		t.Fatalf("put scenario: %s", res2.Status)
	}
	snap := eng.LiveScenario()
	if snap.Name != "t2" {
		t.Fatalf("live scenario name: got %q want t2", snap.Name)
	}
}

func TestEmbeddedControlUIIndex(t *testing.T) {
	if !HasEmbeddedControlUI() {
		t.Skip("no embedded UI (run `make frontend` in repo root)")
	}
	srv := New(ServerConfig{})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	res, err := ts.Client().Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /: %s", res.Status)
	}
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("Content-Type: %q", ct)
	}
}
