package v2

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/sipcapture/gossipper/internal/settingsauth"
)

type userCreateBody struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role,omitempty"`
}

type userUpdateBody struct {
	Password string `json:"password,omitempty"`
	Role     string `json:"role,omitempty"`
}

func (s *Server) requireAuthDB(w http.ResponseWriter) (*sql.DB, bool) {
	if s.cfg.Auth == nil || !s.cfg.Auth.Enabled() {
		s.writeError(w, http.StatusNotFound, "internal auth is not enabled — users API requires it")
		return nil, false
	}
	db := s.cfg.Auth.DB()
	if db == nil {
		s.writeError(w, http.StatusServiceUnavailable, "auth DB is not available")
		return nil, false
	}
	return db, true
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	db, ok := s.requireAuthDB(w)
	if !ok {
		return
	}
	users, err := settingsauth.ListUsers(r.Context(), db)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if users == nil {
		users = []settingsauth.User{}
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	db, ok := s.requireAuthDB(w)
	if !ok {
		return
	}
	var body userCreateBody
	if err := s.decodeJSON(r, &body, 1<<16); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := settingsauth.CreateUser(r.Context(), db, body.Username, body.Password); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	users, err := settingsauth.ListUsers(r.Context(), db)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var created settingsauth.User
	for _, u := range users {
		if u.Username == body.Username {
			created = u
			break
		}
	}
	role := strings.TrimSpace(body.Role)
	if role == "" {
		role = "operator"
	}
	if created.ID != 0 {
		if err := settingsauth.UpdateUserRole(r.Context(), db, created.ID, role); err != nil {
			s.writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		created.Role = role
	}
	s.writeAudit(r.Context(), r, "user.create", body.Username, role)
	s.writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	db, ok := s.requireAuthDB(w)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(pathID(r), 10, 64)
	if err != nil || id <= 0 {
		s.writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	var body userUpdateBody
	if err := s.decodeJSON(r, &body, 1<<16); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Password != "" {
		if err := settingsauth.UpdateUserPassword(r.Context(), db, id, body.Password); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				s.writeError(w, http.StatusNotFound, "user not found")
				return
			}
			s.writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if body.Role != "" {
		if err := settingsauth.UpdateUserRole(r.Context(), db, id, body.Role); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				s.writeError(w, http.StatusNotFound, "user not found")
				return
			}
			s.writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	got, err := settingsauth.GetUser(r.Context(), db, id)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeAudit(r.Context(), r, "user.update", got.Username, body.Role)
	s.writeJSON(w, http.StatusOK, got)
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	db, ok := s.requireAuthDB(w)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(pathID(r), 10, 64)
	if err != nil || id <= 0 {
		s.writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	users, _ := settingsauth.ListUsers(r.Context(), db)
	if len(users) <= 1 {
		s.writeError(w, http.StatusConflict, "refusing to delete the last user")
		return
	}
	got, err := settingsauth.GetUser(r.Context(), db, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.writeError(w, http.StatusNotFound, "user not found")
			return
		}
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := settingsauth.DeleteUser(r.Context(), db, id); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeAudit(r.Context(), r, "user.delete", got.Username, "")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListAudit(w http.ResponseWriter, r *http.Request) {
	db, ok := s.requireAuthDB(w)
	if !ok {
		return
	}
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			limit = n
		}
	}
	entries, err := settingsauth.ListAudit(r.Context(), db, limit)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if entries == nil {
		entries = []settingsauth.AuditEntry{}
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"audit": entries})
}

// writeAudit is a best-effort helper: if internal auth is enabled and we can
// pull a user id out of the JWT, we record a row; otherwise we still log the
// action with username empty.
func (s *Server) writeAudit(ctx context.Context, r *http.Request, action, target, payload string) {
	if s.cfg.Auth == nil || !s.cfg.Auth.Enabled() {
		return
	}
	db := s.cfg.Auth.DB()
	if db == nil {
		return
	}
	uid, username := claimsFromRequest(s.cfg.Auth, r)
	if payload == "" {
		payload = "{}"
	}
	settingsauth.AppendAudit(ctx, db, uid, username, action, target, payload)
}
