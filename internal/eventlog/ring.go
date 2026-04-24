package eventlog

import (
	"sync"
	"sync/atomic"
)

// ringBuffer is a fixed-capacity MPSC (multiple producers, single consumer)
// ring buffer that overwrites the oldest event when full. It is intentionally
// guarded by a mutex rather than using lock-free atomics: the contention is
// extremely low (one push per SIP message) and a mutex keeps the code obvious
// and easy to reason about.
//
// The drain side blocks on a sync.Cond rather than busy-spinning so the
// process stays cool when there is no traffic.
type ringBuffer struct {
	mu      sync.Mutex
	cond    *sync.Cond
	buf     []Event
	head    int    // next read position
	tail    int    // next write position
	size    int    // current number of items in buffer
	cap     int    // total capacity
	dropped uint64 // total events overwritten because the buffer was full
	closed  bool
}

func newRingBuffer(capacity int) *ringBuffer {
	if capacity <= 0 {
		capacity = 1
	}
	rb := &ringBuffer{
		buf: make([]Event, capacity),
		cap: capacity,
	}
	rb.cond = sync.NewCond(&rb.mu)
	return rb
}

// push enqueues an event. When the buffer is full, the oldest event is
// overwritten and the drop counter is incremented. push never blocks.
func (r *ringBuffer) push(ev Event) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	if r.size == r.cap {
		// Overwrite oldest by advancing head.
		r.head = (r.head + 1) % r.cap
		r.size--
		atomic.AddUint64(&r.dropped, 1)
	}
	r.buf[r.tail] = ev
	r.tail = (r.tail + 1) % r.cap
	r.size++
	r.cond.Signal()
	r.mu.Unlock()
}

// drain blocks until at least one event is available or the ring is closed,
// then copies up to max events into out and returns the slice with the
// drained items. The returned ok is false only when the buffer has been
// closed AND fully drained.
func (r *ringBuffer) drain(out []Event) ([]Event, bool) {
	r.mu.Lock()
	for r.size == 0 && !r.closed {
		r.cond.Wait()
	}
	if r.size == 0 && r.closed {
		r.mu.Unlock()
		return out[:0], false
	}
	out = out[:0]
	max := cap(out)
	if max <= 0 {
		max = r.size
	}
	n := r.size
	if n > max {
		n = max
	}
	for i := 0; i < n; i++ {
		out = append(out, r.buf[r.head])
		// zero slot to release any references held by Attrs
		r.buf[r.head] = Event{}
		r.head = (r.head + 1) % r.cap
	}
	r.size -= n
	r.mu.Unlock()
	return out, true
}

// close marks the ring as closed and wakes the consumer so it can drain any
// remaining items and observe the closed flag. Subsequent push calls become
// no-ops.
func (r *ringBuffer) close() {
	r.mu.Lock()
	r.closed = true
	r.cond.Broadcast()
	r.mu.Unlock()
}

// dropCount returns the cumulative number of events that were overwritten
// because the buffer was full when push() was called.
func (r *ringBuffer) dropCount() uint64 {
	return atomic.LoadUint64(&r.dropped)
}

// length returns the current number of buffered events. Used by tests.
func (r *ringBuffer) length() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.size
}
