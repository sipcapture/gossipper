package siplog

import (
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	callCap       = 200
	callSIPCap    = 256
	callAppCap    = 512
	callRTPSample = 40
	callRTPCap    = 512
)

// CallSummary is one CDR row for the Calls list.
type CallSummary struct {
	CallID       string    `json:"call_id"`
	Scenario     string    `json:"scenario,omitempty"`
	From         string    `json:"from,omitempty"`
	To           string    `json:"to,omitempty"`
	Peer         string    `json:"peer,omitempty"`
	Direction    string    `json:"direction,omitempty"`
	State        string    `json:"state"`
	Result       string    `json:"result,omitempty"`
	Error        string    `json:"error,omitempty"`
	StartedAt    time.Time `json:"started_at"`
	EndedAt      time.Time `json:"ended_at,omitempty"`
	DurationMS   int64     `json:"duration_ms"`
	SIPCount     int       `json:"sip_count"`
	SIPSend      int       `json:"sip_send"`
	SIPRecv      int       `json:"sip_recv"`
	DebugCount   int       `json:"debug_count"`
	RTPCount     int       `json:"rtp_count"`
	RTPSend      int       `json:"rtp_send"`
	RTPRecv      int       `json:"rtp_recv"`
	RTPBytesSend int64     `json:"rtp_bytes_send"`
	RTPBytesRecv int64     `json:"rtp_bytes_recv"`
	RTPDest      string    `json:"rtp_dest,omitempty"`
	RTPSrc       string    `json:"rtp_src,omitempty"`
	PayloadType  int       `json:"payload_type"`
	Codec        string    `json:"codec,omitempty"`
}

// RTPSample is one RTP datagram header (no payload) for the call detail.
type RTPSample struct {
	Ts   time.Time `json:"ts"`
	Dir  string    `json:"dir"`
	Src  string    `json:"src"`
	Dst  string    `json:"dst"`
	PT   int       `json:"pt"`
	Seq  uint16    `json:"seq"`
	Size int       `json:"size"`
}

// CallDetail is the Calls drill-in payload: summary plus SIP / debug / RTP.
type CallDetail struct {
	CallSummary
	SIP   []Record    `json:"sip"`
	Debug []Record    `json:"debug"`
	RTP   []RTPSample `json:"rtp"`
}

type callEntry struct {
	sum  CallSummary
	sip  []Record
	app  []Record
	rtp  []RTPSample
	pkts []RTPPacket
	seq  uint64
}

// Catalog indexes SIP, debug, and RTP by Call-ID for the Calls (CDR) page.
// Capture is always on and independent of Live Trace toggles. When a SQLite
// store is attached, writes are queued asynchronously and List/Get/Dump
// overlay the database so calls survive process restart.
type Catalog struct {
	mu         sync.Mutex
	cap        int
	order      []string
	byID       map[string]*callEntry
	persist    *callStore
	jobs       chan persistJob
	workerDone chan struct{}
	persistOn  atomic.Bool
}

func newCatalog(cap int) *Catalog {
	if cap < 1 {
		cap = callCap
	}
	return &Catalog{cap: cap, byID: make(map[string]*callEntry)}
}

// List returns CDR rows, newest first.
func (c *Catalog) List() []CallSummary {
	if c == nil {
		return []CallSummary{}
	}
	mem := c.listMem()
	st := c.store()
	if st == nil {
		return mem
	}
	db, err := st.List(persistCap)
	if err != nil || len(db) == 0 {
		if len(mem) > 0 {
			return mem
		}
		if db == nil {
			return []CallSummary{}
		}
		return db
	}
	return mergeCallLists(db, mem)
}

func (c *Catalog) listMem() []CallSummary {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]CallSummary, 0, len(c.order))
	now := time.Now().UTC()
	for i := len(c.order) - 1; i >= 0; i-- {
		e := c.byID[c.order[i]]
		if e == nil {
			continue
		}
		out = append(out, e.snapshot(now))
	}
	return out
}

func (c *Catalog) store() *callStore {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	st := c.persist
	c.mu.Unlock()
	return st
}

func mergeCallLists(db, mem []CallSummary) []CallSummary {
	byID := make(map[string]CallSummary, len(db)+len(mem))
	order := make([]string, 0, len(db)+len(mem))
	seen := make(map[string]struct{}, len(db)+len(mem))
	for _, row := range db {
		byID[row.CallID] = row
		if _, ok := seen[row.CallID]; ok {
			continue
		}
		seen[row.CallID] = struct{}{}
		order = append(order, row.CallID)
	}
	for _, row := range mem {
		byID[row.CallID] = row
		if _, ok := seen[row.CallID]; ok {
			continue
		}
		seen[row.CallID] = struct{}{}
		order = append(order, row.CallID)
	}
	sort.SliceStable(order, func(i, j int) bool {
		a, b := byID[order[i]], byID[order[j]]
		if a.StartedAt.Equal(b.StartedAt) {
			return a.CallID > b.CallID
		}
		return a.StartedAt.After(b.StartedAt)
	})
	out := make([]CallSummary, 0, len(order))
	for _, id := range order {
		out = append(out, byID[id])
	}
	return out
}

// Get returns one call or false if unknown.
func (c *Catalog) Get(callID string) (CallDetail, bool) {
	if c == nil {
		return CallDetail{}, false
	}
	callID = strings.TrimSpace(callID)
	if callID == "" {
		return CallDetail{}, false
	}
	if d, ok := c.getMem(callID); ok {
		return d, true
	}
	st := c.store()
	if st == nil {
		return CallDetail{}, false
	}
	d, ok, err := st.Get(callID)
	if err != nil || !ok {
		return CallDetail{}, false
	}
	return d, true
}

func (c *Catalog) getMem(callID string) (CallDetail, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.byID[callID]
	if e == nil {
		return CallDetail{}, false
	}
	d := CallDetail{CallSummary: e.snapshot(time.Now().UTC())}
	d.SIP = append([]Record(nil), e.sip...)
	d.Debug = append([]Record(nil), e.app...)
	d.RTP = append([]RTPSample(nil), e.rtp...)
	if d.SIP == nil {
		d.SIP = []Record{}
	}
	if d.Debug == nil {
		d.Debug = []Record{}
	}
	if d.RTP == nil {
		d.RTP = []RTPSample{}
	}
	return d, true
}

// Clear drops every stored call.
func (c *Catalog) Clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.order = c.order[:0]
	c.byID = make(map[string]*callEntry)
	c.mu.Unlock()
	c.enqueue(persistJob{kind: jobClear})
	c.Flush()
}

// AddSIP records one SIP datagram when it belongs to a dialog (not REGISTER/OPTIONS).
func (c *Catalog) AddSIP(rec Record) {
	if c == nil || !dialogSIP(rec) {
		return
	}
	c.mu.Lock()
	e := c.ensureLocked(rec.CallID, rec.Ts)
	if e == nil {
		c.mu.Unlock()
		return
	}
	e.seq++
	rec.Seq = e.seq
	if rec.Scenario != "" && rec.Scenario != "REGISTER" && rec.Scenario != "OPTIONS" && e.sum.Scenario == "" {
		e.sum.Scenario = rec.Scenario
	}
	if rec.Peer != "" && e.sum.Peer == "" {
		e.sum.Peer = rec.Peer
	}
	if e.sum.From == "" {
		e.sum.From = sipAddr(headerVal([]byte(rec.Raw), "From", "f"))
	}
	if e.sum.To == "" {
		e.sum.To = sipAddr(headerVal([]byte(rec.Raw), "To", "t"))
	}
	if e.sum.Direction == "" && strings.EqualFold(rec.Method, "INVITE") {
		if rec.Dir == "recv" {
			e.sum.Direction = "inbound"
		} else if rec.Dir == "send" {
			e.sum.Direction = "outbound"
		}
	}
	if rec.Dir == "send" {
		e.sum.SIPSend++
	} else if rec.Dir == "recv" {
		e.sum.SIPRecv++
	}
	e.sum.SIPCount = e.sum.SIPSend + e.sum.SIPRecv
	applySIPState(e, rec)
	e.sip = appendCapped(e.sip, rec, callSIPCap)
	sum := e.snapshot(time.Now().UTC())
	c.mu.Unlock()
	c.enqueue(persistJob{kind: jobSIP, sum: sum, rec: rec})
}

// AddApp records a debug/app event keyed by Call-ID.
func (c *Catalog) AddApp(rec Record) {
	if c == nil || strings.TrimSpace(rec.CallID) == "" {
		return
	}
	c.mu.Lock()
	e := c.ensureLocked(rec.CallID, rec.Ts)
	if e == nil {
		c.mu.Unlock()
		return
	}
	if rec.Scenario != "" && rec.Scenario != "REGISTER" && rec.Scenario != "OPTIONS" && e.sum.Scenario == "" {
		e.sum.Scenario = rec.Scenario
	}
	e.seq++
	rec.Seq = e.seq
	switch rec.Method {
	case "call.started":
		if e.sum.State == "" {
			e.sum.State = "active"
		}
	case "call.ended":
		e.sum.EndedAt = rec.Ts
		if result := attrKV(rec.Raw, "result"); result != "" {
			e.sum.Result = result
		}
		if errv := attrKV(rec.Raw, "error"); errv != "" {
			e.sum.Error = errv
		}
		if e.sum.Result != "" && e.sum.Result != "success" {
			e.sum.State = "failed"
		} else {
			e.sum.State = "ended"
			if e.sum.Result == "" {
				e.sum.Result = "success"
			}
		}
	}
	e.app = appendCapped(e.app, rec, callAppCap)
	e.sum.DebugCount = len(e.app)
	sum := e.snapshot(time.Now().UTC())
	c.mu.Unlock()
	c.enqueue(persistJob{kind: jobApp, sum: sum, rec: rec})
}

// AddRTP records RTP counters, a header sample, and a clipped payload for zip dump.
func (c *Catalog) AddRTP(dir, srcIP string, srcPort int, dstIP string, dstPort int, callID string, payload []byte) {
	if c == nil || strings.TrimSpace(callID) == "" || len(payload) == 0 {
		return
	}
	if dir != "send" && dir != "recv" {
		dir = "send"
	}
	src := net.JoinHostPort(srcIP, fmt.Sprintf("%d", udpPort(srcPort)))
	dst := net.JoinHostPort(dstIP, fmt.Sprintf("%d", udpPort(dstPort)))
	pt, seq := rtpPTSeq(payload)
	now := time.Now().UTC()
	sample := RTPSample{Ts: now, Dir: dir, Src: src, Dst: dst, PT: pt, Seq: seq, Size: len(payload)}

	c.mu.Lock()
	e := c.ensureLocked(callID, now)
	if e == nil {
		c.mu.Unlock()
		return
	}
	n := int64(len(payload))
	if dir == "send" {
		e.sum.RTPSend++
		e.sum.RTPBytesSend += n
		e.sum.RTPDest = dst
		e.sum.RTPSrc = src
	} else {
		e.sum.RTPRecv++
		e.sum.RTPBytesRecv += n
		if e.sum.RTPDest == "" {
			e.sum.RTPDest = src
		}
		if e.sum.RTPSrc == "" {
			e.sum.RTPSrc = dst
		}
	}
	e.sum.RTPCount = e.sum.RTPSend + e.sum.RTPRecv
	if pt > 0 || e.sum.PayloadType == 0 {
		e.sum.PayloadType = pt
		e.sum.Codec = codecName(pt)
	}
	e.rtp = appendCapped(e.rtp, sample, callRTPSample)
	raw := payload
	if len(raw) > maxRTPRaw {
		raw = raw[:maxRTPRaw]
	}
	pkt := RTPPacket{
		Ts:      now,
		Dir:     dir,
		SrcIP:   peerIPv4(srcIP),
		SrcPort: udpPort(srcPort),
		DstIP:   peerIPv4(dstIP),
		DstPort: udpPort(dstPort),
		CallID:  callID,
		Payload: append([]byte(nil), raw...),
	}
	e.pkts = appendCapped(e.pkts, pkt, callRTPCap)
	sum := e.snapshot(now)
	c.mu.Unlock()
	c.enqueue(persistJob{kind: jobRTP, sum: sum, rtp: cloneRTP(pkt)})
}

// Dump returns SIP+debug records (time order) and captured RTP for a zip export.
func (c *Catalog) Dump(callID string) (recs []Record, rtp []RTPPacket, ok bool) {
	if c == nil {
		return nil, nil, false
	}
	callID = strings.TrimSpace(callID)
	if callID == "" {
		return nil, nil, false
	}
	if recs, rtp, ok = c.dumpMem(callID); ok {
		return recs, rtp, true
	}
	st := c.store()
	if st == nil {
		return nil, nil, false
	}
	recs, rtp, ok, err := st.Dump(callID)
	if err != nil || !ok {
		return nil, nil, false
	}
	return recs, rtp, true
}

func (c *Catalog) dumpMem(callID string) (recs []Record, rtp []RTPPacket, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.byID[callID]
	if e == nil {
		return nil, nil, false
	}
	recs = make([]Record, 0, len(e.sip)+len(e.app))
	recs = append(recs, e.sip...)
	recs = append(recs, e.app...)
	sort.SliceStable(recs, func(i, j int) bool {
		if recs[i].Ts.Equal(recs[j].Ts) {
			return recs[i].Seq < recs[j].Seq
		}
		return recs[i].Ts.Before(recs[j].Ts)
	})
	rtp = append([]RTPPacket(nil), e.pkts...)
	return recs, rtp, true
}

func (c *Catalog) ensureLocked(callID string, ts time.Time) *callEntry {
	callID = strings.TrimSpace(callID)
	if callID == "" {
		return nil
	}
	if e := c.byID[callID]; e != nil {
		return e
	}
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	e := &callEntry{sum: CallSummary{CallID: callID, State: "active", StartedAt: ts}}
	c.byID[callID] = e
	c.order = append(c.order, callID)
	for len(c.order) > c.cap {
		old := c.order[0]
		c.order = c.order[1:]
		delete(c.byID, old)
	}
	return e
}

func (e *callEntry) snapshot(now time.Time) CallSummary {
	s := e.sum
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

func applySIPState(e *callEntry, rec Record) {
	m := strings.ToUpper(rec.Method)
	if m == "BYE" || m == "CANCEL" {
		if e.sum.State == "active" {
			e.sum.State = "ended"
		}
		if e.sum.EndedAt.IsZero() {
			e.sum.EndedAt = rec.Ts
		}
		return
	}
	if rec.Status >= 400 && rec.Status != 401 && rec.Status != 407 {
		cseq := headerVal([]byte(rec.Raw), "CSeq")
		fields := strings.Fields(cseq)
		cm := ""
		if len(fields) >= 2 {
			cm = strings.ToUpper(fields[len(fields)-1])
		}
		if cm == "" || cm == "INVITE" {
			e.sum.State = "failed"
			e.sum.Result = fmt.Sprintf("%d", rec.Status)
			if e.sum.EndedAt.IsZero() {
				e.sum.EndedAt = rec.Ts
			}
		}
	}
}

func dialogSIP(rec Record) bool {
	if strings.TrimSpace(rec.CallID) == "" {
		return false
	}
	m := strings.ToUpper(strings.TrimSpace(rec.Method))
	if m == "REGISTER" || m == "OPTIONS" {
		return false
	}
	sc := strings.ToUpper(strings.TrimSpace(rec.Scenario))
	if sc == "REGISTER" || sc == "OPTIONS" {
		return false
	}
	cseq := headerVal([]byte(rec.Raw), "CSeq")
	fields := strings.Fields(cseq)
	if len(fields) >= 2 {
		cm := strings.ToUpper(fields[len(fields)-1])
		if cm == "REGISTER" || cm == "OPTIONS" {
			return false
		}
	}
	return true
}

func sipAddr(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if i := strings.IndexByte(v, '<'); i >= 0 {
		if j := strings.IndexByte(v[i:], '>'); j > 0 {
			v = v[i+1 : i+j]
		}
	}
	v, _, _ = strings.Cut(v, ";")
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "sip:")
	v = strings.TrimPrefix(v, "sips:")
	return strings.TrimSpace(v)
}

func attrKV(raw, key string) string {
	token := key + "="
	i := strings.Index(raw, token)
	for i >= 0 {
		if i == 0 || raw[i-1] == ' ' || raw[i-1] == '\n' || raw[i-1] == '\t' {
			rest := raw[i+len(token):]
			if strings.HasPrefix(rest, `"`) {
				s, err := strconv.QuotedPrefix(rest)
				if err == nil {
					v, qerr := strconv.Unquote(s)
					if qerr == nil {
						return v
					}
				}
			}
			if j := strings.IndexAny(rest, " \n\t"); j >= 0 {
				return rest[:j]
			}
			return rest
		}
		next := strings.Index(raw[i+1:], token)
		if next < 0 {
			return ""
		}
		i += 1 + next
	}
	return ""
}

func codecName(pt int) string {
	switch pt {
	case 0:
		return "PCMU"
	case 8:
		return "PCMA"
	case 9:
		return "G722"
	case 13:
		return "CN"
	case 18:
		return "G729"
	default:
		if pt <= 0 {
			return ""
		}
		return fmt.Sprintf("PT%d", pt)
	}
}

func appendCapped[T any](buf []T, v T, capn int) []T {
	if capn < 1 {
		return buf
	}
	if len(buf) == capn {
		copy(buf, buf[1:])
		buf[capn-1] = v
		return buf
	}
	return append(buf, v)
}
