package v2

import (
	"net/http"

	"github.com/sipcapture/gossipper/internal/uistore"
)

type scenarioBody struct {
	uistore.ScenarioMeta
	XML string `json:"xml"`
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
	body.ID = pathID(r)
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
