package media

import (
	"container/heap"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

const (
	scaleTickInterval   = time.Millisecond
	scaleSendQueueDepth = 4096
	scaleMaxBatch       = 512
)

var scalePacketPool = sync.Pool{
	New: func() any {
		b := make([]byte, 2048)
		return &b
	},
}

// ScaleEngine sends many cleartext synthetic RTP streams from a central scheduler
// (no per-stream tickers or receive/RTCP loops). Intended for high-scale load tests.
type ScaleEngine struct {
	mu      sync.Mutex
	streams map[uint64]*scaleStream
	byCall  map[string][]uint64
	heap    scaleHeap
	nextID  uint64

	cancel context.CancelFunc
	wg     sync.WaitGroup

	sendCh chan scaleSendJob

	packetsSent atomic.Uint64
	octetsSent  atomic.Uint64
}

type scaleStream struct {
	id       uint64
	callID   string
	conn     *net.UDPConn
	remote   *net.UDPAddr
	cfg      StreamConfig
	packet   []byte
	sendBuf  []byte
	sequence uint16
	timestamp uint32
	interval time.Duration
	nextSend time.Time
	paused   bool
	heapIdx  int

	packetsSent uint64
}

type scaleSendJob struct {
	conn *net.UDPConn
	msgs []udpSendMsg
}

// NewScaleEngine constructs a scale engine; call Run before registering streams.
func NewScaleEngine() *ScaleEngine {
	return &ScaleEngine{
		streams: make(map[uint64]*scaleStream),
		byCall:  make(map[string][]uint64),
		sendCh:  make(chan scaleSendJob, scaleSendQueueDepth),
	}
}

// Run starts scheduler and sender workers until ctx is cancelled.
func (e *ScaleEngine) Run(ctx context.Context) {
	if e.cancel != nil {
		return
	}
	child, cancel := context.WithCancel(ctx)
	e.cancel = cancel

	if !ScaleDirectSend() {
		workers := runtime.GOMAXPROCS(0)
		if workers < 1 {
			workers = 1
		}
		if workers > 8 {
			workers = 8
		}
		for i := 0; i < workers; i++ {
			e.wg.Add(1)
			go func() {
				defer e.wg.Done()
				e.senderLoop(child)
			}()
		}
	}
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		e.schedulerLoop(child)
	}()
}

// Stop cancels workers and closes all stream sockets.
func (e *ScaleEngine) Stop() {
	if e.cancel != nil {
		e.cancel()
	}
	e.wg.Wait()
	e.mu.Lock()
	defer e.mu.Unlock()
	for id, st := range e.streams {
		forgetBatchWriter(st.conn)
		_ = st.conn.Close()
		delete(e.streams, id)
	}
	e.byCall = make(map[string][]uint64)
	e.heap = nil
}

// RegisterStream starts a cleartext synthetic RTP stream (scale mode only).
func (e *ScaleEngine) RegisterStream(_ context.Context, callID string, endpoint Endpoint, cfg StreamConfig, localIP string, localPort int) error {
	if endpoint.IP == "" || endpoint.Port <= 0 {
		return fmt.Errorf("invalid RTP endpoint %s:%d", endpoint.IP, endpoint.Port)
	}
	if !cfg.Synthetic {
		return errors.New("scale engine supports synthetic cleartext RTP only")
	}
	remoteIP := net.ParseIP(endpoint.IP)
	if remoteIP == nil {
		return fmt.Errorf("invalid remote IP %q", endpoint.IP)
	}
	remote := &net.UDPAddr{IP: remoteIP, Port: endpoint.Port}

	conn, err := openScaleUDP(localIP, localPort)
	if err != nil {
		return err
	}
	if cfg.PacketDuration <= 0 {
		cfg.PacketDuration = 20 * time.Millisecond
	}
	if cfg.SamplesPerPkt == 0 {
		cfg.SamplesPerPkt = 160
	}
	payload := buildSyntheticPayload(cfg)
	pkt, err := BuildPacket(StreamConfig{
		PayloadType: cfg.PayloadType,
		SSRC:        cfg.SSRC,
		Sequence:    cfg.Sequence,
		Timestamp:   cfg.Timestamp,
	}, payload)
	if err != nil {
		_ = conn.Close()
		return err
	}
	if len(pkt) < 12 {
		_ = conn.Close()
		return errors.New("RTP packet too short")
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cancel == nil {
		_ = conn.Close()
		return errors.New("scale engine not running")
	}
	e.nextID++
	id := e.nextID
	now := time.Now()
	sendBuf := make([]byte, len(pkt))
	copy(sendBuf, pkt)
	st := &scaleStream{
		id:        id,
		callID:    callID,
		conn:      conn,
		remote:    remote,
		cfg:       cfg,
		packet:    pkt,
		sendBuf:   sendBuf,
		sequence:  cfg.Sequence,
		timestamp: cfg.Timestamp,
		interval:  cfg.PacketDuration,
		nextSend:  now,
		heapIdx:   -1,
	}
	e.streams[id] = st
	e.byCall[callID] = append(e.byCall[callID], id)
	heap.Push(&e.heap, st)
	return nil
}

// UnregisterCall stops and removes all streams for a call ID.
func (e *ScaleEngine) UnregisterCall(callID string) Stats {
	e.mu.Lock()
	ids := e.byCall[callID]
	delete(e.byCall, callID)
	var total Stats
	for _, id := range ids {
		st, ok := e.streams[id]
		if !ok {
			continue
		}
		if st.heapIdx >= 0 {
			heap.Remove(&e.heap, st.heapIdx)
		}
		delete(e.streams, id)
		forgetBatchWriter(st.conn)
		_ = st.conn.Close()
		total.RTPPacketsSent += uint32(st.packetsSent)
		total.RTPOctetsSent += uint32(st.packetsSent) * st.cfg.SamplesPerPkt
	}
	e.mu.Unlock()
	return total
}

// PauseCall pauses all streams for a call.
func (e *ScaleEngine) PauseCall(callID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, id := range e.byCall[callID] {
		if st, ok := e.streams[id]; ok {
			st.paused = true
		}
	}
}

// ResumeCall resumes all streams for a call.
func (e *ScaleEngine) ResumeCall(callID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := time.Now()
	for _, id := range e.byCall[callID] {
		if st, ok := e.streams[id]; ok {
			st.paused = false
			st.nextSend = now
			if st.heapIdx >= 0 {
				heap.Fix(&e.heap, st.heapIdx)
			}
		}
	}
}

// Snapshot returns aggregate send counters across all streams.
func (e *ScaleEngine) Snapshot() Stats {
	return Stats{
		RTPPacketsSent: uint32(e.packetsSent.Load()),
		RTPOctetsSent:  uint32(e.octetsSent.Load()),
	}
}

// StreamCount returns the number of active scale streams.
func (e *ScaleEngine) StreamCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.streams)
}

func (e *ScaleEngine) schedulerLoop(ctx context.Context) {
	ticker := time.NewTicker(scaleTickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.tick(time.Now())
		}
	}
}

func (e *ScaleEngine) tick(now time.Time) {
	e.mu.Lock()
	var due []*scaleStream
	for e.heap.Len() > 0 {
		st := e.heap[0]
		if st.nextSend.After(now) {
			break
		}
		heap.Pop(&e.heap)
		if !st.paused {
			due = append(due, st)
		}
		st.nextSend = st.nextSend.Add(st.interval)
		heap.Push(&e.heap, st)
	}
	e.mu.Unlock()
	if len(due) == 0 {
		return
	}

	byConn := make(map[*net.UDPConn][]udpSendMsg)
	for _, st := range due {
		patchRTPPacket(st.sendBuf, st.sequence, st.timestamp)
		if ScaleDirectSend() {
			byConn[st.conn] = append(byConn[st.conn], udpSendMsg{Addr: st.remote, Buf: st.sendBuf})
		} else {
			buf, bp := allocScalePacket(st.sendBuf)
			byConn[st.conn] = append(byConn[st.conn], udpSendMsg{Addr: st.remote, Buf: buf, pool: bp})
		}
		st.sequence++
		st.timestamp += st.cfg.SamplesPerPkt
		st.packetsSent++
	}

	for conn, msgs := range byConn {
		for off := 0; off < len(msgs); off += scaleMaxBatch {
			end := off + scaleMaxBatch
			if end > len(msgs) {
				end = len(msgs)
			}
			batch := msgs[off:end]
			if ScaleDirectSend() {
				n, _ := udpSendBatch(conn, batch)
				e.addSent(n, batch)
				continue
			}
			select {
			case e.sendCh <- scaleSendJob{conn: conn, msgs: batch}:
			default:
				n, _ := udpSendBatch(conn, batch)
				e.addSent(n, batch)
				releaseScaleMsgs(batch)
			}
		}
	}
}

func (e *ScaleEngine) senderLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-e.sendCh:
			n, err := udpSendBatch(job.conn, job.msgs)
			releaseScaleMsgs(job.msgs)
			if err != nil {
				continue
			}
			e.addSent(n, job.msgs[:n])
		}
	}
}

func allocScalePacket(template []byte) ([]byte, *[]byte) {
	bp := scalePacketPool.Get().(*[]byte)
	buf := (*bp)[:len(template):cap(*bp)]
	copy(buf, template)
	return buf, bp
}

func releaseScaleMsgs(msgs []udpSendMsg) {
	for _, m := range msgs {
		if m.pool == nil {
			continue
		}
		*m.pool = (*m.pool)[:cap(*m.pool)]
		scalePacketPool.Put(m.pool)
	}
}

func (e *ScaleEngine) addSent(n int, msgs []udpSendMsg) {
	if n <= 0 {
		return
	}
	var octets uint64
	for i := 0; i < n; i++ {
		if len(msgs[i].Buf) > 12 {
			octets += uint64(len(msgs[i].Buf) - 12)
		}
	}
	e.packetsSent.Add(uint64(n))
	e.octetsSent.Add(octets)
}

func patchRTPPacket(pkt []byte, seq uint16, ts uint32) {
	if len(pkt) < 8 {
		return
	}
	binary.BigEndian.PutUint16(pkt[2:4], seq)
	binary.BigEndian.PutUint32(pkt[4:8], ts)
}

type scaleHeap []*scaleStream

func (h scaleHeap) Len() int           { return len(h) }
func (h scaleHeap) Less(i, j int) bool { return h[i].nextSend.Before(h[j].nextSend) }
func (h scaleHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].heapIdx = i
	h[j].heapIdx = j
}

func (h *scaleHeap) Push(x any) {
	st := x.(*scaleStream)
	st.heapIdx = len(*h)
	*h = append(*h, st)
}

func (h *scaleHeap) Pop() any {
	old := *h
	n := len(old)
	st := old[n-1]
	old[n-1] = nil
	st.heapIdx = -1
	*h = old[0 : n-1]
	return st
}
