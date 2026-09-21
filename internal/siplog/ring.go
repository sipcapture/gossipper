package siplog

import (
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultCap = 1024
	maxRaw     = 16 << 10

	KindSIP = "sip"
	KindApp = "app"
)

// Record is one captured SIP datagram or an app/debug event.
type Record struct {
	Seq      uint64    `json:"seq"`
	Ts       time.Time `json:"ts"`
	Kind     string    `json:"kind,omitempty"`
	Dir      string    `json:"dir"`
	Peer     string    `json:"peer,omitempty"`
	Scenario string    `json:"scenario,omitempty"`
	Summary  string    `json:"summary"`
	Method   string    `json:"method,omitempty"`
	Status   int       `json:"status,omitempty"`
	CallID   string    `json:"call_id,omitempty"`
	Level    string    `json:"level,omitempty"`
	Raw      string    `json:"raw"`
}

// IsSIP is true for SIP datagrams (empty Kind is SIP for older records).
func (r Record) IsSIP() bool {
	return r.Kind == "" || r.Kind == KindSIP
}

// IsApp is true for scenario/app debug events.
func (r Record) IsApp() bool {
	return r.Kind == KindApp
}

// Ring is a bounded, process-local live-trace log (SIP + app debug).
type Ring struct {
	mu      sync.Mutex
	seq     uint64
	gen     uint64
	cap     int
	buf     []Record
	waiters []chan struct{}
	sipOn   atomic.Bool
	appOn   atomic.Bool
	appMin  atomic.Int32 // 0 debug, 1 info, 2 warn, 3 error
}

// New constructs a ring that keeps the last cap messages (default 1024).
// SIP capture starts on; app/debug capture starts off.
func New(cap int) *Ring {
	if cap < 1 {
		cap = defaultCap
	}
	r := &Ring{cap: cap, buf: make([]Record, 0, cap)}
	r.sipOn.Store(true)
	return r
}

// Add appends a datagram. raw is copied.
func (r *Ring) Add(dir, peer string, raw []byte) {
	r.AddTagged(dir, peer, "", raw)
}

// AddTagged appends a datagram tagged with the live scenario name (or REGISTER/OPTIONS).
func (r *Ring) AddTagged(dir, peer, scenario string, raw []byte) {
	if r == nil || !r.SIPEnabled() || len(raw) == 0 {
		return
	}
	if dir != "send" && dir != "recv" {
		dir = "recv"
	}
	method, status, summary := summarize(raw)
	r.append(Record{
		Ts:       time.Now().UTC(),
		Kind:     KindSIP,
		Dir:      dir,
		Peer:     peer,
		Scenario: scenario,
		Summary:  summary,
		Method:   method,
		Status:   status,
		CallID:   headerVal(raw, "Call-ID", "i"),
		Raw:      clipRaw(raw),
	})
}

// AddApp appends a scenario/app debug event when app capture is on and
// the event meets the current min log level.
func (r *Ring) AddApp(scenario, eventKind, summary, callID, raw, level string) {
	if r == nil || !r.AppEnabled() {
		return
	}
	rank, canon, ok := ParseMinLevel(level)
	if !ok {
		rank, canon = 0, "debug"
	}
	if rank < int(r.appMin.Load()) {
		return
	}
	if summary == "" {
		summary = eventKind
	}
	r.append(Record{
		Ts:       time.Now().UTC(),
		Kind:     KindApp,
		Dir:      "app",
		Scenario: scenario,
		Summary:  summary,
		Method:   eventKind,
		CallID:   callID,
		Level:    canon,
		Raw:      clipRaw([]byte(raw)),
	})
}

func (r *Ring) append(rec Record) {
	r.mu.Lock()
	r.seq++
	rec.Seq = r.seq
	if len(r.buf) == r.cap {
		copy(r.buf, r.buf[1:])
		r.buf[r.cap-1] = rec
	} else {
		r.buf = append(r.buf, rec)
	}
	r.notifyLocked()
	r.mu.Unlock()
}

// SIPEnabled reports whether SIP datagrams are recorded.
func (r *Ring) SIPEnabled() bool {
	return r != nil && r.sipOn.Load()
}

// AppEnabled reports whether scenario/app debug events are recorded.
func (r *Ring) AppEnabled() bool {
	return r != nil && r.appOn.Load()
}

// Capture returns the SIP and app capture flags (defaults: SIP on, app off).
func (r *Ring) Capture() (sip, app bool) {
	if r == nil {
		return true, false
	}
	return r.sipOn.Load(), r.appOn.Load()
}

// SetCapture updates independent SIP and app capture flags and wakes waiters.
func (r *Ring) SetCapture(sip, app bool) {
	if r == nil {
		return
	}
	changed := r.sipOn.Load() != sip || r.appOn.Load() != app
	r.sipOn.Store(sip)
	r.appOn.Store(app)
	if !changed {
		return
	}
	r.mu.Lock()
	r.notifyLocked()
	r.mu.Unlock()
}

// MinLevel is the app-debug floor: debug, info, warn, or error (default debug).
func (r *Ring) MinLevel() string {
	if r == nil {
		return "debug"
	}
	return LevelName(int(r.appMin.Load()))
}

// SetMinLevel changes the app-debug floor on the fly. Returns false if name is unknown.
func (r *Ring) SetMinLevel(name string) bool {
	if r == nil {
		return true
	}
	rank, _, ok := ParseMinLevel(name)
	if !ok {
		return false
	}
	if int(r.appMin.Load()) == rank {
		return true
	}
	r.appMin.Store(int32(rank))
	r.mu.Lock()
	r.notifyLocked()
	r.mu.Unlock()
	return true
}

// ParseMinLevel maps debug|info|warn|error. Empty name is debug.
func ParseMinLevel(name string) (rank int, canon string, ok bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "debug":
		return 0, "debug", true
	case "info":
		return 1, "info", true
	case "warn", "warning":
		return 2, "warn", true
	case "error", "err":
		return 3, "error", true
	default:
		return 0, "", false
	}
}

// LevelName is the canonical name for a min-level rank.
func LevelName(rank int) string {
	switch rank {
	case 1:
		return "info"
	case 2:
		return "warn"
	case 3:
		return "error"
	default:
		return "debug"
	}
}

// LevelRank is 0–3 for filtering. Unknown/empty is debug (0).
func LevelRank(name string) int {
	rank, _, ok := ParseMinLevel(name)
	if !ok {
		return 0
	}
	return rank
}

// Since returns records with Seq > after, oldest first, capped at limit.
// next is the highest Seq in the ring (so the client can poll incrementally).
func (r *Ring) Since(after uint64, limit int) (next uint64, recs []Record) {
	next, _, recs = r.Snapshot(after, limit)
	return next, recs
}

// Snapshot is Since plus a generation that increments on Clear.
func (r *Ring) Snapshot(after uint64, limit int) (next, gen uint64, recs []Record) {
	if r == nil {
		return 0, 0, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	next = r.seq
	gen = r.gen
	nbuf := len(r.buf)
	if nbuf == 0 {
		return next, gen, nil
	}
	if limit < 1 || limit > nbuf {
		limit = nbuf
	}
	out := make([]Record, 0, nbuf)
	for _, rec := range r.buf {
		if rec.Seq <= after {
			continue
		}
		out = append(out, rec)
		if len(out) >= limit {
			break
		}
	}
	return next, gen, out
}

// Subscribe fires (non-blocking) on Add and Clear. cancel unregisters the waiter.
func (r *Ring) Subscribe() (ch <-chan struct{}, cancel func()) {
	n := make(chan struct{}, 1)
	if r == nil {
		return n, func() {}
	}
	r.mu.Lock()
	r.waiters = append(r.waiters, n)
	r.mu.Unlock()
	var once sync.Once
	return n, func() {
		once.Do(func() {
			r.mu.Lock()
			defer r.mu.Unlock()
			out := r.waiters[:0]
			for _, w := range r.waiters {
				if w != n {
					out = append(out, w)
				}
			}
			r.waiters = out
		})
	}
}

func (r *Ring) notifyLocked() {
	for _, ch := range r.waiters {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// Clear drops buffered messages. Seq keeps growing so pollers do not replay.
func (r *Ring) Clear() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.buf = r.buf[:0]
	r.gen++
	r.notifyLocked()
	r.mu.Unlock()
}

func clipRaw(raw []byte) string {
	if len(raw) > maxRaw {
		raw = raw[:maxRaw]
	}
	return string(raw)
}

func summarize(raw []byte) (method string, status int, summary string) {
	line := firstLine(raw)
	if strings.HasPrefix(line, "SIP/2.0 ") {
		rest := strings.TrimSpace(strings.TrimPrefix(line, "SIP/2.0 "))
		code, _, _ := strings.Cut(rest, " ")
		if n, err := strconv.Atoi(code); err == nil {
			status = n
		}
		return "", status, line
	}
	method, _, _ = strings.Cut(line, " ")
	return method, 0, line
}

func firstLine(raw []byte) string {
	s := string(raw)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

func headerVal(raw []byte, names ...string) string {
	s := strings.ReplaceAll(string(raw), "\r\n", "\n")
	if i := strings.Index(s, "\n\n"); i >= 0 {
		s = s[:i]
	}
	for _, line := range strings.Split(s, "\n") {
		name, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		for _, want := range names {
			if strings.EqualFold(name, want) {
				return strings.TrimSpace(val)
			}
		}
	}
	return ""
}
