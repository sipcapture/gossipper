package backfill

import (
	"encoding/json"
	"time"
)

// SBCLogEntry is a CDR-style log entry for one call, correlated by CallID.
type SBCLogEntry struct {
	Timestamp        time.Time `json:"timestamp"`
	CallID           string    `json:"call_id"`
	From             string    `json:"from"`
	To               string    `json:"to"`
	SrcIP            string    `json:"src_ip"`
	DstIP            string    `json:"dst_ip"`
	DurationMS       int64     `json:"duration_ms"`
	Status           int       `json:"status"`
	Direction        string    `json:"direction"`
	DisconnectReason string    `json:"disconnect_reason"`
}

func buildLogEntry(cfg Config, c syntheticCall) SBCLogEntry {
	reason := "caller_bye"
	if c.Failed {
		reason = "server_reject"
	}
	return SBCLogEntry{
		Timestamp:        c.Start,
		CallID:           c.CallID,
		From:             "sip:user@" + cfg.SrcIP,
		To:               "sip:user@" + cfg.DstIP,
		SrcIP:            cfg.SrcIP,
		DstIP:            cfg.DstIP,
		DurationMS:       c.Duration.Milliseconds(),
		Status:           c.Status,
		Direction:        "outbound",
		DisconnectReason: reason,
	}
}

func marshalLogEntry(e SBCLogEntry) ([]byte, error) {
	return json.Marshal(e)
}
