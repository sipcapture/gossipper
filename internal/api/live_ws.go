package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sipcapture/gossipper/internal/stats"
)

var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 8192,
	CheckOrigin: func(r *http.Request) bool {
		// Embedded UI is same-origin; dev (Vite proxy) uses another port — allow for local tooling.
		return true
	},
}

func pumpWSRead(conn *websocket.Conn) {
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
}

// snapshotForLive mirrors GET /stats, GET /control, GET /api/v1/transports for WebSocket pushes.
func (s *Server) snapshotForLive() map[string]any {
	out := map[string]any{"ts": time.Now().UnixMilli()}
	if s.cfg.Engine == nil {
		return out
	}

	extras, extraIDs := s.mergedExtras()
	var dynIDs []string
	if s.cfg.LiveExtras != nil {
		_, dynIDs = s.cfg.LiveExtras()
	}

	if len(extras) > 0 {
		type statRow struct {
			ID    string        `json:"id"`
			Stats stats.Summary `json:"stats"`
		}
		srows := []statRow{{ID: "primary", Stats: s.cfg.Engine.Stats().Snapshot()}}
		if pid := strings.TrimSpace(s.cfg.StatsPrimaryID); pid != "" {
			srows[0].ID = pid
		}
		for i, e := range extras {
			if e == nil {
				continue
			}
			id := extraIDs[i]
			srows = append(srows, statRow{ID: id, Stats: e.Stats().Snapshot()})
		}
		st := map[string]any{"multi": true, "engines": srows}
		if s.cfg.LiveExtras != nil {
			st["dynamic_client_ids"] = dynIDs
		}
		out["stats"] = st

		type ctlRow struct {
			ID     string  `json:"id"`
			Rate   float64 `json:"rate"`
			Paused bool    `json:"paused"`
		}
		crows := []ctlRow{{ID: srows[0].ID, Rate: s.cfg.Engine.Rate(), Paused: s.cfg.Engine.Paused()}}
		for i, e := range extras {
			if e == nil {
				continue
			}
			id := extraIDs[i]
			crows = append(crows, ctlRow{ID: id, Rate: e.Rate(), Paused: e.Paused()})
		}
		out["control"] = map[string]any{"multi": true, "engines": crows}
	} else {
		snap := s.cfg.Engine.Stats().Snapshot()
		if s.cfg.LiveExtras != nil {
			out["stats"] = map[string]any{
				"multi":              false,
				"stats":              snap,
				"dynamic_client_ids": dynIDs,
			}
		} else {
			out["stats"] = snap
		}
		out["control"] = controlState{
			Rate:   s.cfg.Engine.Rate(),
			Paused: s.cfg.Engine.Paused(),
		}
	}

	out["transports"] = s.buildTransportsGetResponse()
	return out
}

func (s *Server) handleLiveWS(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Engine == nil {
		http.Error(w, "engine unavailable", http.StatusServiceUnavailable)
		return
	}
	if !s.authorizeRequest(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Debug("api: websocket upgrade", "error", err)
		return
	}
	defer conn.Close()

	const maxMessageSize = 1 << 20
	conn.SetReadLimit(maxMessageSize)
	go pumpWSRead(conn)

	ticker := time.NewTicker(750 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			payload := s.snapshotForLive()
			_ = conn.SetWriteDeadline(time.Now().Add(8 * time.Second))
			if err := conn.WriteJSON(payload); err != nil {
				return
			}
		}
	}
}
