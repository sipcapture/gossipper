package v2

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sipcapture/gossipper/internal/settingsauth"
	"github.com/sipcapture/gossipper/internal/supervisor"
	"github.com/sipcapture/gossipper/internal/uistore"
)

type harness struct {
	t      *testing.T
	srv    *httptest.Server
	store  *uistore.Store
	auth   *settingsauth.Auth
	token  string
	reg    *supervisor.Registry
	runner *supervisor.StubRunner
}

func newHarness(t *testing.T, withAuth bool) *harness {
	t.Helper()
	dir := t.TempDir()
	store, err := uistore.Open(filepath.Join(dir, "data"))
	if err != nil {
		t.Fatalf("Open store: %v", err)
	}
	db, err := settingsauth.OpenStore(filepath.Join(dir, "settings.sqlite"))
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	jobsStore := supervisor.NewJobsStore(db)
	runner := supervisor.NewStubRunner()
	reg := supervisor.NewRegistry(jobsStore, runner)

	cfg := Config{Store: store, Registry: reg}
	var auth *settingsauth.Auth
	var token string
	if withAuth {
		auth, err = settingsauth.Open(filepath.Join(dir, "auth.sqlite"), "0123456789abcdef-secret")
		if err != nil {
			t.Fatalf("Open auth: %v", err)
		}
		t.Cleanup(func() { _ = auth.Close() })
		if err := auth.CreateUser(t.Context(), "admin", "admin0000"); err != nil {
			t.Fatalf("CreateUser: %v", err)
		}
		tok, _, err := auth.Login(t.Context(), "admin", "admin0000")
		if err != nil {
			t.Fatalf("Login: %v", err)
		}
		token = tok
		cfg.Auth = auth
	}
	mux := http.NewServeMux()
	New(cfg).Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &harness{t: t, srv: srv, store: store, auth: auth, token: token, reg: reg, runner: runner}
}

func (h *harness) path(prefix string, id int64) string {
	return prefix + itoa64(id)
}

func itoa64(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func (h *harness) do(method, path string, body any) *http.Response {
	h.t.Helper()
	var reader io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, h.srv.URL+path, reader)
	if err != nil {
		h.t.Fatalf("new request: %v", err)
	}
	if reader != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if h.token != "" {
		req.Header.Set("Authorization", "Bearer "+h.token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatalf("do %s %s: %v", method, path, err)
	}
	return resp
}

func decode[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	defer resp.Body.Close()
	var out T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

func TestV2Health(t *testing.T) {
	h := newHarness(t, false)
	resp := h.do(http.MethodGet, "/api/v2/health", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	body := decode[map[string]any](t, resp)
	if body["status"] != "ok" || body["auth"] != "none" {
		t.Fatalf("unexpected body: %+v", body)
	}
}

func TestV2HealthWithAuth(t *testing.T) {
	h := newHarness(t, true)
	resp := h.do(http.MethodGet, "/api/v2/health", nil)
	body := decode[map[string]any](t, resp)
	if body["auth"] != "internal" {
		t.Fatalf("expected internal, got %v", body["auth"])
	}
}

func TestV2RequiresAuth(t *testing.T) {
	h := newHarness(t, true)
	prev := h.token
	h.token = ""
	resp := h.do(http.MethodGet, "/api/v2/servers", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
	h.token = prev
	resp = h.do(http.MethodGet, "/api/v2/servers", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 after auth, got %d", resp.StatusCode)
	}
}

func TestV2ServerProfilesCRUD(t *testing.T) {
	h := newHarness(t, false)
	body := map[string]any{
		"id":   "primary",
		"name": "Primary",
		"transports": []map[string]any{
			{"transport": "u1", "local_ip": "0.0.0.0", "local_port": 5060, "enabled": true},
		},
	}
	resp := h.do(http.MethodPost, "/api/v2/servers", body)
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST status=%d body=%s", resp.StatusCode, string(raw))
	}
	resp.Body.Close()
	resp = h.do(http.MethodGet, "/api/v2/servers", nil)
	listed := decode[map[string][]uistore.ServerProfile](t, resp)
	if len(listed["servers"]) != 1 {
		t.Fatalf("list len=%d", len(listed["servers"]))
	}
	body["name"] = "Renamed"
	resp = h.do(http.MethodPut, "/api/v2/servers/primary", body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT status=%d", resp.StatusCode)
	}
	resp.Body.Close()
	resp = h.do(http.MethodGet, "/api/v2/servers/primary", nil)
	got := decode[uistore.ServerProfile](t, resp)
	if got.Name != "Renamed" {
		t.Fatalf("name mismatch: %q", got.Name)
	}
	resp = h.do(http.MethodDelete, "/api/v2/servers/primary", nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE status=%d", resp.StatusCode)
	}
	resp.Body.Close()
	resp = h.do(http.MethodGet, "/api/v2/servers/primary", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestV2ClientProfilesAndScenariosCRUD(t *testing.T) {
	h := newHarness(t, false)
	resp := h.do(http.MethodPost, "/api/v2/clients", map[string]any{"id": "stress", "name": "Stress UAC", "rate": 10})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("client POST status=%d", resp.StatusCode)
	}
	resp.Body.Close()
	resp = h.do(http.MethodPost, "/api/v2/scenarios", map[string]any{
		"id":   "uas_basic",
		"name": "Basic UAS",
		"xml":  `<?xml version="1.0"?><scenario name="basic"/>`,
	})
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("scenario POST status=%d body=%s", resp.StatusCode, string(raw))
	}
	resp.Body.Close()
	resp = h.do(http.MethodGet, "/api/v2/scenarios/uas_basic", nil)
	body := decode[uistore.ScenarioBody](t, resp)
	if !strings.Contains(body.XML, "basic") {
		t.Fatalf("body mismatch: %q", body.XML)
	}
	resp = h.do(http.MethodDelete, "/api/v2/clients/stress", nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE client status=%d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestV2MediaUploadAndList(t *testing.T) {
	h := newHarness(t, false)
	wav := miniWAV()
	req, _ := http.NewRequest(http.MethodPost, h.srv.URL+"/api/v2/media/wav/test.wav", bytes.NewReader(wav))
	req.Header.Set("Content-Type", "audio/wav")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("upload status=%d", resp.StatusCode)
	}
	resp.Body.Close()
	resp = h.do(http.MethodGet, "/api/v2/media/wav", nil)
	body := decode[map[string]any](t, resp)
	list, _ := body["media"].([]any)
	if len(list) != 1 {
		t.Fatalf("expected 1 media, got %d", len(list))
	}
	resp = h.do(http.MethodGet, "/api/v2/media/wav/test.wav", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("download status=%d", resp.StatusCode)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !bytes.Equal(raw, wav) {
		t.Fatalf("payload mismatch: len(got)=%d len(want)=%d", len(raw), len(wav))
	}
	resp = h.do(http.MethodDelete, "/api/v2/media/wav/test.wav", nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE status=%d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestV2MediaUploadRejectsBogusWAV(t *testing.T) {
	h := newHarness(t, false)
	req, _ := http.NewRequest(http.MethodPost, h.srv.URL+"/api/v2/media/wav/bad.wav", bytes.NewReader([]byte("RIFFhello world this is not a WAV body")))
	req.Header.Set("Content-Type", "audio/wav")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid WAV, got %d", resp.StatusCode)
	}
}

// miniWAV is the minimum RIFF/WAVE/fmt-chunk header validator accepts.
func miniWAV() []byte {
	out := []byte("RIFF")
	out = append(out, 0x24, 0x00, 0x00, 0x00)
	out = append(out, []byte("WAVE")...)
	out = append(out, []byte("fmt ")...)
	out = append(out, 0x10, 0x00, 0x00, 0x00)
	out = append(out, 0x01, 0x00, 0x01, 0x00, 0x40, 0x1f, 0x00, 0x00, 0x80, 0x3e, 0x00, 0x00, 0x02, 0x00, 0x10, 0x00)
	out = append(out, []byte("data")...)
	out = append(out, 0x00, 0x00, 0x00, 0x00)
	return out
}

func TestV2JobsStartStopDelete(t *testing.T) {
	h := newHarness(t, false)
	if _, err := h.store.PutServerProfile(uistore.ServerProfile{ID: "primary", Name: "Primary"}, true); err != nil {
		t.Fatal(err)
	}
	resp := h.do(http.MethodPost, "/api/v2/jobs", map[string]any{"profile_id": "primary", "profile_kind": "server"})
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("start status=%d body=%s", resp.StatusCode, string(raw))
	}
	job := decode[supervisor.Job](t, resp)
	if job.Status != supervisor.StatusRunning {
		t.Fatalf("expected running, got %q", job.Status)
	}
	resp = h.do(http.MethodGet, "/api/v2/jobs", nil)
	listed := decode[map[string][]supervisor.Job](t, resp)
	if len(listed["jobs"]) != 1 {
		t.Fatalf("list len=%d", len(listed["jobs"]))
	}
	resp = h.do(http.MethodPost, "/api/v2/jobs/"+job.ID+"/stop", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stop status=%d", resp.StatusCode)
	}
	stopped := decode[supervisor.Job](t, resp)
	if stopped.Status != supervisor.StatusStopped {
		t.Fatalf("expected stopped, got %q", stopped.Status)
	}
	resp = h.do(http.MethodDelete, "/api/v2/jobs/"+job.ID, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status=%d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestV2StartJobCustomIDAndBuiltinScenario(t *testing.T) {
	h := newHarness(t, false)
	if _, err := h.store.PutServerProfile(uistore.ServerProfile{ID: "primary", Name: "Primary"}, true); err != nil {
		t.Fatal(err)
	}
	resp := h.do(http.MethodPost, "/api/v2/jobs", map[string]any{
		"id":           "my-stress-run",
		"profile_id":   "primary",
		"profile_kind": "server",
		"scenario_id":  "uac",
	})
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("start status=%d body=%s", resp.StatusCode, string(raw))
	}
	job := decode[supervisor.Job](t, resp)
	if job.ID != "my-stress-run" {
		t.Fatalf("job id=%q", job.ID)
	}
	if job.ScenarioID != "uac" {
		t.Fatalf("scenario_id=%q", job.ScenarioID)
	}
	resp.Body.Close()
}

func TestV2StartJobValidatesProfile(t *testing.T) {
	h := newHarness(t, false)
	resp := h.do(http.MethodPost, "/api/v2/jobs", map[string]any{"profile_id": "missing", "profile_kind": "server"})
	if resp.StatusCode != http.StatusNotFound {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 404 for missing profile, got %d body=%s", resp.StatusCode, string(raw))
	}
	resp.Body.Close()
	resp = h.do(http.MethodPost, "/api/v2/jobs", map[string]any{"profile_id": "x", "profile_kind": "wat"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad kind, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestV2AuthLoginFlow(t *testing.T) {
	h := newHarness(t, true)
	prev := h.token
	h.token = ""
	resp := h.do(http.MethodPost, "/api/v2/auth/login", map[string]string{"username": "admin", "password": "admin0000"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status=%d", resp.StatusCode)
	}
	body := decode[map[string]any](t, resp)
	tok, _ := body["token"].(string)
	if tok == "" {
		t.Fatalf("no token returned")
	}
	h.token = tok
	resp = h.do(http.MethodGet, "/api/v2/me", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("me status=%d", resp.StatusCode)
	}
	me := decode[map[string]any](t, resp)
	if me["username"] != "admin" {
		t.Fatalf("me username=%v", me["username"])
	}
	h.token = prev
}

func TestV2BuiltinScenarios(t *testing.T) {
	h := newHarness(t, false)
	resp := h.do(http.MethodGet, "/api/v2/builtin-scenarios", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list builtin status=%d", resp.StatusCode)
	}
	body := decode[map[string]any](t, resp)
	sc, _ := body["scenarios"].([]any)
	if len(sc) < 3 {
		t.Fatalf("expected builtin scenarios, got %d", len(sc))
	}
	resp.Body.Close()

	resp = h.do(http.MethodGet, "/api/v2/builtin-scenarios/uac", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get builtin uac status=%d", resp.StatusCode)
	}
	got := decode[map[string]any](t, resp)
	xml, _ := got["xml"].(string)
	if !strings.Contains(xml, "<scenario") {
		t.Fatalf("expected scenario xml, got %q", xml[:min(40, len(xml))])
	}
	resp.Body.Close()
}

func TestV2Settings(t *testing.T) {
	h := newHarness(t, false)
	resp := h.do(http.MethodGet, "/api/v2/settings", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("settings status=%d", resp.StatusCode)
	}
	body := decode[map[string]any](t, resp)
	if body["ui_data_dir"] == "" {
		t.Fatalf("missing ui_data_dir")
	}
	resp.Body.Close()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
