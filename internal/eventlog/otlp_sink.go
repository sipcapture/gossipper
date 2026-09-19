package eventlog

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	otelapi "go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
)

// OTLPProvider knows how to construct an OTel SDK Exporter.
//
// Real implementations live in otlp_grpc_exporter.go / otlp_http_exporter.go;
// tests can plug in an in-memory exporter without depending on the gRPC stack.
type OTLPProvider func(ctx context.Context) (sdklog.Exporter, error)

// otlpSink implements Sink on top of the OTel SDK BatchProcessor.
//
// The resource map is forwarded to the SDK as an *resource.Resource so the
// collector receives standard resource attributes (service.name, gossipper.role,
// user-defined -log_attr ...) on every record, instead of duplicating them
// inside the per-record attributes.
type otlpSink struct {
	provider *sdklog.LoggerProvider
	logger   otelapi.Logger
	mu       sync.Mutex
	closed   bool
}

// NewOTLPSink wires an OTel BatchProcessor with the given exporter and
// resource attributes.
func NewOTLPSink(ctx context.Context, build OTLPProvider, resourceAttrs map[string]any) (Sink, error) {
	if build == nil {
		return nil, fmt.Errorf("eventlog: nil OTLPProvider")
	}
	exporter, err := build(ctx)
	if err != nil {
		return nil, err
	}
	res := buildResource(resourceAttrs)
	provider := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter)),
		sdklog.WithResource(res),
	)
	return &otlpSink{
		provider: provider,
		logger:   provider.Logger("github.com/sipcapture/gossipper"),
	}, nil
}

func (s *otlpSink) Write(events []Event) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()
	for _, ev := range events {
		var rec otelapi.Record
		t := ev.Time
		if t.IsZero() {
			t = time.Now()
		}
		rec.SetTimestamp(t)
		rec.SetObservedTimestamp(time.Now())
		rec.SetSeverity(severityToOTLP(ev.Level))
		rec.SetSeverityText(ev.Level.String())
		rec.SetEventName(ev.Kind)
		rec.SetBody(attribute.StringValue(ev.Msg))
		if kind := ev.Kind; kind != "" {
			rec.AddAttributes(attribute.String("gossipper.kind", kind))
		}
		for k, v := range ev.Attrs {
			rec.AddAttributes(anyToAttribute(k, v))
		}
		s.logger.Emit(emitContext(ev.Attrs), rec)
	}
	return nil
}

func (s *otlpSink) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.provider.ForceFlush(ctx)
}

func (s *otlpSink) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.provider.Shutdown(ctx)
}

func severityToOTLP(l Level) otelapi.Severity {
	switch l {
	case LevelDebug:
		return otelapi.SeverityDebug
	case LevelWarn:
		return otelapi.SeverityWarn
	case LevelError:
		return otelapi.SeverityError
	default:
		return otelapi.SeverityInfo
	}
}

func buildResource(attrs map[string]any) *resource.Resource {
	if len(attrs) == 0 {
		return resource.NewSchemaless()
	}
	kvs := make([]attributeKV, 0, len(attrs))
	for k, v := range attrs {
		kvs = append(kvs, attributeKV{Key: k, Value: v})
	}
	return resource.NewSchemaless(kvsToAttrs(kvs)...)
}
