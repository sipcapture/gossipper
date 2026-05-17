package v2

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/sipcapture/gossipper/internal/supervisor"
)

// handleStartServerShortcut is a convenience over POST /jobs:
// `POST /api/v2/servers/{id}/start` starts a job for the named server
// profile, returning the spawned Job. Optional JSON body mirrors
// startJobBody minus profile_kind/profile_id (forced to server / {id}).
func (s *Server) handleStartServerShortcut(w http.ResponseWriter, r *http.Request) {
	s.handleStartProfileShortcut(w, r, "server")
}

func (s *Server) handleStopServerShortcut(w http.ResponseWriter, r *http.Request) {
	s.handleStopProfileShortcut(w, r, "server")
}

func (s *Server) handleStartProfileShortcut(w http.ResponseWriter, r *http.Request, kind string) {
	if !s.requireRegistry(w) {
		return
	}
	if !s.requireStore(w) {
		return
	}
	id := pathID(r)
	switch kind {
	case "server":
		if _, err := s.cfg.Store.GetServerProfile(id); err != nil {
			code, msg := mapStoreError(err)
			s.writeError(w, code, msg)
			return
		}
	case "client":
		if _, err := s.cfg.Store.GetClientProfile(id); err != nil {
			code, msg := mapStoreError(err)
			s.writeError(w, code, msg)
			return
		}
	default:
		s.writeError(w, http.StatusBadRequest, "kind must be server|client")
		return
	}

	body := startJobBody{ProfileID: id, ProfileKind: kind}
	if r.ContentLength > 0 {
		if err := s.decodeJSON(r, &body, 1<<20); err != nil {
			s.writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		body.ProfileID = id
		body.ProfileKind = kind
	}

	jobID := body.ID
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
		ProfileID:    id,
		ProfileKind:  kind,
		ScenarioID:   body.ScenarioID,
		Status:       supervisor.StatusPending,
		ArgsJSON:     argsJSON,
		ArtifactsDir: artifactsDir,
		CreatedAt:    time.Now().UTC(),
	}
	spec := supervisor.Spec{
		JobID:           jobID,
		DataDir:         s.cfg.Store.Layout().Root,
		ProfileID:       id,
		ProfileKind:     kind,
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
	s.writeAudit(r.Context(), r, kind+".start", id, out.ID)
	s.writeJSON(w, http.StatusCreated, out)
}

// handleStopProfileShortcut stops the most recent running/pending job for the
// given profile. Returns 404 when there is no such job.
func (s *Server) handleStopProfileShortcut(w http.ResponseWriter, r *http.Request, kind string) {
	if !s.requireRegistry(w) {
		return
	}
	id := pathID(r)
	jobs, err := s.cfg.Registry.Store.List(r.Context(), 500)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var target *supervisor.Job
	for i := range jobs {
		j := &jobs[i]
		if j.ProfileKind != kind || j.ProfileID != id {
			continue
		}
		if j.Status == supervisor.StatusRunning || j.Status == supervisor.StatusPending {
			target = j
			break
		}
	}
	if target == nil {
		s.writeError(w, http.StatusNotFound, "no running job for profile")
		return
	}
	out, err := s.cfg.Registry.StopJob(r.Context(), target.ID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeAudit(r.Context(), r, kind+".stop", id, out.ID)
	s.writeJSON(w, http.StatusOK, out)
}
