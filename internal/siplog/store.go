package siplog

import (
	"database/sql"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const persistCap = 2000

type callStore struct {
	db *sql.DB
}

func openCallStore(path string) (*callStore, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("calls sqlite path is empty")
	}
	if dir := filepath.Dir(path); dir != "." && dir != string(filepath.Separator) {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("calls sqlite: mkdir %q: %w", dir, err)
		}
	}
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(30 * time.Minute)
	if err := migrateCalls(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &callStore{db: db}, nil
}

func migrateCalls(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS calls (
			call_id TEXT PRIMARY KEY,
			scenario TEXT NOT NULL DEFAULT '',
			from_uri TEXT NOT NULL DEFAULT '',
			to_uri TEXT NOT NULL DEFAULT '',
			peer TEXT NOT NULL DEFAULT '',
			direction TEXT NOT NULL DEFAULT '',
			state TEXT NOT NULL DEFAULT 'active',
			result TEXT NOT NULL DEFAULT '',
			error TEXT NOT NULL DEFAULT '',
			started_at TEXT NOT NULL,
			ended_at TEXT,
			sip_count INTEGER NOT NULL DEFAULT 0,
			sip_send INTEGER NOT NULL DEFAULT 0,
			sip_recv INTEGER NOT NULL DEFAULT 0,
			debug_count INTEGER NOT NULL DEFAULT 0,
			rtp_count INTEGER NOT NULL DEFAULT 0,
			rtp_send INTEGER NOT NULL DEFAULT 0,
			rtp_recv INTEGER NOT NULL DEFAULT 0,
			rtp_bytes_send INTEGER NOT NULL DEFAULT 0,
			rtp_bytes_recv INTEGER NOT NULL DEFAULT 0,
			rtp_dest TEXT NOT NULL DEFAULT '',
			rtp_src TEXT NOT NULL DEFAULT '',
			payload_type INTEGER NOT NULL DEFAULT 0,
			codec TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_calls_started_at ON calls(started_at DESC);`,
		`CREATE TABLE IF NOT EXISTS call_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			call_id TEXT NOT NULL,
			kind TEXT NOT NULL,
			seq INTEGER NOT NULL,
			ts TEXT NOT NULL,
			dir TEXT NOT NULL DEFAULT '',
			peer TEXT NOT NULL DEFAULT '',
			scenario TEXT NOT NULL DEFAULT '',
			summary TEXT NOT NULL DEFAULT '',
			method TEXT NOT NULL DEFAULT '',
			status INTEGER NOT NULL DEFAULT 0,
			level TEXT NOT NULL DEFAULT '',
			raw TEXT NOT NULL DEFAULT '',
			FOREIGN KEY (call_id) REFERENCES calls(call_id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_call_events_call ON call_events(call_id, seq);`,
		`CREATE TABLE IF NOT EXISTS call_rtp (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			call_id TEXT NOT NULL,
			ts TEXT NOT NULL,
			dir TEXT NOT NULL DEFAULT '',
			src_ip TEXT NOT NULL DEFAULT '',
			src_port INTEGER NOT NULL DEFAULT 0,
			dst_ip TEXT NOT NULL DEFAULT '',
			dst_port INTEGER NOT NULL DEFAULT 0,
			payload BLOB,
			FOREIGN KEY (call_id) REFERENCES calls(call_id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_call_rtp_call ON call_rtp(call_id);`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("calls sqlite migrate: %w", err)
		}
	}
	return nil
}

func (s *callStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *callStore) Upsert(sum CallSummary) error {
	if s == nil || s.db == nil || strings.TrimSpace(sum.CallID) == "" {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	started := formatTime(sum.StartedAt)
	if started == "" {
		started = now
	}
	var ended any
	if !sum.EndedAt.IsZero() {
		ended = formatTime(sum.EndedAt)
	}
	_, err := s.db.Exec(`INSERT INTO calls (
			call_id, scenario, from_uri, to_uri, peer, direction, state, result, error,
			started_at, ended_at, sip_count, sip_send, sip_recv, debug_count,
			rtp_count, rtp_send, rtp_recv, rtp_bytes_send, rtp_bytes_recv,
			rtp_dest, rtp_src, payload_type, codec, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(call_id) DO UPDATE SET
			scenario=CASE WHEN excluded.scenario != '' THEN excluded.scenario ELSE calls.scenario END,
			from_uri=CASE WHEN excluded.from_uri != '' THEN excluded.from_uri ELSE calls.from_uri END,
			to_uri=CASE WHEN excluded.to_uri != '' THEN excluded.to_uri ELSE calls.to_uri END,
			peer=CASE WHEN excluded.peer != '' THEN excluded.peer ELSE calls.peer END,
			direction=CASE WHEN excluded.direction != '' THEN excluded.direction ELSE calls.direction END,
			state=excluded.state,
			result=CASE WHEN excluded.result != '' THEN excluded.result ELSE calls.result END,
			error=CASE WHEN excluded.error != '' THEN excluded.error ELSE calls.error END,
			ended_at=COALESCE(excluded.ended_at, calls.ended_at),
			sip_count=excluded.sip_count,
			sip_send=excluded.sip_send,
			sip_recv=excluded.sip_recv,
			debug_count=excluded.debug_count,
			rtp_count=excluded.rtp_count,
			rtp_send=excluded.rtp_send,
			rtp_recv=excluded.rtp_recv,
			rtp_bytes_send=excluded.rtp_bytes_send,
			rtp_bytes_recv=excluded.rtp_bytes_recv,
			rtp_dest=CASE WHEN excluded.rtp_dest != '' THEN excluded.rtp_dest ELSE calls.rtp_dest END,
			rtp_src=CASE WHEN excluded.rtp_src != '' THEN excluded.rtp_src ELSE calls.rtp_src END,
			payload_type=excluded.payload_type,
			codec=CASE WHEN excluded.codec != '' THEN excluded.codec ELSE calls.codec END,
			updated_at=excluded.updated_at`,
		sum.CallID, sum.Scenario, sum.From, sum.To, sum.Peer, sum.Direction, sum.State, sum.Result, sum.Error,
		started, ended, sum.SIPCount, sum.SIPSend, sum.SIPRecv, sum.DebugCount,
		sum.RTPCount, sum.RTPSend, sum.RTPRecv, sum.RTPBytesSend, sum.RTPBytesRecv,
		sum.RTPDest, sum.RTPSrc, sum.PayloadType, sum.Codec, now,
	)
	return err
}

func (s *callStore) InsertEvent(rec Record) error {
	if s == nil || s.db == nil || strings.TrimSpace(rec.CallID) == "" {
		return nil
	}
	kind := rec.Kind
	if kind == "" {
		kind = KindSIP
	}
	_, err := s.db.Exec(`INSERT INTO call_events (
			call_id, kind, seq, ts, dir, peer, scenario, summary, method, status, level, raw
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rec.CallID, kind, rec.Seq, formatTime(rec.Ts), rec.Dir, rec.Peer, rec.Scenario,
		rec.Summary, rec.Method, rec.Status, rec.Level, rec.Raw,
	)
	return err
}

func (s *callStore) InsertRTP(pkt RTPPacket) error {
	if s == nil || s.db == nil || strings.TrimSpace(pkt.CallID) == "" {
		return nil
	}
	_, err := s.db.Exec(`INSERT INTO call_rtp (
			call_id, ts, dir, src_ip, src_port, dst_ip, dst_port, payload
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		pkt.CallID, formatTime(pkt.Ts), pkt.Dir, ipString(pkt.SrcIP), int(pkt.SrcPort),
		ipString(pkt.DstIP), int(pkt.DstPort), pkt.Payload,
	)
	if err != nil {
		return err
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM call_rtp WHERE call_id = ?`, pkt.CallID).Scan(&n); err != nil {
		return err
	}
	extra := n - callRTPCap
	if extra <= 0 {
		return nil
	}
	_, err = s.db.Exec(`DELETE FROM call_rtp WHERE id IN (
		SELECT id FROM call_rtp WHERE call_id = ? ORDER BY id ASC LIMIT ?
	)`, pkt.CallID, extra)
	return err
}

func (s *callStore) Prune(capn int) error {
	if s == nil || s.db == nil {
		return nil
	}
	if capn < 1 {
		capn = persistCap
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM calls`).Scan(&n); err != nil {
		return err
	}
	extra := n - capn
	if extra <= 0 {
		return nil
	}
	_, err := s.db.Exec(`DELETE FROM calls WHERE call_id IN (
		SELECT call_id FROM calls ORDER BY started_at ASC, call_id ASC LIMIT ?
	)`, extra)
	return err
}

func (s *callStore) Clear() error {
	if s == nil || s.db == nil {
		return nil
	}
	if _, err := s.db.Exec(`DELETE FROM call_rtp`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`DELETE FROM call_events`); err != nil {
		return err
	}
	_, err := s.db.Exec(`DELETE FROM calls`)
	return err
}

func (s *callStore) List(limit int) ([]CallSummary, error) {
	if s == nil || s.db == nil {
		return []CallSummary{}, nil
	}
	if limit < 1 {
		limit = persistCap
	}
	rows, err := s.db.Query(`SELECT
			call_id, scenario, from_uri, to_uri, peer, direction, state, result, error,
			started_at, ended_at, sip_count, sip_send, sip_recv, debug_count,
			rtp_count, rtp_send, rtp_recv, rtp_bytes_send, rtp_bytes_recv,
			rtp_dest, rtp_src, payload_type, codec
		FROM calls ORDER BY started_at DESC, call_id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]CallSummary, 0)
	now := time.Now().UTC()
	for rows.Next() {
		sum, err := scanSummary(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, fillDuration(sum, now))
	}
	return out, rows.Err()
}

func (s *callStore) Get(callID string) (CallDetail, bool, error) {
	if s == nil || s.db == nil {
		return CallDetail{}, false, nil
	}
	callID = strings.TrimSpace(callID)
	if callID == "" {
		return CallDetail{}, false, nil
	}
	row := s.db.QueryRow(`SELECT
			call_id, scenario, from_uri, to_uri, peer, direction, state, result, error,
			started_at, ended_at, sip_count, sip_send, sip_recv, debug_count,
			rtp_count, rtp_send, rtp_recv, rtp_bytes_send, rtp_bytes_recv,
			rtp_dest, rtp_src, payload_type, codec
		FROM calls WHERE call_id = ?`, callID)
	sum, err := scanSummary(row)
	if errors.Is(err, sql.ErrNoRows) {
		return CallDetail{}, false, nil
	}
	if err != nil {
		return CallDetail{}, false, err
	}
	d := CallDetail{CallSummary: fillDuration(sum, time.Now().UTC())}
	evRows, err := s.db.Query(`SELECT kind, seq, ts, dir, peer, scenario, summary, method, status, level, raw
		FROM call_events WHERE call_id = ? ORDER BY seq ASC, id ASC`, callID)
	if err != nil {
		return CallDetail{}, false, err
	}
	defer evRows.Close()
	d.SIP = []Record{}
	d.Debug = []Record{}
	for evRows.Next() {
		rec, err := scanEvent(evRows, callID)
		if err != nil {
			return CallDetail{}, false, err
		}
		if rec.IsApp() {
			d.Debug = append(d.Debug, rec)
		} else if rec.IsSIP() {
			d.SIP = append(d.SIP, rec)
		}
	}
	if err := evRows.Err(); err != nil {
		return CallDetail{}, false, err
	}
	pkts, err := s.listRTP(callID)
	if err != nil {
		return CallDetail{}, false, err
	}
	d.RTP = samplesFromRTP(pkts)
	return d, true, nil
}

func (s *callStore) Dump(callID string) ([]Record, []RTPPacket, bool, error) {
	if s == nil || s.db == nil {
		return nil, nil, false, nil
	}
	callID = strings.TrimSpace(callID)
	if callID == "" {
		return nil, nil, false, nil
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM calls WHERE call_id = ?`, callID).Scan(&n); err != nil {
		return nil, nil, false, err
	}
	if n == 0 {
		return nil, nil, false, nil
	}
	evRows, err := s.db.Query(`SELECT kind, seq, ts, dir, peer, scenario, summary, method, status, level, raw
		FROM call_events WHERE call_id = ? ORDER BY ts ASC, seq ASC, id ASC`, callID)
	if err != nil {
		return nil, nil, false, err
	}
	defer evRows.Close()
	recs := make([]Record, 0)
	for evRows.Next() {
		rec, err := scanEvent(evRows, callID)
		if err != nil {
			return nil, nil, false, err
		}
		recs = append(recs, rec)
	}
	if err := evRows.Err(); err != nil {
		return nil, nil, false, err
	}
	pkts, err := s.listRTP(callID)
	if err != nil {
		return nil, nil, false, err
	}
	return recs, pkts, true, nil
}

func (s *callStore) listRTP(callID string) ([]RTPPacket, error) {
	rows, err := s.db.Query(`SELECT ts, dir, src_ip, src_port, dst_ip, dst_port, payload
		FROM call_rtp WHERE call_id = ? ORDER BY id ASC`, callID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]RTPPacket, 0)
	for rows.Next() {
		var (
			ts, dir, srcIP, dstIP string
			srcPort, dstPort      int
			payload               []byte
		)
		if err := rows.Scan(&ts, &dir, &srcIP, &srcPort, &dstIP, &dstPort, &payload); err != nil {
			return nil, err
		}
		out = append(out, RTPPacket{
			Ts:      parseTime(ts),
			Dir:     dir,
			SrcIP:   parseIP(srcIP),
			SrcPort: udpPort(srcPort),
			DstIP:   parseIP(dstIP),
			DstPort: udpPort(dstPort),
			CallID:  callID,
			Payload: payload,
		})
	}
	return out, rows.Err()
}

type summaryScanner interface {
	Scan(dest ...any) error
}

func scanSummary(row summaryScanner) (CallSummary, error) {
	var (
		sum                        CallSummary
		started, ended             sql.NullString
		sipCount, sipSend, sipRecv int
		debugCount, rtpCount       int
		rtpSend, rtpRecv           int
		rtpBytesSend, rtpBytesRecv int64
		payloadType                int
	)
	err := row.Scan(
		&sum.CallID, &sum.Scenario, &sum.From, &sum.To, &sum.Peer, &sum.Direction,
		&sum.State, &sum.Result, &sum.Error, &started, &ended,
		&sipCount, &sipSend, &sipRecv, &debugCount,
		&rtpCount, &rtpSend, &rtpRecv, &rtpBytesSend, &rtpBytesRecv,
		&sum.RTPDest, &sum.RTPSrc, &payloadType, &sum.Codec,
	)
	if err != nil {
		return CallSummary{}, err
	}
	sum.StartedAt = parseTime(started.String)
	if ended.Valid {
		sum.EndedAt = parseTime(ended.String)
	}
	sum.SIPCount = sipCount
	sum.SIPSend = sipSend
	sum.SIPRecv = sipRecv
	sum.DebugCount = debugCount
	sum.RTPCount = rtpCount
	sum.RTPSend = rtpSend
	sum.RTPRecv = rtpRecv
	sum.RTPBytesSend = rtpBytesSend
	sum.RTPBytesRecv = rtpBytesRecv
	sum.PayloadType = payloadType
	return sum, nil
}

func scanEvent(rows *sql.Rows, callID string) (Record, error) {
	var rec Record
	var ts string
	var seq int64
	if err := rows.Scan(&rec.Kind, &seq, &ts, &rec.Dir, &rec.Peer, &rec.Scenario,
		&rec.Summary, &rec.Method, &rec.Status, &rec.Level, &rec.Raw); err != nil {
		return Record{}, err
	}
	rec.CallID = callID
	rec.Seq = uint64(seq)
	rec.Ts = parseTime(ts)
	return rec, nil
}

func samplesFromRTP(pkts []RTPPacket) []RTPSample {
	out := make([]RTPSample, 0, min(len(pkts), callRTPSample))
	start := 0
	if len(pkts) > callRTPSample {
		start = len(pkts) - callRTPSample
	}
	for _, p := range pkts[start:] {
		pt, seq := rtpPTSeq(p.Payload)
		out = append(out, RTPSample{
			Ts:   p.Ts,
			Dir:  p.Dir,
			Src:  net.JoinHostPort(ipString(p.SrcIP), fmt.Sprintf("%d", p.SrcPort)),
			Dst:  net.JoinHostPort(ipString(p.DstIP), fmt.Sprintf("%d", p.DstPort)),
			PT:   pt,
			Seq:  seq,
			Size: len(p.Payload),
		})
	}
	return out
}

func fillDuration(s CallSummary, now time.Time) CallSummary {
	end := s.EndedAt
	if end.IsZero() {
		end = now
	}
	if !s.StartedAt.IsZero() {
		s.DurationMS = end.Sub(s.StartedAt).Milliseconds()
		if s.DurationMS < 0 {
			s.DurationMS = 0
		}
	}
	if s.State == "" {
		s.State = "active"
	}
	return s
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC()
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC()
	}
	return time.Time{}
}

func ipString(ip net.IP) string {
	if ip == nil {
		return ""
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	return ip.String()
}

func parseIP(s string) net.IP {
	s = strings.TrimSpace(s)
	if s == "" {
		return net.IPv4(0, 0, 0, 0)
	}
	if ip := net.ParseIP(s); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			return v4
		}
		return ip
	}
	return net.IPv4(0, 0, 0, 0)
}
