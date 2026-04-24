// Package eventlog provides a non-blocking structured event logger for Gossipper.
//
// The logger feeds a fixed-size ring buffer that is drained by a single
// background goroutine and fanned out to one or more sinks (stdout, JSONL
// file, OTLP). When the buffer overflows the oldest event is overwritten and
// a drop counter is incremented, so the SIP scenario engine never blocks on
// log emission.
package eventlog

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

// Level is a coarse severity for filtering.
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

// String returns the canonical lowercase representation of the level.
func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "debug"
	case LevelWarn:
		return "warn"
	case LevelError:
		return "error"
	default:
		return "info"
	}
}

// ParseLevel parses a level name (case-insensitive). Unknown names map to
// LevelInfo and the second return value is false.
func ParseLevel(name string) (Level, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "info":
		return LevelInfo, true
	case "debug":
		return LevelDebug, true
	case "warn", "warning":
		return LevelWarn, true
	case "error", "err":
		return LevelError, true
	default:
		return LevelInfo, false
	}
}

// Kind names well-known event categories. Sinks may surface them as
// otel attribute "gossipper.kind" or as a column in text output.
const (
	KindSIPSend     = "sip.send"
	KindSIPRecv     = "sip.recv"
	KindCallStart   = "call.started"
	KindCallEnd     = "call.ended"
	KindAuth        = "auth"
	KindUnexpected  = "sip.unexpected"
	KindTimeout     = "timeout"
	KindActionLog   = "action.log"
	KindError       = "error"
	KindEngineStart = "engine.started"
	KindEngineStop  = "engine.stopped"
)

// Event is a single structured record emitted by the engine.
//
// Attrs is intentionally `map[string]any` (not a typed builder) to keep the
// hot path allocation-free for the most common case: a literal map composed
// at the call site.
type Event struct {
	Time  time.Time
	Level Level
	Kind  string
	Msg   string
	Attrs map[string]any
}

// MergeAttrs returns a new map containing all keys from base and extra.
// Keys in extra override keys in base. nil inputs are tolerated.
func MergeAttrs(base, extra map[string]any) map[string]any {
	if len(base) == 0 && len(extra) == 0 {
		return nil
	}
	out := make(map[string]any, len(base)+len(extra))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

// FormatAttrs renders attributes in deterministic key=value form sorted by
// key. Used by stdout sink and tests.
func FormatAttrs(attrs map[string]any) string {
	if len(attrs) == 0 {
		return ""
	}
	keys := make([]string, 0, len(attrs))
	for k := range attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(formatValue(attrs[k]))
	}
	return b.String()
}

func formatValue(v any) string {
	switch val := v.(type) {
	case nil:
		return ""
	case string:
		if strings.ContainsAny(val, " \t\"") {
			return strconv.Quote(val)
		}
		return val
	case bool:
		if val {
			return "true"
		}
		return "false"
	case int:
		return strconv.Itoa(val)
	case int32:
		return strconv.FormatInt(int64(val), 10)
	case int64:
		return strconv.FormatInt(val, 10)
	case uint:
		return strconv.FormatUint(uint64(val), 10)
	case uint32:
		return strconv.FormatUint(uint64(val), 10)
	case uint64:
		return strconv.FormatUint(val, 10)
	case float32:
		return strconv.FormatFloat(float64(val), 'f', -1, 32)
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	case time.Duration:
		return val.String()
	case time.Time:
		return val.Format(time.RFC3339Nano)
	case error:
		return strconv.Quote(val.Error())
	default:
		return strconv.Quote(toString(val))
	}
}

func toString(v any) string {
	type stringer interface{ String() string }
	if s, ok := v.(stringer); ok {
		return s.String()
	}
	return ""
}
