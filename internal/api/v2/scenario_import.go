package v2

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/sipcapture/gossipper/internal/pcap2scenario"
	"github.com/sipcapture/gossipper/internal/supervisor"
	"github.com/sipcapture/gossipper/internal/uistore"
)

type importPCAPJobBody struct {
	JobID         string `json:"job_id"`
	Which         string `json:"which"` // uac | uas | both
	ScenarioID    string `json:"scenario_id"`
	UASScenarioID string `json:"uas_scenario_id,omitempty"`
}

func (s *Server) handleImportScenarioFromPCAPJob(w http.ResponseWriter, r *http.Request) {
	if !s.requireStore(w) || !s.requireRegistry(w) {
		return
	}
	var body importPCAPJobBody
	if err := s.decodeJSON(r, &body, 1<<20); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	jobID := strings.TrimSpace(body.JobID)
	if jobID == "" {
		s.writeError(w, http.StatusBadRequest, "job_id is required")
		return
	}
	which := strings.ToLower(strings.TrimSpace(body.Which))
	if which == "" {
		which = "uac"
	}
	if which != "uac" && which != "uas" && which != "both" {
		s.writeError(w, http.StatusBadRequest, "which must be uac, uas, or both")
		return
	}
	scID := strings.TrimSpace(body.ScenarioID)
	if (which == "uac" || which == "both") && scID == "" {
		s.writeError(w, http.StatusBadRequest, "scenario_id is required for uac/both")
		return
	}
	uasID := strings.TrimSpace(body.UASScenarioID)
	if uasID == "" && scID != "" {
		uasID = scID + "_uas"
	}
	if (which == "uas" || which == "both") && uasID == "" {
		s.writeError(w, http.StatusBadRequest, "uas_scenario_id or scenario_id is required for uas/both")
		return
	}

	job, err := s.cfg.Registry.Store.Get(r.Context(), jobID)
	if err != nil {
		s.writeError(w, http.StatusNotFound, "job not found")
		return
	}
	if job.ProfileKind != supervisor.ToolProfileKind || job.ProfileID != supervisor.ToolPCAP2Scenario {
		s.writeError(w, http.StatusBadRequest, "job is not a succeeded pcap2scenario tool run")
		return
	}
	if job.Status != supervisor.StatusSucceeded {
		s.writeError(w, http.StatusConflict, fmt.Sprintf("job status is %q; wait for succeeded", job.Status))
		return
	}

	var args map[string]any
	if err := json.Unmarshal([]byte(job.ArgsJSON), &args); err != nil {
		s.writeError(w, http.StatusInternalServerError, "invalid job args_json")
		return
	}
	outDir, err := pcap2scenario.OutDirFromArgs(s.cfg.Store.Layout().Root, job.ArtifactsDir, args)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	type imported struct {
		ID   string `json:"id"`
		Role string `json:"role"`
		File string `json:"file"`
	}
	var out []imported

	importOne := func(id, role, filename string) error {
		if id == "" {
			return errors.New("scenario id is empty")
		}
		path := filepath.Join(outDir, filename)
		raw, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", filename, err)
		}
		meta := uistore.ScenarioMeta{ID: id, Name: id, Role: role}
		if _, err := s.cfg.Store.PutScenario(meta, string(raw), true); err != nil {
			if errors.Is(err, uistore.ErrConflict) {
				if _, err = s.cfg.Store.PutScenario(meta, string(raw), false); err != nil {
					return err
				}
			} else {
				return err
			}
		}
		out = append(out, imported{ID: id, Role: role, File: filename})
		s.writeAudit(r.Context(), r, "scenario.import_pcap", id, jobID)
		return nil
	}

	if which == "uac" || which == "both" {
		if err := importOne(scID, "uac", "scenario_uac.xml"); err != nil {
			code := http.StatusInternalServerError
			if strings.Contains(err.Error(), "read ") {
				code = http.StatusNotFound
			}
			s.writeError(w, code, err.Error())
			return
		}
	}
	if which == "uas" || which == "both" {
		if err := importOne(uasID, "uas", "scenario_uas.xml"); err != nil {
			code := http.StatusInternalServerError
			if strings.Contains(err.Error(), "read ") {
				code = http.StatusNotFound
			}
			s.writeError(w, code, err.Error())
			return
		}
	}

	s.writeJSON(w, http.StatusCreated, map[string]any{
		"imported": out,
		"out_dir":  outDir,
		"job_id":   jobID,
	})
}
