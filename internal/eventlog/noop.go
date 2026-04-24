package eventlog

// noopLogger discards every event. It is returned from Noop() and from New
// when the caller passes no sinks.
type noopLogger struct{}

// Noop returns a Logger that discards every event with zero overhead. The
// engine uses this when no logging is configured so callers can always
// dereference Logger without a nil check.
func Noop() Logger { return noopLogger{} }

func (noopLogger) Emit(Event)                     {}
func (noopLogger) With(map[string]any) Logger     { return noopLogger{} }
func (noopLogger) Close() error                   { return nil }
func (noopLogger) Dropped() uint64                { return 0 }
