package eventlog

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"sync"
	"time"
)

// Sink is one destination for drained events. Implementations must be
// safe for use from the single drain goroutine; they do not need to be
// safe for concurrent calls.
type Sink interface {
	Write([]Event) error
	Flush() error
	Close() error
}

// writerSink writes events to an io.Writer using a configurable formatter.
// Used by both StdoutSink and FileSink (JSONL) to share locking, buffering
// and Close semantics.
type writerSink struct {
	mu       sync.Mutex
	bw       *bufio.Writer
	closer   io.Closer
	format   func(Event) string
	resource map[string]any
}

// NewStdoutSink returns a sink that writes events to os.Stderr in a
// human-readable text format. Resource attributes are merged into each line
// for parity with the OTLP sink output.
func NewStdoutSink(resource map[string]any) Sink {
	return newWriterSink(os.Stderr, nil, formatText, resource)
}

// NewWriterSink writes events as text lines to an arbitrary writer.
// Used by tests and as a building block for stdout/stderr-style sinks.
func NewWriterSink(w io.Writer, resource map[string]any) Sink {
	return newWriterSink(w, nil, formatText, resource)
}

// NewFileSink opens path for writing and returns a sink that emits one
// JSON object per line. Files are truncated; use NewFileAppendSink to
// preserve previous content.
func NewFileSink(path string, resource map[string]any) (Sink, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("eventlog: open %s: %w", path, err)
	}
	return newWriterSink(f, f, formatJSON, resource), nil
}

func newWriterSink(w io.Writer, closer io.Closer, format func(Event) string, resource map[string]any) *writerSink {
	return &writerSink{
		bw:       bufio.NewWriterSize(w, 16*1024),
		closer:   closer,
		format:   format,
		resource: cloneAttrs(resource),
	}
}

func (s *writerSink) Write(events []Event) error {
	if len(events) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ev := range events {
		ev = withResource(ev, s.resource)
		if _, err := s.bw.WriteString(s.format(ev)); err != nil {
			return err
		}
		if err := s.bw.WriteByte('\n'); err != nil {
			return err
		}
	}
	return nil
}

func (s *writerSink) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bw.Flush()
}

func (s *writerSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	flushErr := s.bw.Flush()
	if s.closer != nil {
		closeErr := s.closer.Close()
		if flushErr == nil {
			return closeErr
		}
	}
	return flushErr
}

func formatText(ev Event) string {
	t := ev.Time
	if t.IsZero() {
		t = time.Now()
	}
	level := ev.Level.String()
	kind := ev.Kind
	if kind == "" {
		kind = "event"
	}
	out := fmt.Sprintf("%s %s %s", t.UTC().Format(time.RFC3339Nano), level, kind)
	if ev.Msg != "" {
		out += " " + quoteIfNeeded(ev.Msg)
	}
	if attrs := FormatAttrs(ev.Attrs); attrs != "" {
		out += " " + attrs
	}
	return out
}

func quoteIfNeeded(msg string) string {
	for _, r := range msg {
		if r == ' ' || r == '\t' || r == '"' || r == '\n' {
			return fmt.Sprintf("%q", msg)
		}
	}
	return msg
}

func formatJSON(ev Event) string {
	t := ev.Time
	if t.IsZero() {
		t = time.Now()
	}
	rec := struct {
		Time  string         `json:"time"`
		Level string         `json:"level"`
		Kind  string         `json:"kind"`
		Msg   string         `json:"msg,omitempty"`
		Attrs map[string]any `json:"attrs,omitempty"`
	}{
		Time:  t.UTC().Format(time.RFC3339Nano),
		Level: ev.Level.String(),
		Kind:  ev.Kind,
		Msg:   ev.Msg,
		Attrs: jsonAttrs(ev.Attrs),
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return fmt.Sprintf(`{"time":%q,"level":%q,"kind":%q,"msg":%q,"err":%q}`,
			t.UTC().Format(time.RFC3339Nano), ev.Level.String(), ev.Kind, ev.Msg, err.Error())
	}
	return string(data)
}

func jsonAttrs(attrs map[string]any) map[string]any {
	if len(attrs) == 0 {
		return nil
	}
	out := make(map[string]any, len(attrs))
	for k, v := range attrs {
		switch val := v.(type) {
		case time.Duration:
			out[k] = val.String()
		case time.Time:
			out[k] = val.Format(time.RFC3339Nano)
		case error:
			out[k] = val.Error()
		default:
			out[k] = val
		}
	}
	return out
}

func cloneAttrs(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// withResource returns ev with resource attributes merged into Attrs.
// Resource attributes do not overwrite event-level attributes.
func withResource(ev Event, resource map[string]any) Event {
	if len(resource) == 0 {
		return ev
	}
	merged := make(map[string]any, len(resource)+len(ev.Attrs))
	for k, v := range resource {
		merged[k] = v
	}
	for k, v := range ev.Attrs {
		merged[k] = v
	}
	ev.Attrs = merged
	return ev
}

// memorySink keeps events in memory; used by tests and engine_test.go.
type memorySink struct {
	mu     sync.Mutex
	events []Event
}

// NewMemorySink returns a sink that stores all received events. Useful
// for tests that need to assert on emitted events.
func NewMemorySink() Sink {
	return &memorySink{}
}

func (s *memorySink) Write(events []Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ev := range events {
		copyEv := ev
		copyEv.Attrs = cloneAttrs(ev.Attrs)
		s.events = append(s.events, copyEv)
	}
	return nil
}

func (s *memorySink) Flush() error { return nil }
func (s *memorySink) Close() error { return nil }

// Events returns a copy of the captured events.
func MemorySinkEvents(s Sink) []Event {
	ms, ok := s.(*memorySink)
	if !ok {
		return nil
	}
	ms.mu.Lock()
	defer ms.mu.Unlock()
	out := make([]Event, len(ms.events))
	copy(out, ms.events)
	return out
}

// MemorySinkKinds returns a sorted slice of event kinds captured by a
// memory sink. Convenience helper for tests.
func MemorySinkKinds(s Sink) []string {
	events := MemorySinkEvents(s)
	out := make([]string, 0, len(events))
	for _, ev := range events {
		out = append(out, ev.Kind)
	}
	sort.Strings(out)
	return out
}
