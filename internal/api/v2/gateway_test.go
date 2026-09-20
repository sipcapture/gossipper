package v2

import (
	"context"
	"net/http"
	"testing"

	"github.com/sipcapture/gossipper/internal/gateway"
)

func TestGatewayGetPutArmOriginate(t *testing.T) {
	h := newHarness(t, false)

	resp := h.do(http.MethodGet, "/api/v2/gateway", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET status=%d", resp.StatusCode)
	}
	snap := decode[gateway.Snapshot](t, resp)
	if snap.Status.State != gateway.StateOff {
		t.Fatalf("status=%+v", snap.Status)
	}

	put := h.do(http.MethodPut, "/api/v2/gateway", gateway.Config{
		Enabled:      true,
		Domain:       "pbx.local",
		Addr:         "192.168.1.10:5060",
		Username:     "1001",
		Password:     "secret",
		Register:     false,
		AdvertisedIP: "192.168.1.20",
		ContactPort:  5060,
	})
	if put.StatusCode != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", put.StatusCode, decode[map[string]any](t, put))
	}
	got := decode[gateway.Snapshot](t, put)
	if got.Config.Password != "***" {
		t.Fatalf("password not masked: %q", got.Config.Password)
	}
	if got.Config.Username != "1001" || got.Config.Domain != "pbx.local" {
		t.Fatalf("config=%+v", got.Config)
	}

	armBad := h.do(http.MethodPut, "/api/v2/gateway/arm", map[string]string{"scenario_id": "one_way"})
	if armBad.StatusCode != http.StatusBadRequest {
		t.Fatalf("arm UAC status=%d", armBad.StatusCode)
	}
	_ = armBad.Body.Close()

	armOK := h.do(http.MethodPut, "/api/v2/gateway/arm", map[string]string{"scenario_id": "early_183"})
	if armOK.StatusCode != http.StatusOK {
		t.Fatalf("arm UAS status=%d", armOK.StatusCode)
	}
	armed := decode[gateway.Snapshot](t, armOK)
	if armed.ArmedScenarioID != "early_183" {
		t.Fatalf("armed=%s", armed.ArmedScenarioID)
	}

	origUAS := h.do(http.MethodPost, "/api/v2/gateway/originate", map[string]any{
		"scenario_id": "early_183", "to": "100",
	})
	if origUAS.StatusCode != http.StatusBadRequest {
		t.Fatalf("originate UAS status=%d", origUAS.StatusCode)
	}
	_ = origUAS.Body.Close()

	orig := h.do(http.MethodPost, "/api/v2/gateway/originate", map[string]any{
		"scenario_id": "one_way", "to": "100", "total_calls": 1,
	})
	if orig.StatusCode != http.StatusCreated {
		t.Fatalf("originate status=%d", orig.StatusCode)
	}
	body := decode[map[string]any](t, orig)
	jobID, _ := body["job_id"].(string)
	if jobID == "" {
		t.Fatalf("missing job_id: %+v", body)
	}
	spec, ok := h.runner.Spec(jobID)
	if !ok {
		t.Fatal("stub runner missing spec")
	}
	if spec.ProfileKind != "gateway" || spec.ScenarioID != "one_way" {
		t.Fatalf("spec kind=%s scenario=%s", spec.ProfileKind, spec.ScenarioID)
	}
	if spec.Engine["sip_from"] != "sip:1001@pbx.local" {
		t.Fatalf("sip_from=%v", spec.Engine["sip_from"])
	}
	if spec.Engine["remote_host"] != "192.168.1.10" {
		t.Fatalf("remote_host=%v", spec.Engine["remote_host"])
	}
	if spec.Engine["remote_port"] != 5060 && spec.Engine["remote_port"] != float64(5060) {
		t.Fatalf("remote_port=%v (%T)", spec.Engine["remote_port"], spec.Engine["remote_port"])
	}
}

func TestGatewayProfilesCRUD(t *testing.T) {
	h := newHarness(t, false)

	list0 := h.do(http.MethodGet, "/api/v2/gateways", nil)
	if list0.StatusCode != http.StatusOK {
		t.Fatalf("list empty status=%d", list0.StatusCode)
	}
	empty := decode[map[string]any](t, list0)
	if g, _ := empty["gateways"].([]any); len(g) != 0 {
		t.Fatalf("want empty list, got %+v", empty)
	}

	created := h.do(http.MethodPost, "/api/v2/gateways", gateway.Config{
		ID: "desk", Name: "Desk", Enabled: true,
		Domain: "pbx.local", Addr: "192.168.1.10:5060", Username: "1001", Password: "secret",
	})
	if created.StatusCode != http.StatusCreated {
		t.Fatalf("create status=%d", created.StatusCode)
	}
	snap := decode[gateway.Snapshot](t, created)
	if snap.Config.ID != "desk" || snap.Config.Name != "Desk" || !snap.Config.Enabled {
		t.Fatalf("created=%+v", snap.Config)
	}
	if snap.Config.Password != "***" {
		t.Fatalf("password not masked: %q", snap.Config.Password)
	}

	dup := h.do(http.MethodPost, "/api/v2/gateways", gateway.Config{
		ID: "desk", Name: "Desk 2", Enabled: true, Domain: "pbx.local", Addr: "192.168.1.10:5060", Username: "1003",
	})
	if dup.StatusCode != http.StatusConflict {
		t.Fatalf("dup status=%d", dup.StatusCode)
	}
	_ = dup.Body.Close()

	off := h.do(http.MethodPut, "/api/v2/gateways/desk", gateway.Config{
		Name: "Desk", Enabled: false, Domain: "pbx.local", Addr: "192.168.1.10:5060", Username: "1001",
	})
	if off.StatusCode != http.StatusOK {
		t.Fatalf("put status=%d", off.StatusCode)
	}
	got := decode[gateway.Snapshot](t, off)
	if got.Config.Enabled {
		t.Fatal("expected disabled")
	}

	orig := h.do(http.MethodPost, "/api/v2/gateways/desk/originate", map[string]any{
		"scenario_id": "one_way", "to": "100",
	})
	if orig.StatusCode != http.StatusBadRequest {
		t.Fatalf("originate disabled status=%d", orig.StatusCode)
	}
	_ = orig.Body.Close()

	arm := h.do(http.MethodPut, "/api/v2/gateways/desk/arm", map[string]string{"scenario_id": "early_183"})
	if arm.StatusCode != http.StatusOK {
		t.Fatalf("arm status=%d", arm.StatusCode)
	}
	armed := decode[gateway.Snapshot](t, arm)
	if armed.ArmedScenarioID != "early_183" {
		t.Fatalf("armed=%s", armed.ArmedScenarioID)
	}

	del := h.do(http.MethodDelete, "/api/v2/gateways/desk", nil)
	if del.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status=%d", del.StatusCode)
	}
	_ = del.Body.Close()

	missing := h.do(http.MethodGet, "/api/v2/gateways/desk", nil)
	if missing.StatusCode != http.StatusNotFound {
		t.Fatalf("get missing status=%d", missing.StatusCode)
	}
	_ = missing.Body.Close()
}

func TestGatewayDiscoverIPs(t *testing.T) {
	h := newHarness(t, false)
	prev := discoverIPs
	discoverIPs = func(_ context.Context) gateway.IPHints {
		return gateway.IPHints{
			Local:    []gateway.LocalAddr{{IP: "192.168.1.20", Iface: "eth0"}},
			External: "203.0.113.10",
		}
	}
	t.Cleanup(func() { discoverIPs = prev })

	resp := h.do(http.MethodGet, "/api/v2/gateway/ips", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	got := decode[gateway.IPHints](t, resp)
	if len(got.Local) != 1 || got.Local[0].IP != "192.168.1.20" || got.External != "203.0.113.10" {
		t.Fatalf("hints=%+v", got)
	}
}
