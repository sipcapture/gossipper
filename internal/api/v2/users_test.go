package v2

import (
	"io"
	"net/http"
	"testing"

	"github.com/sipcapture/gossipper/internal/settingsauth"
)

func TestV2UsersRequireAuth(t *testing.T) {
	h := newHarness(t, false)
	resp := h.do(http.MethodGet, "/api/v2/users", nil)
	if resp.StatusCode != http.StatusNotFound {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 404 when auth is disabled, got %d body=%s", resp.StatusCode, string(raw))
	}
	resp.Body.Close()
}

func TestV2UsersCRUD(t *testing.T) {
	h := newHarness(t, true)

	resp := h.do(http.MethodGet, "/api/v2/users", nil)
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("list: status=%d body=%s", resp.StatusCode, string(raw))
	}
	out := decode[map[string][]settingsauth.User](t, resp)
	if len(out["users"]) != 1 || out["users"][0].Username != "admin" {
		t.Fatalf("seed admin missing: %+v", out)
	}

	resp = h.do(http.MethodPost, "/api/v2/users", map[string]any{
		"username": "alice", "password": "wonderland1", "role": "operator",
	})
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("create: status=%d body=%s", resp.StatusCode, string(raw))
	}
	created := decode[settingsauth.User](t, resp)
	if created.Username != "alice" || created.Role != "operator" {
		t.Fatalf("unexpected created user: %+v", created)
	}

	resp = h.do(http.MethodPut, h.path("/api/v2/users/", created.ID), map[string]any{
		"password": "newpassword42",
	})
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("update: status=%d body=%s", resp.StatusCode, string(raw))
	}
	resp.Body.Close()

	resp = h.do(http.MethodDelete, h.path("/api/v2/users/", created.ID), nil)
	if resp.StatusCode != http.StatusNoContent {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("delete: status=%d body=%s", resp.StatusCode, string(raw))
	}
	resp.Body.Close()
}

func TestV2RefuseDeletingLastUser(t *testing.T) {
	h := newHarness(t, true)
	resp := h.do(http.MethodGet, "/api/v2/users", nil)
	users := decode[map[string][]settingsauth.User](t, resp)
	if len(users["users"]) != 1 {
		t.Fatalf("expected 1 user, got %d", len(users["users"]))
	}
	resp = h.do(http.MethodDelete, h.path("/api/v2/users/", users["users"][0].ID), nil)
	if resp.StatusCode != http.StatusConflict {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 409 when deleting last user, got %d body=%s", resp.StatusCode, string(raw))
	}
	resp.Body.Close()
}

func TestV2AuditLogRecordsMutations(t *testing.T) {
	h := newHarness(t, true)

	resp := h.do(http.MethodPost, "/api/v2/servers", map[string]any{
		"id": "p", "name": "p",
		"transports": []map[string]any{{"transport": "u1", "local_port": 5060, "enabled": true}},
	})
	resp.Body.Close()

	resp = h.do(http.MethodGet, "/api/v2/audit", nil)
	out := decode[map[string][]settingsauth.AuditEntry](t, resp)
	if len(out["audit"]) == 0 {
		t.Fatalf("expected at least one audit entry")
	}
	var found bool
	for _, e := range out["audit"] {
		if e.Action == "server.create" && e.Target == "p" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("server.create not in audit log: %+v", out["audit"])
	}
}

func TestV2AdminEndpointsRequireAdminRole(t *testing.T) {
	h := newHarness(t, true)

	resp := h.do(http.MethodPost, "/api/v2/users", map[string]any{
		"username": "bob", "password": "bobpassword1", "role": "operator",
	})
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("create operator: status=%d body=%s", resp.StatusCode, string(raw))
	}
	resp.Body.Close()

	opTok, _, err := h.auth.Login(h.t.Context(), "bob", "bobpassword1")
	if err != nil {
		t.Fatalf("login operator: %v", err)
	}

	for _, path := range []string{"/api/v2/users", "/api/v2/audit"} {
		resp = h.doAs(opTok, http.MethodGet, path, nil)
		if resp.StatusCode != http.StatusForbidden {
			raw, _ := io.ReadAll(resp.Body)
			t.Fatalf("GET %s as operator: want 403 got %d body=%s", path, resp.StatusCode, string(raw))
		}
		resp.Body.Close()
	}

	resp = h.doAs(opTok, http.MethodPost, "/api/v2/settings/rotate-jwt-secret", nil)
	if resp.StatusCode != http.StatusForbidden {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("rotate jwt as operator: want 403 got %d body=%s", resp.StatusCode, string(raw))
	}
	resp.Body.Close()

	resp = h.doAs(opTok, http.MethodGet, "/api/v2/scenarios", nil)
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET scenarios as operator: want 200 got %d body=%s", resp.StatusCode, string(raw))
	}
	resp.Body.Close()

	resp = h.do(http.MethodGet, "/api/v2/users", nil)
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET users as admin: want 200 got %d body=%s", resp.StatusCode, string(raw))
	}
	resp.Body.Close()
}
