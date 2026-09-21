package v2

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sipcapture/gossipper/internal/siplog"
)

type sipTraceCapture struct {
	SIP   bool   `json:"sip"`
	App   bool   `json:"app"`
	Level string `json:"level"`
}

type sipTraceResponse struct {
	Messages []siplog.Record `json:"messages"`
	Next     uint64          `json:"next"`
	Cleared  bool            `json:"cleared,omitempty"`
	Capture  sipTraceCapture `json:"capture"`
}

func (s *Server) captureState() sipTraceCapture {
	sip, app := s.cfg.SIPTrace.Capture()
	return sipTraceCapture{SIP: sip, App: app, Level: s.cfg.SIPTrace.MinLevel()}
}

func (s *Server) handleGetSIPTraceCapture(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, s.captureState())
}

func (s *Server) handlePutSIPTraceCapture(w http.ResponseWriter, r *http.Request) {
	var body sipTraceCapture
	if err := s.decodeJSON(r, &body, 1<<12); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	level := strings.TrimSpace(body.Level)
	if level != "" {
		if _, _, ok := siplog.ParseMinLevel(level); !ok {
			s.writeError(w, http.StatusBadRequest, "level must be debug, info, warn, or error")
			return
		}
	}
	s.cfg.SIPTrace.SetCapture(body.SIP, body.App)
	if level != "" {
		_ = s.cfg.SIPTrace.SetMinLevel(level)
	}
	s.writeJSON(w, http.StatusOK, s.captureState())
}

func (s *Server) handleGetSIPTrace(w http.ResponseWriter, r *http.Request) {
	since, ok := s.parseUintQuery(w, r, "since")
	if !ok {
		return
	}
	limit := 200
	if v := strings.TrimSpace(r.URL.Query().Get("limit")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			s.writeError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		limit = n
	}
	if limit > 500 {
		limit = 500
	}
	next, recs := s.cfg.SIPTrace.Since(since, limit)
	if recs == nil {
		recs = []siplog.Record{}
	}
	s.writeJSON(w, http.StatusOK, sipTraceResponse{Messages: recs, Next: next, Capture: s.captureState()})
}

func (s *Server) handleClearSIPTrace(w http.ResponseWriter, r *http.Request) {
	s.cfg.SIPTrace.Clear()
	next, _ := s.cfg.SIPTrace.Since(0, 1)
	s.writeJSON(w, http.StatusOK, sipTraceResponse{Messages: []siplog.Record{}, Next: next, Cleared: true, Capture: s.captureState()})
}

// handleExportSIPTrace dumps the whole in-memory ring as text or a reconstructed pcap.
func (s *Server) handleExportSIPTrace(w http.ResponseWriter, r *http.Request) {
	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format == "" {
		format = "text"
	}
	if format != "text" && format != "pcap" {
		s.writeError(w, http.StatusBadRequest, "format must be text or pcap")
		return
	}
	_, recs := s.cfg.SIPTrace.Since(0, 1<<20)
	recs = siplog.FilterScenario(recs, r.URL.Query().Get("scenario"))
	sipOn, appOn := s.cfg.SIPTrace.Capture()
	recs = siplog.FilterKind(recs, sipOn, appOn)
	recs = siplog.FilterLevel(recs, s.cfg.SIPTrace.MinLevel())
	stamp := time.Now().UTC().Format("20060102-150405")
	if format == "text" {
		name := fmt.Sprintf("gossipper-trace-%s.txt", stamp)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
		_, _ = w.Write([]byte(siplog.FormatText(recs)))
		return
	}
	var buf bytes.Buffer
	if err := siplog.WritePCAP(&buf, recs); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	name := fmt.Sprintf("gossipper-trace-%s.pcap", stamp)
	w.Header().Set("Content-Type", "application/vnd.tcpdump.pcap")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	_, _ = w.Write(buf.Bytes())
}

// handleSIPTraceWS upgrades to a websocket that pushes SIP ring records as they
// arrive. First frame is the backlog after ?since= (default 0). Later frames
// are incremental; Clear emits {cleared:true, messages:[]}.
func (s *Server) handleSIPTraceWS(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeWS(w, r) {
		return
	}
	since, ok := s.parseUintQuery(w, r, "since")
	if !ok {
		return
	}
	conn, err := liveUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	conn.SetReadLimit(1024)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go func() {
		for {
			if _, _, err := conn.NextReader(); err != nil {
				cancel()
				return
			}
		}
	}()

	ring := s.cfg.SIPTrace
	notify, unsub := ring.Subscribe()
	defer unsub()

	var lastGen uint64
	var lastSip, lastApp bool
	var lastLevel string
	captureInit := false
	first := true
	emit := func() error {
		next, gen, recs := ring.Snapshot(since, 200)
		if recs == nil {
			recs = []siplog.Record{}
		}
		cap := s.captureState()
		cleared := !first && gen != lastGen
		lastGen = gen
		captureChanged := captureInit && (cap.SIP != lastSip || cap.App != lastApp || cap.Level != lastLevel)
		lastSip, lastApp, lastLevel = cap.SIP, cap.App, cap.Level
		captureInit = true
		if cleared {
			since = next
			_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			return conn.WriteJSON(sipTraceResponse{Messages: []siplog.Record{}, Next: next, Cleared: true, Capture: cap})
		}
		if !first && len(recs) == 0 && !captureChanged {
			return nil
		}
		first = false
		if len(recs) > 0 {
			since = recs[len(recs)-1].Seq
		} else {
			since = next
		}
		_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		return conn.WriteJSON(sipTraceResponse{Messages: recs, Next: next, Capture: cap})
	}
	if err := emit(); err != nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-notify:
			if err := emit(); err != nil {
				return
			}
		}
	}
}

func (s *Server) parseUintQuery(w http.ResponseWriter, r *http.Request, name string) (uint64, bool) {
	v := strings.TrimSpace(r.URL.Query().Get(name))
	if v == "" {
		return 0, true
	}
	n, err := strconv.ParseUint(v, 10, 64)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid "+name)
		return 0, false
	}
	return n, true
}
