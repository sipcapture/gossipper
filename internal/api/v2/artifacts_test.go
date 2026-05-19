package v2

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/sipcapture/gossipper/internal/supervisor"
)

func TestV2DownloadJobArtifactSummary(t *testing.T) {
	h := newHarness(t, false)
	jobDir := filepath.Join(h.store.Layout().Root, "artifacts", "jobs", "rep1")
	if err := os.MkdirAll(jobDir, 0o755); err != nil {
		t.Fatal(err)
	}
	summaryPath := filepath.Join(jobDir, "summary.json")
	if err := os.WriteFile(summaryPath, []byte(`{"total_calls":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	jobsStore := h.reg.Store
	if _, err := jobsStore.Create(t.Context(), supervisor.Job{
		ID: "rep1", ProfileKind: "client", ProfileID: "c1", Status: supervisor.StatusSucceeded,
		ArtifactsDir: jobDir,
	}); err != nil {
		t.Fatal(err)
	}
	if err := jobsStore.AddArtifact(t.Context(), "rep1", "summary", summaryPath, 18); err != nil {
		t.Fatal(err)
	}
	resp := h.do(http.MethodGet, "/api/v2/jobs/rep1/artifacts/summary", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestV2ListReports(t *testing.T) {
	h := newHarness(t, false)
	jobDir := filepath.Join(h.store.Layout().Root, "artifacts", "jobs", "rep2")
	if err := os.MkdirAll(jobDir, 0o755); err != nil {
		t.Fatal(err)
	}
	htmlPath := filepath.Join(jobDir, "report.html")
	if err := os.WriteFile(htmlPath, []byte("<html></html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	jobsStore := h.reg.Store
	if _, err := jobsStore.Create(t.Context(), supervisor.Job{
		ID: "rep2", ProfileKind: "tool", ProfileID: "report-html", Status: supervisor.StatusSucceeded,
		ArtifactsDir: jobDir,
	}); err != nil {
		t.Fatal(err)
	}
	if err := jobsStore.AddArtifact(t.Context(), "rep2", "report_html", htmlPath, 13); err != nil {
		t.Fatal(err)
	}
	resp := h.do(http.MethodGet, "/api/v2/reports", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	body := decode[map[string][]supervisor.ReportRow](t, resp)
	if len(body["reports"]) != 1 {
		t.Fatalf("reports=%d", len(body["reports"]))
	}
	resp.Body.Close()
}
