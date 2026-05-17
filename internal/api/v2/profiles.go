package v2

import (
	"net/http"
	"time"

	"github.com/sipcapture/gossipper/internal/supervisor"
	"github.com/sipcapture/gossipper/internal/uistore"
)

// ProfileRuntime is the derived runtime view of a profile attached to list
// responses. Status values:
//   - "built-in"            — owned by the master process; always considered up.
//   - "running" / "pending" — has an active supervisor job.
//   - "succeeded" / "failed" / "stopped" — last terminal state of the most
//     recent supervisor job for the profile.
//   - "idle"                — no supervisor job has ever been started for it.
type ProfileRuntime struct {
	Status     string     `json:"status"`
	JobID      string     `json:"job_id,omitempty"`
	PID        *int       `json:"pid,omitempty"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	ExitCode   *int       `json:"exit_code,omitempty"`
}

// serverProfileWithRuntime / clientProfileWithRuntime wrap the on-disk type
// with the derived runtime view. We embed instead of duplicating fields so
// every uistore change flows through transparently.
type serverProfileWithRuntime struct {
	uistore.ServerProfile
	Runtime *ProfileRuntime `json:"runtime,omitempty"`
}

type clientProfileWithRuntime struct {
	uistore.ClientProfile
	Runtime *ProfileRuntime `json:"runtime,omitempty"`
}

// latestJobForProfile returns the most recently created job (by CreatedAt)
// matching (kind, id), favouring running/pending jobs over terminal ones —
// i.e. an active job always wins over an older terminal one even if it was
// created earlier.
func latestJobForProfile(jobs []supervisor.Job, kind, id string) *supervisor.Job {
	var active, terminal *supervisor.Job
	for i := range jobs {
		j := &jobs[i]
		if j.ProfileKind != kind || j.ProfileID != id {
			continue
		}
		switch j.Status {
		case supervisor.StatusRunning, supervisor.StatusPending:
			if active == nil || j.CreatedAt.After(active.CreatedAt) {
				active = j
			}
		default:
			if terminal == nil || j.CreatedAt.After(terminal.CreatedAt) {
				terminal = j
			}
		}
	}
	if active != nil {
		return active
	}
	return terminal
}

// runtimeFor maps source + latest job to the derived runtime view. nil job
// + non-builtin source yields {status:"idle"}.
func runtimeFor(source string, j *supervisor.Job) *ProfileRuntime {
	if source == uistore.SourceBuiltIn {
		return &ProfileRuntime{Status: "built-in"}
	}
	if j == nil {
		return &ProfileRuntime{Status: "idle"}
	}
	rt := &ProfileRuntime{
		Status:     string(j.Status),
		JobID:      j.ID,
		PID:        j.PID,
		StartedAt:  j.StartedAt,
		FinishedAt: j.FinishedAt,
		ExitCode:   j.ExitCode,
	}
	return rt
}

// loadJobsSafe returns the latest N jobs from the registry store, or nil on
// error / when the registry is not wired. The list endpoints stay functional
// for read-only deployments without a JobsStore — runtime fields simply
// degrade to "idle".
func (s *Server) loadJobsSafe(r *http.Request) []supervisor.Job {
	if s.cfg.Registry == nil || s.cfg.Registry.Store == nil {
		return nil
	}
	jobs, err := s.cfg.Registry.Store.List(r.Context(), 500)
	if err != nil {
		s.log.Warn("v2: list jobs for runtime view", "err", err)
		return nil
	}
	return jobs
}

// --------- server profiles ---------

func (s *Server) handleListServerProfiles(w http.ResponseWriter, r *http.Request) {
	if !s.requireStore(w) {
		return
	}
	out, err := s.cfg.Store.ListServerProfiles()
	if err != nil {
		code, msg := mapStoreError(err)
		s.writeError(w, code, msg)
		return
	}
	jobs := s.loadJobsSafe(r)
	wrapped := make([]serverProfileWithRuntime, 0, len(out))
	for _, p := range out {
		wrapped = append(wrapped, serverProfileWithRuntime{
			ServerProfile: p,
			Runtime:       runtimeFor(p.Source, latestJobForProfile(jobs, string(uistore.KindServer), p.ID)),
		})
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"servers": wrapped})
}

func (s *Server) handleCreateServerProfile(w http.ResponseWriter, r *http.Request) {
	if !s.requireStore(w) {
		return
	}
	var body uistore.ServerProfile
	if err := s.decodeJSON(r, &body, 1<<20); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	got, err := s.cfg.Store.PutServerProfile(body, true)
	if err != nil {
		code, msg := mapStoreError(err)
		s.writeError(w, code, msg)
		return
	}
	s.writeAudit(r.Context(), r, "server.create", got.ID, "")
	s.writeJSON(w, http.StatusCreated, got)
}

func (s *Server) handleGetServerProfile(w http.ResponseWriter, r *http.Request) {
	if !s.requireStore(w) {
		return
	}
	got, err := s.cfg.Store.GetServerProfile(pathID(r))
	if err != nil {
		code, msg := mapStoreError(err)
		s.writeError(w, code, msg)
		return
	}
	s.writeJSON(w, http.StatusOK, got)
}

func (s *Server) handleUpdateServerProfile(w http.ResponseWriter, r *http.Request) {
	if !s.requireStore(w) {
		return
	}
	var body uistore.ServerProfile
	if err := s.decodeJSON(r, &body, 1<<20); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	body.ID = pathID(r)
	got, err := s.cfg.Store.PutServerProfile(body, false)
	if err != nil {
		code, msg := mapStoreError(err)
		s.writeError(w, code, msg)
		return
	}
	s.writeAudit(r.Context(), r, "server.update", got.ID, "")
	s.writeJSON(w, http.StatusOK, got)
}

func (s *Server) handleDeleteServerProfile(w http.ResponseWriter, r *http.Request) {
	if !s.requireStore(w) {
		return
	}
	id := pathID(r)
	if err := s.cfg.Store.DeleteServerProfile(id); err != nil {
		code, msg := mapStoreError(err)
		s.writeError(w, code, msg)
		return
	}
	s.writeAudit(r.Context(), r, "server.delete", id, "")
	w.WriteHeader(http.StatusNoContent)
}

// --------- client profiles ---------

func (s *Server) handleListClientProfiles(w http.ResponseWriter, r *http.Request) {
	if !s.requireStore(w) {
		return
	}
	out, err := s.cfg.Store.ListClientProfiles()
	if err != nil {
		code, msg := mapStoreError(err)
		s.writeError(w, code, msg)
		return
	}
	jobs := s.loadJobsSafe(r)
	wrapped := make([]clientProfileWithRuntime, 0, len(out))
	for _, p := range out {
		wrapped = append(wrapped, clientProfileWithRuntime{
			ClientProfile: p,
			Runtime:       runtimeFor(p.Source, latestJobForProfile(jobs, string(uistore.KindClient), p.ID)),
		})
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"clients": wrapped})
}

func (s *Server) handleCreateClientProfile(w http.ResponseWriter, r *http.Request) {
	if !s.requireStore(w) {
		return
	}
	var body uistore.ClientProfile
	if err := s.decodeJSON(r, &body, 1<<20); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	got, err := s.cfg.Store.PutClientProfile(body, true)
	if err != nil {
		code, msg := mapStoreError(err)
		s.writeError(w, code, msg)
		return
	}
	s.writeAudit(r.Context(), r, "client.create", got.ID, "")
	s.writeJSON(w, http.StatusCreated, got)
}

func (s *Server) handleGetClientProfile(w http.ResponseWriter, r *http.Request) {
	if !s.requireStore(w) {
		return
	}
	got, err := s.cfg.Store.GetClientProfile(pathID(r))
	if err != nil {
		code, msg := mapStoreError(err)
		s.writeError(w, code, msg)
		return
	}
	s.writeJSON(w, http.StatusOK, got)
}

func (s *Server) handleUpdateClientProfile(w http.ResponseWriter, r *http.Request) {
	if !s.requireStore(w) {
		return
	}
	var body uistore.ClientProfile
	if err := s.decodeJSON(r, &body, 1<<20); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	body.ID = pathID(r)
	got, err := s.cfg.Store.PutClientProfile(body, false)
	if err != nil {
		code, msg := mapStoreError(err)
		s.writeError(w, code, msg)
		return
	}
	s.writeAudit(r.Context(), r, "client.update", got.ID, "")
	s.writeJSON(w, http.StatusOK, got)
}

func (s *Server) handleDeleteClientProfile(w http.ResponseWriter, r *http.Request) {
	if !s.requireStore(w) {
		return
	}
	id := pathID(r)
	if err := s.cfg.Store.DeleteClientProfile(id); err != nil {
		code, msg := mapStoreError(err)
		s.writeError(w, code, msg)
		return
	}
	s.writeAudit(r.Context(), r, "client.delete", id, "")
	w.WriteHeader(http.StatusNoContent)
}
