package v2

import (
	"net/http"
	"strings"

	"github.com/sipcapture/gossipper/internal/scenario"
)

func (s *Server) handleListBuiltinScenarios(w http.ResponseWriter, _ *http.Request) {
	out := scenario.ListBuiltins()
	if out == nil {
		out = []scenario.BuiltinInfo{}
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"scenarios": out, "source": "builtin"})
}

func (s *Server) handleGetBuiltinScenario(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	xml, err := scenario.BuiltinXML(id)
	if err != nil {
		s.writeError(w, http.StatusNotFound, "not found")
		return
	}
	meta := scenario.BuiltinInfo{}
	for _, b := range scenario.ListBuiltins() {
		if b.ID == id {
			meta = b
			break
		}
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"meta":   meta,
		"xml":    xml,
		"source": "builtin",
	})
}
