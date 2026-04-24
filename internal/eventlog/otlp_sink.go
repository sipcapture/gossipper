package eventlog

import (
	"context"
	"fmt"
	"sync"
	"time"

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
		logger:   provider.Logger("github.com/qxip/gossipper"),
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
		rec.SetBody(otelapi.StringValue(ev.Msg))
		if kind := ev.Kind; kind != "" {
			rec.AddAttributes(otelapi.String("gossipper.kind", kind))
		}
		for k, v := range ev.Attrs {
			rec.AddAttributes(toOTLPKV(k, v))
		}
		s.logger.Emit(context.Background(), rec)
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

func toOTLPKV(k string, v any) otelapi.KeyValue {
	switch val := v.(type) {
	case nil:
		return otelapi.String(k, "")
	case string:
		return otelapi.String(k, val)
	case bool:
		return otelapi.Bool(k, val)
	case int:
		return otelapi.Int64(k, int64(val))
	case int32:
		return otelapi.Int64(k, int64(val))
	case int64:
		return otelapi.Int64(k, val)
	case uint:
		return otelapi.Int64(k, int64(val))
	case uint32:
		return otelapi.Int64(k, int64(val))
	case uint64:
		return otelapi.Int64(k, int64(val))
	case float32:
		return otelapi.Float64(k, float64(val))
	case float64:
		return otelapi.Float64(k, val)
	case time.Duration:
		return otelapi.String(k, val.String())
	case time.Time:
		return otelapi.String(k, val.Format(time.RFC3339Nano))
	case error:
		return otelapi.String(k, val.Error())
	default:
		return otelapi.String(k, fmt.Sprintf("%v", val))
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
