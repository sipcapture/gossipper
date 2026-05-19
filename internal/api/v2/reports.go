package v2

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/sipcapture/gossipper/internal/supervisor"
)

func (s *Server) handleListReports(w http.ResponseWriter, r *http.Request) {
	if !s.requireRegistry(w) {
		return
	}
	limit := 100
	if v := strings.TrimSpace(r.URL.Query().Get("limit")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	out, err := s.cfg.Registry.Store.ListReportArtifacts(r.Context(), limit)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if out == nil {
		out = []supervisor.ReportRow{}
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"reports": out})
}
