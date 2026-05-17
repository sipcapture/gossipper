package v2

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/golang-jwt/jwt/v5"
)

type healthResp struct {
	Status  string `json:"status"`
	Version string `json:"version,omitempty"`
	Auth    string `json:"auth"`
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	resp := healthResp{Status: "ok", Version: s.cfg.Version, Auth: "none"}
	if s.cfg.Auth != nil && s.cfg.Auth.Enabled() {
		resp.Auth = "internal"
	}
	s.writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleAuthStatus(w http.ResponseWriter, _ *http.Request) {
	if s.cfg.Auth != nil && s.cfg.Auth.Enabled() {
		s.writeJSON(w, http.StatusOK, map[string]string{"auth": "internal"})
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"auth": "none"})
}

type loginBody struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Auth == nil || !s.cfg.Auth.Enabled() {
		s.writeError(w, http.StatusNotFound, "internal auth is not enabled")
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	if err != nil {
		s.writeError(w, http.StatusBadRequest, fmt.Sprintf("read body: %v", err))
		return
	}
	var in loginBody
	if err := json.Unmarshal(data, &in); err != nil {
		s.writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid json: %v", err))
		return
	}
	tok, exp, err := s.cfg.Auth.Login(r.Context(), in.Username, in.Password)
	if err != nil {
		s.writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"token":      tok,
		"expires_at": exp,
		"token_type": "Bearer",
	})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Auth == nil || !s.cfg.Auth.Enabled() {
		s.writeJSON(w, http.StatusOK, map[string]any{
			"auth":     "none",
			"username": "anonymous",
		})
		return
	}
	token := bearerFromRequest(r)
	claims, err := s.cfg.Auth.ParseToken(token)
	if err != nil {
		s.writeError(w, http.StatusUnauthorized, "invalid token")
		return
	}
	mc, _ := claims.(jwt.MapClaims)
	username, _ := mc["sub"].(string)
	uid, _ := mc["uid"].(float64)
	exp, _ := mc["exp"].(float64)
	s.writeJSON(w, http.StatusOK, map[string]any{
		"auth":       "internal",
		"username":   username,
		"user_id":    int64(uid),
		"expires_at": int64(exp),
		"role":       "admin",
	})
}

// bearerFromRequest mirrors settingsauth.bearerFromRequest (unexported).
func bearerFromRequest(r *http.Request) string {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(h) >= len(prefix) && (h[:len(prefix)] == prefix || h[:len(prefix)] == "bearer ") {
		return h[len(prefix):]
	}
	return r.URL.Query().Get("token")
}

// claimsFromRequest extracts (uid, username) from the request's JWT. Returns
// (nil, "") when auth is disabled or the token is missing/invalid.
func claimsFromRequest(a interface {
	Enabled() bool
	ParseToken(string) (jwt.Claims, error)
}, r *http.Request) (*int64, string) {
	if a == nil || !a.Enabled() {
		return nil, ""
	}
	token := bearerFromRequest(r)
	if token == "" {
		return nil, ""
	}
	claims, err := a.ParseToken(token)
	if err != nil {
		return nil, ""
	}
	mc, _ := claims.(jwt.MapClaims)
	username, _ := mc["sub"].(string)
	uidf, _ := mc["uid"].(float64)
	if uidf == 0 {
		return nil, username
	}
	uid := int64(uidf)
	return &uid, username
}
