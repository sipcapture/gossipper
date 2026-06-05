package supervisor

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/sipcapture/gossipper/internal/safepath"
)

// ToolMeta describes a runnable stress utility exposed via /api/v2/tools.
type ToolMeta struct {
	ID          string         `json:"id"`
	Title       string         `json:"title"`
	Summary     string         `json:"summary"`
	ArgsSchema  map[string]any `json:"args_schema,omitempty"`
	ExampleArgs map[string]any `json:"example_args,omitempty"`
}

// Known tool ids (whitelist).
const (
	ToolPCAP2Scenario = "pcap2scenario"
	ToolReportHTML    = "report-html"
	ToolSummaryToPDF  = "summary-to-pdf"
	ToolRTPSend       = "rtp_send"
	ToolInfindex      = "infindex"
)

// ListTools returns metadata for stress tools runnable as supervisor jobs.
func ListTools() []ToolMeta {
	return []ToolMeta{
		{
			ID: ToolPCAP2Scenario, Title: "pcap2scenario",
			Summary: "PCAP → paired UAC/UAS XML scenarios under the job artifacts dir.",
			ArgsSchema: map[string]any{
				"pcap":      "string (required) — path under data-dir, e.g. media/pcap/capture.pcap",
				"out_dir":   "string (optional) — output directory; default artifacts/.../scenarios",
				"sip_port":  "int (optional)",
				"pcap_link": "string (optional) — datalink: auto, ethernet, linux_sll, …",
			},
			ExampleArgs: map[string]any{"pcap": "media/pcap/capture.pcap", "sip_port": 0},
		},
		{
			ID: ToolReportHTML, Title: "report-html",
			Summary: "Convert summary JSON to standalone HTML in the job artifacts dir.",
			ArgsSchema: map[string]any{
				"in":  "string (required) — summary.json path under data-dir",
				"out": "string (optional) — default artifacts/.../report.html",
			},
			ExampleArgs: map[string]any{"in": "artifacts/jobs/<job-id>/summary.json"},
		},
		{
			ID: ToolSummaryToPDF, Title: "summary-to-pdf",
			Summary: "Render HTML report to PDF (embedded renderer or Chromium in PATH).",
			ArgsSchema: map[string]any{
				"in":  "string (required) — HTML path under data-dir",
				"out": "string (optional) — default artifacts/.../report.pdf",
			},
			ExampleArgs: map[string]any{"in": "artifacts/jobs/<job-id>/report.html"},
		},
		{
			ID: ToolRTPSend, Title: "rtp_send",
			Summary: "Standalone synthetic RTP sender (no SIP scenario).",
			ArgsSchema: map[string]any{
				"rtp_addr":    "string (required) — host:port",
				"rtp_codec":   "string (optional) — default PCMU/8000",
				"rtp_freq_ms": "int (optional) — default 20",
				"duration_ms": "int (optional) — run length; 0 = until stop",
			},
			ExampleArgs: map[string]any{"rtp_addr": "127.0.0.1:4000", "duration_ms": 5000},
		},
		{
			ID: ToolInfindex, Title: "infindex",
			Summary: "Generate CSV injection index for SIPp-style lookup acceleration.",
			ArgsSchema: map[string]any{
				"csv":   "string (required) — CSV path under data-dir",
				"field": "int (optional) — column index, default 0",
			},
			ExampleArgs: map[string]any{"csv": "media/inject/users.csv", "field": 0},
		},
	}
}

// ValidateToolID reports whether id is a known runnable tool.
func ValidateToolID(id string) bool {
	_, ok := lookupToolMeta(id)
	return ok
}

func lookupToolMeta(id string) (ToolMeta, bool) {
	id = strings.TrimSpace(id)
	for _, t := range ListTools() {
		if t.ID == id {
			return t, true
		}
	}
	return ToolMeta{}, false
}

// RememberToolArtifacts registers non-empty files produced by tool jobs.
func RememberToolArtifacts(ctx context.Context, store *JobsStore, jobID, artifactsDir, toolID string) {
	if store == nil || artifactsDir == "" {
		return
	}
	rememberArtifact(ctx, store, artifactsDir, jobID, "log", "worker.log")
	switch toolID {
	case ToolReportHTML:
		rememberArtifact(ctx, store, artifactsDir, jobID, "report_html", "report.html")
	case ToolSummaryToPDF:
		rememberArtifact(ctx, store, artifactsDir, jobID, "report_pdf", "report.pdf")
	case ToolPCAP2Scenario:
		rememberArtifact(ctx, store, artifactsDir, jobID, "scenario_uac", "scenarios", "scenario_uac.xml")
		rememberArtifact(ctx, store, artifactsDir, jobID, "scenario_uas", "scenarios", "scenario_uas.xml")
	case ToolInfindex:
		// index path is next to csv; scan worker.log documents output
	}
	_ = filepath.WalkDir(artifactsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, ierr := d.Info(); ierr != nil || info.Size() == 0 {
			return nil
		}
		base := filepath.Base(path)
		if !safepath.Within(artifactsDir, path) {
			return nil
		}
		info, ierr := safepath.Stat(artifactsDir, path)
		if ierr != nil || info.IsDir() || info.Size() == 0 {
			return nil
		}
		switch filepath.Ext(base) {
		case ".html":
			_ = store.AddArtifact(ctx, jobID, "report_html", path, info.Size())
		case ".pdf":
			_ = store.AddArtifact(ctx, jobID, "report_pdf", path, info.Size())
		case ".xml":
			_ = store.AddArtifact(ctx, jobID, "scenario_xml", path, info.Size())
		}
		return nil
	})
}
