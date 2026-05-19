package v2

import (
	"net/http"
	"testing"

	"github.com/sipcapture/gossipper/internal/supervisor"
)

func TestV2GetLoadTest(t *testing.T) {
	h := newHarness(t, false)
	resp := h.do(http.MethodGet, "/api/v2/load-test", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestV2RunLoadTestAsync(t *testing.T) {
	h := newHarness(t, false)
	resp := h.do(http.MethodPost, "/api/v2/load-test/run", map[string]any{
		"director":       "127.0.0.1:5060",
		"scenario_id":    "invite_media",
		"total_calls":    2,
		"rate":           1,
		"max_concurrent": 1,
		"health_enabled": true,
		"health_min_success_ratio": 0.9,
		"health_max_failed_calls":  0,
	})
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status=%d want 202", resp.StatusCode)
	}
	out := decode[map[string]any](t, resp)
	job, ok := out["job"].(map[string]any)
	if !ok {
		t.Fatalf("job payload missing: %#v", out)
	}
	if job["status"] != string(supervisor.StatusRunning) {
		t.Fatalf("status=%v", job["status"])
	}
	if out["async"] != true {
		t.Fatalf("async=%v", out["async"])
	}
	resp.Body.Close()
}
