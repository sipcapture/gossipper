package v2

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/sipcapture/gossipper/internal/supervisor"
)

func TestV2ImportScenarioFromPCAPJob(t *testing.T) {
	h := newHarness(t, false)
	jobID := "pcap-import-1"
	jobDir, err := h.store.Layout().JobArtifactDir(jobID)
	if err != nil {
		t.Fatal(err)
	}
	scDir := filepath.Join(jobDir, "scenarios")
	if err := os.MkdirAll(scDir, 0o755); err != nil {
		t.Fatal(err)
	}
	uac := `<scenario name="uac"><send>INVITE</send></scenario>`
	uas := `<scenario name="uas"><recv>INVITE</recv></scenario>`
	if err := os.WriteFile(filepath.Join(scDir, "scenario_uac.xml"), []byte(uac), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scDir, "scenario_uas.xml"), []byte(uas), 0o644); err != nil {
		t.Fatal(err)
	}

	job := supervisor.Job{
		ID:           jobID,
		ProfileID:    supervisor.ToolPCAP2Scenario,
		ProfileKind:  supervisor.ToolProfileKind,
		Status:       supervisor.StatusPending,
		ArgsJSON:     `{"pcap":"media/pcap/x.pcap"}`,
		ArtifactsDir: jobDir,
	}
	if _, err := h.reg.Store.Create(t.Context(), job); err != nil {
		t.Fatal(err)
	}
	exit := 0
	if err := h.reg.Store.UpdateStatus(t.Context(), jobID, supervisor.StatusSucceeded, &exit, ""); err != nil {
		t.Fatal(err)
	}

	resp := h.do(http.MethodPost, "/api/v2/scenarios/import-from-pcap-job", map[string]any{
		"job_id":      jobID,
		"which":       "both",
		"scenario_id": "from_pcap_uac",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	out := decode[map[string]any](t, resp)
	imp, _ := out["imported"].([]any)
	if len(imp) != 2 {
		t.Fatalf("imported=%v", out["imported"])
	}
	resp.Body.Close()

	got, err := h.store.GetScenario("from_pcap_uac")
	if err != nil {
		t.Fatal(err)
	}
	if got.XML != uac {
		t.Fatalf("uac xml mismatch")
	}
}
