package eventlog

import (
	"bytes"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNoopLoggerIsNeverNil(t *testing.T) {
	l := Noop()
	l.Emit(Event{Msg: "x"})
	if got := l.Dropped(); got != 0 {
		t.Fatalf("noop logger should report zero drops, got %d", got)
	}
	if err := l.Close(); err != nil {
		t.Fatalf("noop close: %v", err)
	}
	if l.With(map[string]any{"a": "b"}) == nil {
		t.Fatalf("noop With should never return nil")
	}
}

func TestLoggerFanOutToMultipleSinks(t *testing.T) {
	a := NewMemorySink()
	b := NewMemorySink()
	l := New(Config{Sinks: []Sink{a, b}, BufferSize: 8})

	l.Emit(Event{Kind: KindSIPSend, Msg: "INVITE"})
	l.Emit(Event{Kind: KindSIPRecv, Msg: "200 OK"})
	if err := l.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	for _, sink := range []Sink{a, b} {
		events := MemorySinkEvents(sink)
		if len(events) != 2 {
			t.Fatalf("sink expected 2 events, got %d", len(events))
		}
		if events[0].Kind != KindSIPSend || events[1].Kind != KindSIPRecv {
			t.Fatalf("unexpected kinds: %+v", events)
		}
	}
}

func TestLoggerWithAttrsMerged(t *testing.T) {
	sink := NewMemorySink()
	base := New(Config{Sinks: []Sink{sink}, BufferSize: 4})
	bound := base.With(map[string]any{"role": "client", "self_tag": "NYC02"})
	bound.Emit(Event{Kind: KindSIPSend, Msg: "INVITE", Attrs: map[string]any{"call_id": "c1"}})
	if err := base.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	events := MemorySinkEvents(sink)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	got := events[0].Attrs
	for _, key := range []string{"role", "self_tag", "call_id"} {
		if _, ok := got[key]; !ok {
			t.Fatalf("expected attr %q in event, got %#v", key, got)
		}
	}
}

func TestLoggerLevelFilter(t *testing.T) {
	sink := NewMemorySink()
	l := New(Config{Sinks: []Sink{sink}, BufferSize: 4, MinLevel: LevelWarn})
	l.Emit(Event{Level: LevelDebug, Msg: "skip-debug"})
	l.Emit(Event{Level: LevelInfo, Msg: "skip-info"})
	l.Emit(Event{Level: LevelWarn, Msg: "keep-warn"})
	l.Emit(Event{Level: LevelError, Msg: "keep-error"})
	_ = l.Close()
	events := MemorySinkEvents(sink)
	if len(events) != 2 {
		t.Fatalf("expected 2 events past filter, got %d (%+v)", len(events), events)
	}
	if events[0].Msg != "keep-warn" || events[1].Msg != "keep-error" {
		t.Fatalf("unexpected events through filter: %+v", events)
	}
}

func TestLoggerDropCounterUnderPressure(t *testing.T) {
	slow := &slowSink{ready: make(chan struct{})}
	l := New(Config{Sinks: []Sink{slow}, BufferSize: 4, BatchSize: 4})

	const total = 1000
	for i := 0; i < total; i++ {
		l.Emit(Event{Msg: idMsg(i)})
	}
	close(slow.ready)
	if err := l.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	dropped := int(l.Dropped())
	received := slow.Received()
	if dropped == 0 {
		t.Fatalf("expected drops under pressure, got 0")
	}
	if received == 0 {
		t.Fatalf("expected sink to receive at least some events, got 0")
	}
	if received+dropped != total {
		t.Fatalf("expected received(%d)+dropped(%d)=%d, got %d", received, dropped, total, received+dropped)
	}
}

func TestLoggerEmitAfterCloseIsSafe(t *testing.T) {
	sink := NewMemorySink()
	l := New(Config{Sinks: []Sink{sink}, BufferSize: 4})
	if err := l.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	l.Emit(Event{Msg: "after"})
	if got := len(MemorySinkEvents(sink)); got != 0 {
		t.Fatalf("expected zero events after close, got %d", got)
	}
}

func TestLoggerConcurrentEmit(t *testing.T) {
	sink := NewMemorySink()
	l := New(Config{Sinks: []Sink{sink}, BufferSize: 4096, BatchSize: 64})

	const writers = 10
	const perWriter = 100
	var wg sync.WaitGroup
	wg.Add(writers)
	for w := 0; w < writers; w++ {
		go func() {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				l.Emit(Event{Kind: KindSIPSend, Msg: "x"})
			}
		}()
	}
	wg.Wait()
	if err := l.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if got := len(MemorySinkEvents(sink)); got != writers*perWriter {
		t.Fatalf("expected %d events, got %d (drops=%d)", writers*perWriter, got, l.Dropped())
	}
}

func TestWriterSinkRendersResource(t *testing.T) {
	var buf threadSafeBuffer
	sink := NewWriterSink(&buf, map[string]any{"role": "client", "self_tag": "NYC02"})
	if err := sink.Write([]Event{{Time: time.Unix(0, 0), Level: LevelInfo, Kind: KindSIPSend, Msg: "INVITE", Attrs: map[string]any{"call_id": "c1"}}}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := sink.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	got := buf.String()
	for _, want := range []string{"sip.send", "INVITE", "role=client", "self_tag=NYC02", "call_id=c1"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected output to contain %q, got %q", want, got)
		}
	}
}

func TestWriterSinkEventOverridesResource(t *testing.T) {
	var buf threadSafeBuffer
	sink := NewWriterSink(&buf, map[string]any{"role": "client"})
	if err := sink.Write([]Event{{Kind: KindSIPRecv, Attrs: map[string]any{"role": "server"}}}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := sink.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if !strings.Contains(buf.String(), "role=server") {
		t.Fatalf("expected event-level role to win, got %q", buf.String())
	}
	if strings.Contains(buf.String(), "role=client") {
		t.Fatalf("did not expect resource-level role to leak through, got %q", buf.String())
	}
}

// slowSink blocks Write until ready is closed; used to deliberately stall
// the drain goroutine so producers keep overwriting the ring buffer.
type slowSink struct {
	ready    chan struct{}
	received atomic.Int64
}

func (s *slowSink) Write(events []Event) error {
	<-s.ready
	s.received.Add(int64(len(events)))
	return nil
}

func (s *slowSink) Flush() error { return nil }
func (s *slowSink) Close() error { return nil }

func (s *slowSink) Received() int { return int(s.received.Load()) }

// threadSafeBuffer wraps bytes.Buffer with a mutex for safe writes from
// the drain goroutine while the test reads.
type threadSafeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *threadSafeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *threadSafeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
