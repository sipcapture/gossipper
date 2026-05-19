package v2

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/sipcapture/gossipper/internal/supervisor"
)

func TestV2ListTools(t *testing.T) {
	h := newHarness(t, false)
	resp := h.do(http.MethodGet, "/api/v2/tools", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	body := decode[map[string][]supervisor.ToolMeta](t, resp)
	if len(body["tools"]) < 3 {
		t.Fatalf("tools=%d", len(body["tools"]))
	}
	resp.Body.Close()
}

func TestV2RunToolJobInfindex(t *testing.T) {
	h := newHarness(t, false)
	csvPath := filepath.Join(h.store.Layout().Root, "inject.csv")
	if err := os.WriteFile(csvPath, []byte("key,value\na,1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	resp := h.do(http.MethodPost, "/api/v2/tools/infindex/run", map[string]any{
		"args": map[string]any{"csv": "inject.csv", "field": 0},
	})
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(raw))
	}
	out := decode[map[string]supervisor.Job](t, resp)
	if out["job"].ProfileKind != supervisor.ToolProfileKind {
		t.Fatalf("kind=%q", out["job"].ProfileKind)
	}
	if out["job"].ProfileID != supervisor.ToolInfindex {
		t.Fatalf("tool=%q", out["job"].ProfileID)
	}
	resp.Body.Close()
}

func TestV2StartToolJobViaJobsEndpoint(t *testing.T) {
	h := newHarness(t, false)
	resp := h.do(http.MethodPost, "/api/v2/jobs", map[string]any{
		"profile_kind": "tool",
		"profile_id":   "infindex",
		"tool_args":    map[string]any{"csv": "missing.csv", "field": 0},
	})
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(raw))
	}
	resp.Body.Close()
}
