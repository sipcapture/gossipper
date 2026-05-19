package v2

import (
	"net/http"
	"strings"

	"github.com/sipcapture/gossipper/internal/loadtest"
)

func (s *Server) handleGetLoadTest(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]any{
		"scenarios": loadtest.InviteMediaScenarios,
		"defaults": map[string]any{
			"scenario_id":              "invite_media",
			"total_calls":              10,
			"rate":                     4,
			"max_concurrent":           2,
			"health_enabled":           true,
			"health_min_success_ratio": 0.95,
			"health_max_failed_calls":  0,
			"soak":                     "total_calls=0 runs until POST /api/v2/jobs/{id}/stop",
		},
		"lifecycle": map[string]string{
			"start":  "POST /api/v2/load-test/run — forks gossipper worker in background, returns job",
			"status": "GET /api/v2/jobs/{id}",
			"stop":   "POST /api/v2/jobs/{id}/stop",
			"events": "GET /api/v2/jobs/{id}/events",
		},
	})
}

func (s *Server) handleRunLoadTest(w http.ResponseWriter, r *http.Request) {
	if !s.requireRegistry(w) || !s.requireStore(w) {
		return
	}
	var body loadtest.Request
	if err := s.decodeJSON(r, &body, 1<<20); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	out, err := loadtest.Start(r.Context(), s.cfg.Store, s.cfg.Registry, body)
	if err != nil {
		code, msg := mapLoadTestError(err)
		s.writeError(w, code, msg)
		return
	}
	s.writeAudit(r.Context(), r, "loadtest.run", out.ID, body.Director)
	s.writeJSON(w, http.StatusAccepted, map[string]any{
		"job":     out,
		"async":   true,
		"message": "load test worker started in background; poll GET /api/v2/jobs/" + out.ID + " or POST .../stop",
	})
}

func mapLoadTestError(err error) (int, string) {
	if err == nil {
		return http.StatusOK, ""
	}
	msg := err.Error()
	if strings.Contains(msg, "director") ||
		strings.Contains(msg, "total_calls") ||
		strings.Contains(msg, "rate") ||
		strings.Contains(msg, "max_concurrent") ||
		strings.Contains(msg, "scenario_id") ||
		strings.Contains(msg, "health_") ||
		strings.Contains(msg, "required") ||
		strings.Contains(msg, " must ") {
		return http.StatusBadRequest, msg
	}
	return http.StatusInternalServerError, msg
}
