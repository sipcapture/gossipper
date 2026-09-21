package v2

import (
	"bytes"
	"net/http"
	"net/url"
	"strings"
	"unicode"

	"github.com/sipcapture/gossipper/internal/siplog"
)

type callsListResponse struct {
	Calls []siplog.CallSummary `json:"calls"`
}

func (s *Server) handleListCalls(w http.ResponseWriter, _ *http.Request) {
	calls := []siplog.CallSummary{}
	if s.cfg.SIPTrace != nil {
		if list := s.cfg.SIPTrace.Calls().List(); list != nil {
			calls = list
		}
	}
	s.writeJSON(w, http.StatusOK, callsListResponse{Calls: calls})
}

func (s *Server) handleGetCall(w http.ResponseWriter, r *http.Request) {
	id, pathDump := parseCallDumpPath(r.PathValue("id"))
	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format != "" && format != "zip" && format != "json" {
		s.writeError(w, http.StatusBadRequest, "format must be zip")
		return
	}
	dump := pathDump || format == "zip"
	if id == "" || s.cfg.SIPTrace == nil {
		s.writeError(w, http.StatusNotFound, "call not found")
		return
	}
	if dump {
		recs, rtp, ok := s.cfg.SIPTrace.Calls().Dump(id)
		if !ok {
			s.writeError(w, http.StatusNotFound, "call not found")
			return
		}
		var buf bytes.Buffer
		if err := siplog.WriteCallDump(&buf, recs, rtp); err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", `attachment; filename="`+callDumpFilename(id)+`"`)
		_, _ = w.Write(buf.Bytes())
		return
	}
	d, ok := s.cfg.SIPTrace.Calls().Get(id)
	if !ok {
		s.writeError(w, http.StatusNotFound, "call not found")
		return
	}
	s.writeJSON(w, http.StatusOK, d)
}

func parseCallDumpPath(raw string) (id string, dump bool) {
	raw = strings.TrimSpace(raw)
	if dec, err := url.PathUnescape(raw); err == nil {
		raw = strings.TrimSpace(dec)
	}
	if strings.HasSuffix(strings.ToLower(raw), "/dump") {
		dump = true
		raw = strings.TrimSpace(raw[:len(raw)-len("/dump")])
	}
	return raw, dump
}

func callDumpFilename(callID string) string {
	var b strings.Builder
	b.WriteString("gossipper-call-")
	n := 0
	for _, r := range callID {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r), r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
		n++
		if n >= 80 {
			break
		}
	}
	name := strings.Trim(b.String(), "-")
	if name == "gossipper-call" || name == "" {
		name = "gossipper-call"
	}
	return name + ".zip"
}

func (s *Server) handleClearCalls(w http.ResponseWriter, _ *http.Request) {
	if s.cfg.SIPTrace != nil {
		s.cfg.SIPTrace.Calls().Clear()
	}
	s.writeJSON(w, http.StatusOK, callsListResponse{Calls: []siplog.CallSummary{}})
}
