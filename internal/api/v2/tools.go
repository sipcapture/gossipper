package v2

import (
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/sipcapture/gossipper/internal/supervisor"
)

func (s *Server) handleListTools(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]any{"tools": supervisor.ListTools()})
}

type runToolBody struct {
	ID   string         `json:"id,omitempty"`
	Args map[string]any `json:"args"`
}

func (s *Server) handleRunTool(w http.ResponseWriter, r *http.Request) {
	if !s.requireRegistry(w) || !s.requireStore(w) {
		return
	}
	toolID := strings.TrimSpace(r.PathValue("id"))
	if !supervisor.ValidateToolID(toolID) {
		s.writeError(w, http.StatusNotFound, "unknown tool")
		return
	}
	var body runToolBody
	if err := s.decodeJSON(r, &body, 1<<20); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Args == nil {
		body.Args = map[string]any{}
	}
	out, err := s.startToolJob(r, toolID, body.ID, body.Args)
	if err != nil {
		code, msg := mapStartToolError(err)
		s.writeError(w, code, msg)
		return
	}
	s.writeAudit(r.Context(), r, "tool.run", out.ID, toolID)
	s.writeJSON(w, http.StatusCreated, map[string]any{"job": out})
}

func (s *Server) startToolJob(r *http.Request, toolID, jobID string, args map[string]any) (supervisor.Job, error) {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		jobID = uuid.NewString()
	}
	artifactsDir, err := s.cfg.Store.Layout().JobArtifactDir(jobID)
	if err != nil {
		return supervisor.Job{}, err
	}
	argsJSON, err := supervisor.EncodeArgs(args)
	if err != nil {
		return supervisor.Job{}, err
	}
	job := supervisor.Job{
		ID:           jobID,
		ProfileID:    toolID,
		ProfileKind:  supervisor.ToolProfileKind,
		Status:       supervisor.StatusPending,
		ArgsJSON:     argsJSON,
		ArtifactsDir: artifactsDir,
		CreatedAt:    time.Now().UTC(),
	}
	spec := supervisor.Spec{
		JobID:        jobID,
		DataDir:      s.cfg.Store.Layout().Root,
		ProfileID:    toolID,
		ProfileKind:  supervisor.ToolProfileKind,
		ArtifactsDir: artifactsDir,
		ToolArgs:     args,
	}
	return s.cfg.Registry.StartJob(r.Context(), job, spec)
}

func mapStartToolError(err error) (int, string) {
	if err == nil {
		return http.StatusOK, ""
	}
	msg := err.Error()
	if strings.Contains(msg, "path") || strings.Contains(msg, "required") || strings.Contains(msg, "unknown tool") {
		return http.StatusBadRequest, msg
	}
	return http.StatusInternalServerError, msg
}
