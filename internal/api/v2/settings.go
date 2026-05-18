package v2

import (
	"io/fs"
	"net/http"
	"path/filepath"
)

type settingsResp struct {
	UIDataDir           string `json:"ui_data_dir"`
	ScenarioHistoryKeep int    `json:"scenario_history_keep"`
	DiskUsageBytes      int64  `json:"disk_usage_bytes,omitempty"`
}

func (s *Server) handleGetSettings(w http.ResponseWriter, _ *http.Request) {
	if !s.requireStore(w) {
		return
	}
	root := s.cfg.Store.Layout().Root
	resp := settingsResp{
		UIDataDir:           root,
		ScenarioHistoryKeep: s.cfg.Store.ScenarioHistoryKeep(),
		DiskUsageBytes:      dirSize(root),
	}
	s.writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleRotateJWTSecret(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Auth == nil || !s.cfg.Auth.Enabled() {
		s.writeError(w, http.StatusNotFound, "internal auth is not enabled")
		return
	}
	secret, err := s.cfg.Auth.RotateSecret(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeAudit(r.Context(), r, "settings.rotate_jwt_secret", "jwt_secret", "")
	s.writeJSON(w, http.StatusOK, map[string]string{
		"jwt_secret": secret,
		"warning":    "All signed-in sessions are now invalid. Save this secret and sign in again.",
	})
}

func dirSize(root string) int64 {
	var total int64
	_ = filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, ierr := d.Info(); ierr == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}
