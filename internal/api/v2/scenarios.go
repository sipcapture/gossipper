package v2

import (
	"net/http"
	"path/filepath"
	"strings"

	"github.com/sipcapture/gossipper/internal/uistore"
)

type scenarioBody struct {
	uistore.ScenarioMeta
	XML string `json:"xml"`
}

func isSafePathID(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	if strings.Contains(id, "/") || strings.Contains(id, "\\") || strings.Contains(id, "..") {
		return false
	}
	return filepath.Base(id) == id
}

func (s *Server) handleListScenarios(w http.ResponseWriter, _ *http.Request) {
	if !s.requireStore(w) {
		return
	}
	out, err := s.cfg.Store.ListScenarios()
	if err != nil {
		code, msg := mapStoreError(err)
		s.writeError(w, code, msg)
		return
	}
	if out == nil {
		out = []uistore.ScenarioMeta{}
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"scenarios": out})
}

func (s *Server) handleGetScenario(w http.ResponseWriter, r *http.Request) {
	if !s.requireStore(w) {
		return
	}
	got, err := s.cfg.Store.GetScenario(pathID(r))
	if err != nil {
		code, msg := mapStoreError(err)
		s.writeError(w, code, msg)
		return
	}
	s.writeJSON(w, http.StatusOK, got)
}

func (s *Server) handleCreateScenario(w http.ResponseWriter, r *http.Request) {
	if !s.requireStore(w) {
		return
	}
	var body scenarioBody
	if err := s.decodeJSON(r, &body, 16<<20); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !isSafePathID(body.ID) {
		s.writeError(w, http.StatusBadRequest, uistore.ErrInvalidID.Error())
		return
	}
	got, err := s.cfg.Store.PutScenario(body.ScenarioMeta, body.XML, true)
	if err != nil {
		code, msg := mapStoreError(err)
		s.writeError(w, code, msg)
		return
	}
	s.writeAudit(r.Context(), r, "scenario.create", got.Meta.ID, "")
	s.writeJSON(w, http.StatusCreated, got)
}

func (s *Server) handleUpdateScenario(w http.ResponseWriter, r *http.Request) {
	if !s.requireStore(w) {
		return
	}
	var body scenarioBody
	if err := s.decodeJSON(r, &body, 16<<20); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	id := pathID(r)
	if !isSafePathID(id) {
		s.writeError(w, http.StatusBadRequest, uistore.ErrInvalidID.Error())
		return
	}
	body.ID = id
	got, err := s.cfg.Store.PutScenario(body.ScenarioMeta, body.XML, false)
	if err != nil {
		code, msg := mapStoreError(err)
		s.writeError(w, code, msg)
		return
	}
	s.writeAudit(r.Context(), r, "scenario.update", got.Meta.ID, "")
	s.writeJSON(w, http.StatusOK, got)
}

func (s *Server) handleDeleteScenario(w http.ResponseWriter, r *http.Request) {
	if !s.requireStore(w) {
		return
	}
	id := pathID(r)
	if err := s.cfg.Store.DeleteScenario(id); err != nil {
		code, msg := mapStoreError(err)
		s.writeError(w, code, msg)
		return
	}
	s.writeAudit(r.Context(), r, "scenario.delete", id, "")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListScenarioHistory(w http.ResponseWriter, r *http.Request) {
	if !s.requireStore(w) {
		return
	}
	out, err := s.cfg.Store.ListScenarioHistory(pathID(r))
	if err != nil {
		code, msg := mapStoreError(err)
		s.writeError(w, code, msg)
		return
	}
	if out == nil {
		out = []uistore.ScenarioHistoryEntry{}
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"history": out})
}

func (s *Server) handleGetScenarioHistory(w http.ResponseWriter, r *http.Request) {
	if !s.requireStore(w) {
		return
	}
	id := pathID(r)
	ts := strings.TrimSpace(r.PathValue("ts"))
	body, err := s.cfg.Store.GetScenarioHistory(id, ts)
	if err != nil {
		code, msg := mapStoreError(err)
		s.writeError(w, code, msg)
		return
	}
	s.writeJSON(w, http.StatusOK, body)
}

func (s *Server) handleDeleteScenarioHistory(w http.ResponseWriter, r *http.Request) {
	if !s.requireStore(w) {
		return
	}
	id := pathID(r)
	ts := strings.TrimSpace(r.PathValue("ts"))
	if err := s.cfg.Store.DeleteScenarioHistory(id, ts); err != nil {
		code, msg := mapStoreError(err)
		s.writeError(w, code, msg)
		return
	}
	s.writeAudit(r.Context(), r, "scenario.history.delete", id, ts)
	w.WriteHeader(http.StatusNoContent)
}

type forkScenarioBody struct {
	uistore.ScenarioMeta
}

func (s *Server) handleForkScenarioHistory(w http.ResponseWriter, r *http.Request) {
	if !s.requireStore(w) {
		return
	}
	id := pathID(r)
	ts := strings.TrimSpace(r.PathValue("ts"))
	var body forkScenarioBody
	if err := s.decodeJSON(r, &body, 1<<20); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(body.ID) == "" {
		s.writeError(w, http.StatusBadRequest, "id is required")
		return
	}
	got, err := s.cfg.Store.ForkScenarioFromHistory(id, ts, body.ScenarioMeta)
	if err != nil {
		code, msg := mapStoreError(err)
		if code == http.StatusInternalServerError && strings.Contains(err.Error(), "fork target id") {
			code = http.StatusBadRequest
		}
		s.writeError(w, code, msg)
		return
	}
	s.writeAudit(r.Context(), r, "scenario.fork", got.Meta.ID, id+":"+ts)
	s.writeJSON(w, http.StatusCreated, got)
}
