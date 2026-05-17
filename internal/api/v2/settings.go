package v2

import (
	"net/http"
)

// handleRotateJWTSecret is admin-only and replaces the current signing key
// with a fresh 256-bit value. Invalidates every issued token immediately —
// the caller's token included. Returns the new secret once so the operator
// can stash it somewhere safe.
func (s *Server) handleRotateJWTSecret(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Auth == nil || !s.cfg.Auth.Enabled() {
		s.writeError(w, http.StatusServiceUnavailable, "auth disabled")
		return
	}
	secret, err := s.cfg.Auth.RotateSecret(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeAudit(r.Context(), r, "settings.rotate_jwt_secret", "", "")
	s.writeJSON(w, http.StatusOK, map[string]string{
		"jwt_secret": secret,
		"warning":    "all existing tokens (including yours) are now invalid; sign in again to continue",
	})
}
