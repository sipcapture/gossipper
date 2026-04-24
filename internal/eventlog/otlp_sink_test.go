package eventlog

import (
	"context"
	"sync"
	"testing"
	"time"

	otelapi "go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
)

// inMemoryExporter is a sdklog.Exporter that captures records for assertions.
type inMemoryExporter struct {
	mu      sync.Mutex
	records []sdklog.Record
}

func (e *inMemoryExporter) Export(_ context.Context, records []sdklog.Record) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := range records {
		e.records = append(e.records, records[i].Clone())
	}
	return nil
}

func (e *inMemoryExporter) ForceFlush(context.Context) error { return nil }
func (e *inMemoryExporter) Shutdown(context.Context) error   { return nil }

func (e *inMemoryExporter) Records() []sdklog.Record {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]sdklog.Record, len(e.records))
	copy(out, e.records)
	return out
}

func TestOTLPSinkExportsResourceAndAttributes(t *testing.T) {
	exp := &inMemoryExporter{}
	resource := map[string]any{
		"service.name":   "gossipper",
		"gossipper.role": "client",
		"self_tag":       "NYC02",
		"peer_tag":       "NYC01",
	}
	sink, err := NewOTLPSink(context.Background(), func(context.Context) (sdklog.Exporter, error) {
		return exp, nil
	}, resource)
	if err != nil {
		t.Fatalf("NewOTLPSink: %v", err)
	}
	if err := sink.Write([]Event{{
		Time:  time.Now(),
		Level: LevelInfo,
		Kind:  KindSIPSend,
		Msg:   "INVITE",
		Attrs: map[string]any{
			"call_id":    "abc",
			"sip.method": "INVITE",
			"src_port":   5060,
		},
	}}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := sink.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	records := exp.Records()
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	rec := records[0]

	if rec.EventName() != KindSIPSend {
		t.Fatalf("expected event name %q, got %q", KindSIPSend, rec.EventName())
	}
	if rec.Body().AsString() != "INVITE" {
		t.Fatalf("expected body INVITE, got %q", rec.Body().AsString())
	}
	if rec.Severity() != otelapi.SeverityInfo {
		t.Fatalf("expected severity Info, got %v", rec.Severity())
	}

	attrs := collectAttrs(rec)
	for _, want := range []string{"call_id", "sip.method", "src_port", "gossipper.kind"} {
		if _, ok := attrs[want]; !ok {
			t.Fatalf("expected record attr %q, got keys %v", want, keys(attrs))
		}
	}
	if attrs["call_id"].AsString() != "abc" {
		t.Fatalf("expected call_id=abc, got %q", attrs["call_id"].AsString())
	}
	if attrs["src_port"].AsInt64() != 5060 {
		t.Fatalf("expected src_port=5060, got %d", attrs["src_port"].AsInt64())
	}

	// Resource attributes should reach the exporter via record.Resource().
	res := rec.Resource()
	if res == nil {
		t.Fatalf("expected non-nil resource on record")
	}
	got := map[string]string{}
	for _, kv := range res.Attributes() {
		got[string(kv.Key)] = kv.Value.Emit()
	}
	for k, v := range resource {
		want, _ := v.(string)
		if got[k] != want {
			t.Fatalf("resource attr %s: expected %q, got %q (full: %+v)", k, want, got[k], got)
		}
	}
}

func TestOTLPSinkRejectsNilProvider(t *testing.T) {
	_, err := NewOTLPSink(context.Background(), nil, nil)
	if err == nil {
		t.Fatal("expected NewOTLPSink to reject nil provider")
	}
}

func TestOTLPSinkConvertsLevels(t *testing.T) {
	cases := map[Level]otelapi.Severity{
		LevelDebug: otelapi.SeverityDebug,
		LevelInfo:  otelapi.SeverityInfo,
		LevelWarn:  otelapi.SeverityWarn,
		LevelError: otelapi.SeverityError,
	}
	for level, want := range cases {
		if got := severityToOTLP(level); got != want {
			t.Fatalf("severityToOTLP(%v)=%v, want %v", level, got, want)
		}
	}
}

func collectAttrs(rec sdklog.Record) map[string]otelapi.Value {
	out := map[string]otelapi.Value{}
	rec.WalkAttributes(func(kv otelapi.KeyValue) bool {
		out[kv.Key] = kv.Value
		return true
	})
	return out
}

func keys(m map[string]otelapi.Value) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
