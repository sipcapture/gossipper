package eventlog

import (
	"errors"
	"sync/atomic"
	"time"
)

// Logger is the public interface used by the engine to emit events.
//
// Emit must be non-blocking: when the underlying buffer is full the oldest
// event is overwritten and a drop counter is incremented.
type Logger interface {
	Emit(Event)
	With(attrs map[string]any) Logger
	Close() error
	Dropped() uint64
}

// Config controls how a multi-sink logger is constructed.
type Config struct {
	BufferSize int
	MinLevel   Level
	Sinks      []Sink
	BatchSize  int
}

const (
	defaultBufferSize = 16 * 1024
	defaultBatchSize  = 256
)

// New constructs a multi-sink logger. The returned logger owns the sinks
// and will Close them as part of its own Close method.
func New(cfg Config) Logger {
	if len(cfg.Sinks) == 0 {
		return Noop()
	}
	if cfg.BufferSize <= 0 {
		cfg.BufferSize = defaultBufferSize
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = defaultBatchSize
	}
	if cfg.BatchSize > cfg.BufferSize {
		cfg.BatchSize = cfg.BufferSize
	}
	l := &logger{
		ring:     newRingBuffer(cfg.BufferSize),
		sinks:    cfg.Sinks,
		minLevel: cfg.MinLevel,
		done:     make(chan struct{}),
		batch:    cfg.BatchSize,
	}
	go l.drainLoop()
	return l
}

type logger struct {
	ring     *ringBuffer
	sinks    []Sink
	minLevel Level
	done     chan struct{}
	batch    int
	closed   atomic.Bool
}

func (l *logger) Emit(ev Event) {
	if l.closed.Load() {
		return
	}
	if ev.Level < l.minLevel {
		return
	}
	if ev.Time.IsZero() {
		ev.Time = time.Now()
	}
	l.ring.push(ev)
}

func (l *logger) With(attrs map[string]any) Logger {
	if len(attrs) == 0 {
		return l
	}
	return &boundLogger{base: l, attrs: cloneAttrs(attrs)}
}

func (l *logger) Close() error {
	if !l.closed.CompareAndSwap(false, true) {
		return nil
	}
	l.ring.close()
	<-l.done
	var errs []error
	for _, sink := range l.sinks {
		if err := sink.Flush(); err != nil {
			errs = append(errs, err)
		}
		if err := sink.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (l *logger) Dropped() uint64 {
	return l.ring.dropCount()
}

func (l *logger) drainLoop() {
	defer close(l.done)
	buf := make([]Event, 0, l.batch)
	for {
		var ok bool
		buf, ok = l.ring.drain(buf[:0:l.batch])
		if len(buf) > 0 {
			for _, sink := range l.sinks {
				_ = sink.Write(buf)
			}
		}
		if !ok {
			for _, sink := range l.sinks {
				_ = sink.Flush()
			}
			return
		}
	}
}

// boundLogger pre-binds a set of attributes that are merged into every
// Emit call. The base logger is shared so Close still operates on the
// single underlying ring buffer.
type boundLogger struct {
	base  Logger
	attrs map[string]any
}

func (b *boundLogger) Emit(ev Event) {
	if len(b.attrs) > 0 {
		ev.Attrs = MergeAttrs(b.attrs, ev.Attrs)
	}
	b.base.Emit(ev)
}

func (b *boundLogger) With(attrs map[string]any) Logger {
	if len(attrs) == 0 {
		return b
	}
	merged := MergeAttrs(b.attrs, attrs)
	return &boundLogger{base: b.base, attrs: merged}
}

func (b *boundLogger) Close() error    { return b.base.Close() }
func (b *boundLogger) Dropped() uint64 { return b.base.Dropped() }
