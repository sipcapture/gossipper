package v2

import (
	"net/http"

	"github.com/sipcapture/gossipper/internal/uistore"
)

// --------- server profiles ---------

func (s *Server) handleListServerProfiles(w http.ResponseWriter, _ *http.Request) {
	if !s.requireStore(w) {
		return
	}
	out, err := s.cfg.Store.ListServerProfiles()
	if err != nil {
		code, msg := mapStoreError(err)
		s.writeError(w, code, msg)
		return
	}
	if out == nil {
		out = []uistore.ServerProfile{}
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"servers": out})
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

func (s *Server) handleListClientProfiles(w http.ResponseWriter, _ *http.Request) {
	if !s.requireStore(w) {
		return
	}
	out, err := s.cfg.Store.ListClientProfiles()
	if err != nil {
		code, msg := mapStoreError(err)
		s.writeError(w, code, msg)
		return
	}
	if out == nil {
		out = []uistore.ClientProfile{}
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"clients": out})
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
