package launcher

import (
	"errors"
	"fmt"

	"github.com/qxip/gossipper/internal/cli"
	"github.com/qxip/gossipper/internal/eventlog"
	"github.com/qxip/gossipper/internal/scenario"
)

// BuildEventLogger constructs an eventlog.Logger from cli flags.
//
// The returned closer closes both the logger and any sinks. When no logging
// destination is configured the function returns eventlog.Noop() and a
// no-op closer so the engine can always dereference Logger without nil
// checks.
func BuildEventLogger(cfg cli.Config, sc scenario.Scenario) (eventlog.Logger, func() error, error) {
	resource := buildResourceAttrs(cfg, sc)

	var sinks []eventlog.Sink
	if cfg.LogStdout {
		sinks = append(sinks, eventlog.NewStdoutSink(resource))
	}
	if cfg.LogFileJSONL != "" {
		sink, err := eventlog.NewFileSink(cfg.LogFileJSONL, resource)
		if err != nil {
			return nil, noopCloser, fmt.Errorf("event logger: %w", err)
		}
		sinks = append(sinks, sink)
	}
	if cfg.LogOTELEndpoint != "" {
		sink, err := newOTLPSink(cfg, resource)
		if err != nil {
			closeSinks(sinks)
			return nil, noopCloser, fmt.Errorf("event logger: %w", err)
		}
		sinks = append(sinks, sink)
	}

	if len(sinks) == 0 {
		return eventlog.Noop(), noopCloser, nil
	}

	level, _ := eventlog.ParseLevel(cfg.LogLevel)
	logger := eventlog.New(eventlog.Config{
		BufferSize: cfg.LogBufferSize,
		MinLevel:   level,
		Sinks:      sinks,
	})
	return logger, logger.Close, nil
}

func buildResourceAttrs(cfg cli.Config, sc scenario.Scenario) map[string]any {
	attrs := map[string]any{
		"service.name":    "gossipper",
		"gossipper.role":  string(sc.Mode),
	}
	for k, v := range cfg.LogAttrs {
		attrs[k] = v
	}
	return attrs
}

func closeSinks(sinks []eventlog.Sink) {
	for _, sink := range sinks {
		_ = sink.Flush()
		_ = sink.Close()
	}
}

func noopCloser() error { return nil }

// roleFromScenario maps scenario.Mode to a string for engine.Config.Role.
func roleFromScenario(sc scenario.Scenario) string {
	switch sc.Mode {
	case scenario.ModeServer:
		return "server"
	case scenario.ModeClient:
		return "client"
	default:
		return ""
	}
}

// guard against unused import of errors when otlp sink stub is empty.
var _ = errors.New
