package v2

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/sipcapture/gossipper/internal/supervisor"
)

// liveUpgrader accepts cross-origin upgrades from the embedded UI; same-origin
// is the normal case but a developer running Vite on :5173 needs this.
var liveUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 4096,
	CheckOrigin:     func(_ *http.Request) bool { return true },
}

// handleLive opens a websocket that emits one JSON snapshot per tick (default
// 1s, configurable via ?interval_ms=). Each snapshot contains:
//
//	{
//	  "ts": "2026-05-17T18:00:00Z",
//	  "jobs": [
//	    {"id": "...", "status": "running", "profile_kind": "client", "pid": 1234},
//	    ...
//	  ],
//	  "counts": {"running": 1, "succeeded": 12, "failed": 0}
//	}
//
// The client can subscribe once and forget; resync happens on every tick so
// no replay-since-token plumbing is needed.
func (s *Server) handleLive(w http.ResponseWriter, r *http.Request) {
	if !s.requireRegistry(w) {
		return
	}
	// Browsers don't send Authorization headers on websocket upgrades, so we
	// accept the bearer via the ?token= query param too.
	if s.cfg.Auth != nil && s.cfg.Auth.Enabled() {
		if tok := r.URL.Query().Get("token"); tok != "" {
			if r.Header.Get("Authorization") == "" {
				r.Header.Set("Authorization", "Bearer "+tok)
			}
		}
		if !s.cfg.Auth.ValidRequest(r) {
			s.writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
	}
	intervalMs := 1000
	if v := strings.TrimSpace(r.URL.Query().Get("interval_ms")); v != "" {
		var n int
		if _, err := jsonUnmarshalInt(v, &n); err == nil && n >= 250 && n <= 60000 {
			intervalMs = n
		}
	}
	conn, err := liveUpgrader.Upgrade(w, r, nil)
	if err != nil {
		// Upgrade has already written a response on failure.
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

	tick := time.NewTicker(time.Duration(intervalMs) * time.Millisecond)
	defer tick.Stop()
	emit := func() error {
		jobs, err := s.cfg.Registry.Store.List(ctx, 200)
		if err != nil {
			return nil
		}
		counts := map[string]int{}
		for _, j := range jobs {
			counts[string(j.Status)]++
		}
		snap := map[string]any{
			"ts":     time.Now().UTC().Format(time.RFC3339Nano),
			"jobs":   trimJobs(jobs),
			"counts": counts,
		}
		_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		return conn.WriteJSON(snap)
	}
	if err := emit(); err != nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if err := emit(); err != nil {
				return
			}
		}
	}
}

// trimJobs cuts large args_json payloads out of the live feed so clients
// don't get megabyte-sized ticks.
func trimJobs(in []supervisor.Job) []map[string]any {
	out := make([]map[string]any, 0, len(in))
	for _, j := range in {
		out = append(out, map[string]any{
			"id":           j.ID,
			"status":       j.Status,
			"profile_id":   j.ProfileID,
			"profile_kind": j.ProfileKind,
			"pid":          j.PID,
			"started_at":   j.StartedAt,
			"finished_at":  j.FinishedAt,
			"exit_code":    j.ExitCode,
		})
	}
	return out
}

// jsonUnmarshalInt is a tiny wrapper around json.Unmarshal that accepts both
// quoted ("1500") and bare (1500) numbers — query string values come in as
// strings.
func jsonUnmarshalInt(raw string, out *int) (int, error) {
	if err := json.Unmarshal([]byte(raw), out); err == nil {
		return *out, nil
	}
	if err := json.Unmarshal([]byte(`"`+raw+`"`), out); err == nil {
		return *out, nil
	}
	return 0, json.Unmarshal([]byte(raw), out)
}
