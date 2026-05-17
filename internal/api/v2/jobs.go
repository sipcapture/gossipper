package v2

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/sipcapture/gossipper/internal/supervisor"
	"github.com/sipcapture/gossipper/internal/uistore"
)

type startJobBody struct {
	// ID lets the caller pin a job id (otherwise UUIDv4 is used).
	ID string `json:"id,omitempty"`
	// ProfileID + Kind identify which profile to launch.
	ProfileID   string `json:"profile_id"`
	ProfileKind string `json:"profile_kind"`
	// ScenarioID overrides the profile's default scenario when set.
	ScenarioID string `json:"scenario_id,omitempty"`
	// RecordWAV enables automatic per-call WAV capture into the job
	// artifacts dir (decoded G.711 RTP).
	RecordWAV bool `json:"record_wav,omitempty"`
	// RecordWAVDuplex controls stereo (L=sent, R=recv) capture; ignored
	// when RecordWAV is false.
	RecordWAVDuplex bool `json:"record_wav_duplex,omitempty"`
	// Engine carries opaque CLI overrides forwarded to the worker spec.
	Engine map[string]any `json:"engine,omitempty"`
}

func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	if !s.requireRegistry(w) {
		return
	}
	limit := 100
	if v := strings.TrimSpace(r.URL.Query().Get("limit")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			limit = n
		}
	}
	out, err := s.cfg.Registry.Store.List(r.Context(), limit)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if out == nil {
		out = []supervisor.Job{}
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"jobs": out})
}

func (s *Server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	if !s.requireRegistry(w) {
		return
	}
	job, err := s.cfg.Registry.Store.Get(r.Context(), pathID(r))
	if err != nil {
		code, msg := mapStoreError(err)
		s.writeError(w, code, msg)
		return
	}
	arts, err := s.cfg.Registry.Store.ListArtifacts(r.Context(), job.ID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if arts == nil {
		arts = []supervisor.Artifact{}
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"job": job, "artifacts": arts})
}

func (s *Server) handleStartJob(w http.ResponseWriter, r *http.Request) {
	if !s.requireRegistry(w) {
		return
	}
	if !s.requireStore(w) {
		return
	}
	var body startJobBody
	if err := s.decodeJSON(r, &body, 1<<20); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	body.ProfileKind = strings.ToLower(strings.TrimSpace(body.ProfileKind))
	body.ProfileID = strings.TrimSpace(body.ProfileID)
	switch body.ProfileKind {
	case string(uistore.KindServer):
		if _, err := s.cfg.Store.GetServerProfile(body.ProfileID); err != nil {
			code, msg := mapStoreError(err)
			s.writeError(w, code, "profile: "+msg)
			return
		}
	case string(uistore.KindClient):
		if _, err := s.cfg.Store.GetClientProfile(body.ProfileID); err != nil {
			code, msg := mapStoreError(err)
			s.writeError(w, code, "profile: "+msg)
			return
		}
	default:
		s.writeError(w, http.StatusBadRequest, `profile_kind must be "server" or "client"`)
		return
	}
	if body.ScenarioID != "" {
		if _, err := s.cfg.Store.GetScenario(body.ScenarioID); err != nil {
			code, msg := mapStoreError(err)
			s.writeError(w, code, "scenario: "+msg)
			return
		}
	}
	jobID := strings.TrimSpace(body.ID)
	if jobID == "" {
		jobID = uuid.NewString()
	}
	artifactsDir, err := s.cfg.Store.Layout().JobArtifactDir(jobID)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	argsJSON, err := supervisor.EncodeArgs(body.Engine)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	job := supervisor.Job{
		ID:           jobID,
		ProfileID:    body.ProfileID,
		ProfileKind:  body.ProfileKind,
		ScenarioID:   body.ScenarioID,
		Status:       supervisor.StatusPending,
		ArgsJSON:     argsJSON,
		ArtifactsDir: artifactsDir,
		CreatedAt:    time.Now().UTC(),
	}
	spec := supervisor.Spec{
		JobID:           jobID,
		DataDir:         s.cfg.Store.Layout().Root,
		ProfileID:       body.ProfileID,
		ProfileKind:     body.ProfileKind,
		ScenarioID:      body.ScenarioID,
		ArtifactsDir:    artifactsDir,
		RecordWAV:       body.RecordWAV,
		RecordWAVDuplex: body.RecordWAVDuplex,
		Engine:          body.Engine,
	}
	out, err := s.cfg.Registry.StartJob(r.Context(), job, spec)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeAudit(r.Context(), r, "job.start", out.ID, body.ProfileKind+"/"+body.ProfileID)
	s.writeJSON(w, http.StatusCreated, out)
}

func (s *Server) handleStopJob(w http.ResponseWriter, r *http.Request) {
	if !s.requireRegistry(w) {
		return
	}
	id := pathID(r)
	out, err := s.cfg.Registry.StopJob(r.Context(), id)
	if err != nil {
		if errors.Is(err, supervisor.ErrNotFound) {
			s.writeError(w, http.StatusNotFound, "not found")
			return
		}
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeAudit(r.Context(), r, "job.stop", id, "")
	s.writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleDeleteJob(w http.ResponseWriter, r *http.Request) {
	if !s.requireRegistry(w) {
		return
	}
	id := pathID(r)
	if err := s.cfg.Registry.Store.Delete(r.Context(), id); err != nil {
		code, msg := mapStoreError(err)
		s.writeError(w, code, msg)
		return
	}
	s.writeAudit(r.Context(), r, "job.delete", id, "")
	w.WriteHeader(http.StatusNoContent)
}
