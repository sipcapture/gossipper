package api

import (
	"context"
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
	"github.com/sipcapture/gossipper/internal/settingsauth"
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

func TestAPITransportsServerAndClient(t *testing.T) {
	srvXML := `<?xml version="1.0" encoding="UTF-8"?>
<scenario name="srv">
  <recv request="INVITE" optional="true"/>
</scenario>`
	scSrv := mustScenario(t, srvXML)
	engSrv := engine.New(engine.Config{
		Scenario:      scSrv,
		Transport:     "u1",
		LocalIP:       "127.0.0.1",
		LocalPort:     5060,
		RemoteHost:    "127.0.0.1",
		RemotePort:    9,
		DefaultRecvTO: 1,
	})
	apiSrv := New(ServerConfig{Engine: engSrv, CLI: cli.DefaultConfig(), ValidateScenario: func(scenario.Scenario) error { return nil }})
	ts := httptest.NewServer(apiSrv.Handler())
	defer ts.Close()

	res, err := ts.Client().Get(ts.URL + "/api/v1/transports")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET transports: %s", res.Status)
	}
	var got transportsGetResponse
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got.Listeners) != 1 {
		t.Fatalf("want 1 listener, got %#v", got.Listeners)
	}
	if len(got.Clients) != 0 {
		t.Fatalf("server mode want no client rows, got %#v", got.Clients)
	}
	if got.DynamicClientAPI.CanPost || got.DynamicClientAPI.CanDelete {
		t.Fatalf("server api without client hooks: want can_post/can_delete false, got %+v", got.DynamicClientAPI)
	}
	if got.Listeners[0].Index != 0 || !got.Listeners[0].Enabled {
		t.Fatalf("unexpected listener: %+v", got.Listeners[0])
	}
	if got.Listeners[0].ScenarioName != "srv" {
		t.Fatalf("listener scenario name: got %q want srv", got.Listeners[0].ScenarioName)
	}

	reqOff, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/transports", strings.NewReader(`{"index":0,"enabled":false}`))
	reqOff.Header.Set("Content-Type", "application/json")
	res2, err := ts.Client().Do(reqOff)
	if err != nil {
		t.Fatal(err)
	}
	defer res2.Body.Close()
	if res2.StatusCode != http.StatusOK {
		t.Fatalf("POST transports off: %s", res2.Status)
	}
	var st2 transportsGetResponse
	if err := json.NewDecoder(res2.Body).Decode(&st2); err != nil {
		t.Fatal(err)
	}
	if len(st2.Listeners) != 1 || st2.Listeners[0].Enabled {
		t.Fatalf("expected disabled: %#v", st2)
	}
	if len(st2.Clients) != 0 {
		t.Fatalf("server want no clients: %#v", st2.Clients)
	}

	reqOn, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/transports", strings.NewReader(`{"listeners":[{"index":0,"enabled":true}]}`))
	reqOn.Header.Set("Content-Type", "application/json")
	res3, err := ts.Client().Do(reqOn)
	if err != nil {
		t.Fatal(err)
	}
	defer res3.Body.Close()
	if res3.StatusCode != http.StatusOK {
		t.Fatalf("POST transports batch: %s", res3.Status)
	}

	// Client engine: no transport slots
	scCli := mustScenario(t, `<?xml version="1.0"?><scenario name="t"><send><![CDATA[OPTIONS sip:x SIP/2.0

]]></send><recv response="200" optional="true"/></scenario>`)
	engCli := engine.New(engine.Config{Scenario: scCli, Transport: "u1", LocalIP: "127.0.0.1", LocalPort: 1, RemoteHost: "127.0.0.1", RemotePort: 9})
	apiCli := New(ServerConfig{Engine: engCli, CLI: cli.DefaultConfig(), ValidateScenario: func(scenario.Scenario) error { return nil }})
	ts2 := httptest.NewServer(apiCli.Handler())
	defer ts2.Close()
	res4, err := ts2.Client().Get(ts2.URL + "/api/v1/transports")
	if err != nil {
		t.Fatal(err)
	}
	defer res4.Body.Close()
	if res4.StatusCode != http.StatusOK {
		t.Fatalf("GET transports client: %s", res4.Status)
	}
	var empty transportsGetResponse
	if err := json.NewDecoder(res4.Body).Decode(&empty); err != nil {
		t.Fatal(err)
	}
	if len(empty.Listeners) != 0 {
		t.Fatalf("client want empty listeners, got %#v", empty.Listeners)
	}
	if len(empty.Clients) != 1 {
		t.Fatalf("client want 1 client transport row, got %#v", empty.Clients)
	}
	if empty.Clients[0].ID != "primary" || !empty.Clients[0].Accepting {
		t.Fatalf("unexpected client row: %+v", empty.Clients[0])
	}
	if empty.Clients[0].ScenarioName != "t" {
		t.Fatalf("scenario name: got %q want t", empty.Clients[0].ScenarioName)
	}
	if empty.Clients[0].Dynamic {
		t.Fatal("primary client should not be marked dynamic")
	}
	reqPause, _ := http.NewRequest(http.MethodPost, ts2.URL+"/api/v1/transports", strings.NewReader(`{"clients":[{"id":"primary","accepting":false}]}`))
	reqPause.Header.Set("Content-Type", "application/json")
	resPause, err := ts2.Client().Do(reqPause)
	if err != nil {
		t.Fatal(err)
	}
	defer resPause.Body.Close()
	if resPause.StatusCode != http.StatusOK {
		t.Fatalf("POST transports client pause: %s", resPause.Status)
	}
	var paused transportsGetResponse
	if err := json.NewDecoder(resPause.Body).Decode(&paused); err != nil {
		t.Fatal(err)
	}
	if len(paused.Clients) != 1 || paused.Clients[0].Accepting {
		t.Fatalf("want paused client row: %#v", paused.Clients)
	}
	if !engCli.Paused() {
		t.Fatal("engine should be paused")
	}
	reqResume, _ := http.NewRequest(http.MethodPost, ts2.URL+"/api/v1/transports", strings.NewReader(`{"clients":[{"id":"primary","accepting":true}]}`))
	reqResume.Header.Set("Content-Type", "application/json")
	resResume, err := ts2.Client().Do(reqResume)
	if err != nil {
		t.Fatal(err)
	}
	defer resResume.Body.Close()
	if resResume.StatusCode != http.StatusOK {
		t.Fatalf("POST transports client resume: %s", resResume.Status)
	}
	if engCli.Paused() {
		t.Fatal("engine should be resumed")
	}

	reqUnknown, _ := http.NewRequest(http.MethodPost, ts2.URL+"/api/v1/transports", strings.NewReader(`{"clients":[{"id":"does-not-exist","accepting":false}]}`))
	reqUnknown.Header.Set("Content-Type", "application/json")
	resUnk, err := ts2.Client().Do(reqUnknown)
	if err != nil {
		t.Fatal(err)
	}
	defer resUnk.Body.Close()
	if resUnk.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown client id want 400, got %d", resUnk.StatusCode)
	}
	reqBad, _ := http.NewRequest(http.MethodPost, ts2.URL+"/api/v1/transports", strings.NewReader(`{"index":0,"enabled":true}`))
	reqBad.Header.Set("Content-Type", "application/json")
	res5, err := ts2.Client().Do(reqBad)
	if err != nil {
		t.Fatal(err)
	}
	defer res5.Body.Close()
	if res5.StatusCode != http.StatusBadRequest {
		t.Fatalf("client POST want 400, got %d", res5.StatusCode)
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

func TestAPIMultiStatsShape(t *testing.T) {
	sc := mustScenario(t, `<?xml version="1.0"?><scenario name="t"><send><![CDATA[OPTIONS sip:x SIP/2.0

]]></send><recv response="200" optional="true"/></scenario>`)
	eng0 := engine.New(engine.Config{Scenario: sc, Transport: "u1", LocalIP: "127.0.0.1", LocalPort: 1, RemoteHost: "127.0.0.1", RemotePort: 9})
	eng1 := engine.New(engine.Config{Scenario: sc, Transport: "u1", LocalIP: "127.0.0.1", LocalPort: 2, RemoteHost: "127.0.0.1", RemotePort: 9})
	srv := New(ServerConfig{
		Engine:           eng0,
		ExtraEngines:     []*engine.Engine{eng1},
		ExtraIDs:         []string{"side"},
		StatsPrimaryID:   "main",
		CLI:              cli.DefaultConfig(),
		ValidateScenario: func(scenario.Scenario) error { return nil },
	})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	res, err := ts.Client().Get(ts.URL + "/api/v1/stats")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out["multi"] != true {
		t.Fatalf("stats: %#v", out)
	}
	wl, _ := out["engines"].([]any)
	if len(wl) != 2 {
		t.Fatalf("engines len=%d", len(wl))
	}
}

func TestAPIClientsPostDisabled(t *testing.T) {
	sc := mustScenario(t, `<?xml version="1.0"?><scenario name="t"><send><![CDATA[OPTIONS sip:x SIP/2.0

]]></send><recv response="200" optional="true"/></scenario>`)
	eng := engine.New(engine.Config{Scenario: sc, Transport: "u1", LocalIP: "127.0.0.1", LocalPort: 1, RemoteHost: "127.0.0.1", RemotePort: 9})
	srv := New(ServerConfig{Engine: eng, CLI: cli.DefaultConfig(), ValidateScenario: func(scenario.Scenario) error { return nil }})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	res, err := ts.Client().Post(ts.URL+"/api/v1/clients", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("want 404 when AddLoadClient nil, got %d", res.StatusCode)
	}
}

func TestAPIClientsPostOK(t *testing.T) {
	sc := mustScenario(t, `<?xml version="1.0"?><scenario name="t"><send><![CDATA[OPTIONS sip:x SIP/2.0

]]></send><recv response="200" optional="true"/></scenario>`)
	eng0 := engine.New(engine.Config{Scenario: sc, Transport: "u1", LocalIP: "127.0.0.1", LocalPort: 1, RemoteHost: "127.0.0.1", RemotePort: 9})
	engDyn := engine.New(engine.Config{Scenario: sc, Transport: "u1", LocalIP: "127.0.0.1", LocalPort: 2, RemoteHost: "127.0.0.1", RemotePort: 9})
	srv := New(ServerConfig{
		Engine: eng0,
		LiveExtras: func() ([]*engine.Engine, []string) {
			return []*engine.Engine{engDyn}, []string{"dyn-test"}
		},
		AddLoadClient: func(_ context.Context, wantID string, body []byte) (string, error) {
			if wantID != "want" {
				t.Fatalf("wantID: got %q", wantID)
			}
			if len(body) == 0 {
				t.Fatal("empty body")
			}
			return "dyn-test", nil
		},
		CLI:              cli.DefaultConfig(),
		ValidateScenario: func(scenario.Scenario) error { return nil },
	})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	res, err := ts.Client().Post(ts.URL+"/api/v1/clients?id=want", "application/json", strings.NewReader(`{"transport":"udp"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("want 201, got %d", res.StatusCode)
	}
	var out map[string]any
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out["id"] != "dyn-test" || out["started"] != true {
		t.Fatalf("body: %#v", out)
	}

	res2, err := ts.Client().Get(ts.URL + "/api/v1/stats")
	if err != nil {
		t.Fatal(err)
	}
	defer res2.Body.Close()
	var stats map[string]any
	if err := json.NewDecoder(res2.Body).Decode(&stats); err != nil {
		t.Fatal(err)
	}
	if stats["multi"] != true {
		t.Fatalf("stats multi: %#v", stats)
	}
	engines, _ := stats["engines"].([]any)
	if len(engines) != 2 {
		t.Fatalf("engines len=%d", len(engines))
	}
}

func TestAPIHealthWithQueryToken(t *testing.T) {
	sc := mustScenario(t, `<?xml version="1.0"?><scenario name="t"><send><![CDATA[OPTIONS sip:x SIP/2.0

]]></send><recv response="200" optional="true"/></scenario>`)
	eng := engine.New(engine.Config{Scenario: sc, Transport: "u1", LocalIP: "127.0.0.1", LocalPort: 1, RemoteHost: "127.0.0.1", RemotePort: 9})
	srv := New(ServerConfig{Engine: eng, CLI: cli.DefaultConfig(), Token: "abc", ValidateScenario: func(scenario.Scenario) error { return nil }})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/health?token=abc", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("want 200 with query token, got %d", res.StatusCode)
	}
}

func TestAPIClientsGet(t *testing.T) {
	sc := mustScenario(t, `<?xml version="1.0"?><scenario name="t"><send><![CDATA[OPTIONS sip:x SIP/2.0

]]></send><recv response="200" optional="true"/></scenario>`)
	eng0 := engine.New(engine.Config{Scenario: sc, Transport: "u1", LocalIP: "127.0.0.1", LocalPort: 1, RemoteHost: "127.0.0.1", RemotePort: 9})
	eng1 := engine.New(engine.Config{Scenario: sc, Transport: "u1", LocalIP: "127.0.0.1", LocalPort: 2, RemoteHost: "127.0.0.1", RemotePort: 9})
	srv := New(ServerConfig{
		Engine:           eng0,
		LiveExtras:       func() ([]*engine.Engine, []string) { return []*engine.Engine{eng1}, []string{"dyn-a"} },
		CLI:              cli.DefaultConfig(),
		ValidateScenario: func(scenario.Scenario) error { return nil },
	})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	res, err := ts.Client().Get(ts.URL + "/api/v1/clients")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("clients get: %s", res.Status)
	}
	var out map[string]any
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	dyn, _ := out["dynamic"].([]any)
	if len(dyn) != 1 || dyn[0] != "dyn-a" {
		t.Fatalf("dynamic: %#v", out)
	}
	engs, _ := out["engines"].([]any)
	if len(engs) != 2 {
		t.Fatalf("engines: want 2 got %d %#v", len(engs), out)
	}
	var sawDyn bool
	for _, row := range engs {
		m, ok := row.(map[string]any)
		if !ok {
			t.Fatalf("engine row type %T", row)
		}
		if m["id"] == "dyn-a" && m["dynamic"] == true {
			sawDyn = true
		}
	}
	if !sawDyn {
		t.Fatalf("expected dyn-a with dynamic=true in %#v", engs)
	}
}

func TestAPIClientsGetWithoutLiveExtras(t *testing.T) {
	sc := mustScenario(t, `<?xml version="1.0"?><scenario name="t"><send><![CDATA[OPTIONS sip:x SIP/2.0

]]></send><recv response="200" optional="true"/></scenario>`)
	eng := engine.New(engine.Config{Scenario: sc, Transport: "u1", LocalIP: "127.0.0.1", LocalPort: 1, RemoteHost: "127.0.0.1", RemotePort: 9})
	srv := New(ServerConfig{Engine: eng, CLI: cli.DefaultConfig(), ValidateScenario: func(scenario.Scenario) error { return nil }})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	res, err := ts.Client().Get(ts.URL + "/api/v1/clients")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", res.StatusCode)
	}
	var out map[string]any
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	engs, _ := out["engines"].([]any)
	if len(engs) != 1 {
		t.Fatalf("want 1 engine row, got %#v", out)
	}
	dyn, _ := out["dynamic"].([]any)
	if len(dyn) != 0 {
		t.Fatalf("want empty dynamic, got %#v", dyn)
	}
}

func TestAPIClientsDelete(t *testing.T) {
	sc := mustScenario(t, `<?xml version="1.0"?><scenario name="t"><send><![CDATA[OPTIONS sip:x SIP/2.0

]]></send><recv response="200" optional="true"/></scenario>`)
	eng := engine.New(engine.Config{Scenario: sc, Transport: "u1", LocalIP: "127.0.0.1", LocalPort: 1, RemoteHost: "127.0.0.1", RemotePort: 9})
	var gotID string
	srv := New(ServerConfig{
		Engine: eng,
		RemoveLoadClient: func(_ context.Context, id string) error {
			gotID = id
			return nil
		},
		CLI:              cli.DefaultConfig(),
		ValidateScenario: func(scenario.Scenario) error { return nil },
	})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	req, err := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/clients?id=x1", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("want 204, got %d", res.StatusCode)
	}
	if gotID != "x1" {
		t.Fatalf("id: got %q", gotID)
	}
}

func TestInternalAuthLoginAndJWT(t *testing.T) {
	dir := t.TempDir()
	dbpath := filepath.Join(dir, "settings.sqlite")
	secret := "0123456789abcdef01"
	authSvc, err := settingsauth.Open(dbpath, secret)
	if err != nil {
		t.Fatal(err)
	}
	defer authSvc.Close()
	if err := authSvc.CreateUser(context.Background(), "op", "longpass-99"); err != nil {
		t.Fatal(err)
	}
	sc := mustScenario(t, `<?xml version="1.0"?><scenario name="t"><send><![CDATA[OPTIONS sip:x SIP/2.0

]]></send><recv response="200" optional="true"/></scenario>`)
	eng := engine.New(engine.Config{Scenario: sc, Transport: "u1", LocalIP: "127.0.0.1", LocalPort: 1, RemoteHost: "127.0.0.1", RemotePort: 9})
	srv := New(ServerConfig{
		Engine:           eng,
		CLI:              cli.DefaultConfig(),
		SettingsAuth:     authSvc,
		ValidateScenario: func(scenario.Scenario) error { return nil },
	})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	res0, err := ts.Client().Get(ts.URL + "/api/v1/health")
	if err != nil {
		t.Fatal(err)
	}
	_ = res0.Body.Close()
	if res0.StatusCode != http.StatusUnauthorized {
		t.Fatalf("health without token: want 401, got %d", res0.StatusCode)
	}

	resS, err := ts.Client().Get(ts.URL + "/api/v1/auth/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resS.Body.Close()
	var st map[string]any
	if err := json.NewDecoder(resS.Body).Decode(&st); err != nil {
		t.Fatal(err)
	}
	if st["auth"] != "internal" {
		t.Fatalf("auth status: %#v", st)
	}

	resL, err := ts.Client().Post(ts.URL+"/api/v1/auth/login", "application/json", strings.NewReader(`{"username":"op","password":"longpass-99"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resL.Body.Close()
	if resL.StatusCode != http.StatusOK {
		t.Fatalf("login: %d", resL.StatusCode)
	}
	var loginOut map[string]any
	if err := json.NewDecoder(resL.Body).Decode(&loginOut); err != nil {
		t.Fatal(err)
	}
	jwt, _ := loginOut["token"].(string)
	if jwt == "" {
		t.Fatalf("no token: %#v", loginOut)
	}

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/health", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	res1, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res1.Body.Close()
	if res1.StatusCode != http.StatusOK {
		t.Fatalf("health with jwt: want 200, got %d", res1.StatusCode)
	}
}
