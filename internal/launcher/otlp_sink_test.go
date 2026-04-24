package launcher

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/qxip/gossipper/internal/cli"
	"github.com/qxip/gossipper/internal/eventlog"
)

// TestBuildOTLPSinkSendsHTTPRequest spins up an httptest server that
// pretends to be an OTLP/HTTP collector and verifies that the launcher
// correctly wires the otlploghttp exporter through eventlog.NewOTLPSink.
//
// We do not parse the body (that would require linking the OTLP protobuf
// schema in the test); receiving any request on /v1/logs is sufficient
// proof that the sink is wired and exporting.
func TestBuildOTLPSinkSendsHTTPRequest(t *testing.T) {
	t.Parallel()

	var (
		mu      sync.Mutex
		called  = false
		bodyLen int
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		called = true
		bodyLen = len(body)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := cli.Config{
		LogOTELEndpoint: srv.URL,
		LogOTELProto:    "http",
		LogOTELInsecure: true,
	}
	sink, err := buildOTLPSink(cfg, map[string]any{
		"service.name":   "gossipper-test",
		"gossipper.role": "client",
	})
	if err != nil {
		t.Fatalf("buildOTLPSink: %v", err)
	}

	if err := sink.Write([]eventlog.Event{
		{
			Time:  time.Now(),
			Level: eventlog.LevelInfo,
			Kind:  eventlog.KindSIPSend,
			Msg:   "INVITE",
			Attrs: map[string]any{"call_id": "abc"},
		},
	}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	flushCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = flushCtx
	if err := sink.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		ok := called
		mu.Unlock()
		if ok {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := sink.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if !called {
		t.Fatalf("expected OTLP HTTP collector to receive at least one request")
	}
	if bodyLen == 0 {
		t.Fatalf("expected non-empty OTLP HTTP body")
	}
}

func TestParseHTTPEndpoint(t *testing.T) {
	t.Parallel()
	cases := []struct {
		raw          string
		insecureFlag bool
		wantHost     string
		wantPath     string
		wantInsecure bool
		wantErr      bool
	}{
		{"otel:4318", false, "otel:4318", "", false, false},
		{"otel:4318", true, "otel:4318", "", true, false},
		{"http://otel:4318", false, "otel:4318", "", true, false},
		{"https://otel:4318/custom", false, "otel:4318", "/custom", false, false},
		{"https://otel:4318/custom", true, "otel:4318", "/custom", true, false},
		{"ftp://otel:4318", false, "", "", false, true},
	}
	for _, tc := range cases {
		host, path, insecure, err := parseHTTPEndpoint(tc.raw, tc.insecureFlag)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("parseHTTPEndpoint(%q) expected error", tc.raw)
			}
			continue
		}
		if err != nil {
			t.Fatalf("parseHTTPEndpoint(%q) unexpected error: %v", tc.raw, err)
		}
		if host != tc.wantHost || path != tc.wantPath || insecure != tc.wantInsecure {
			t.Fatalf("parseHTTPEndpoint(%q,%v) = (%q,%q,%v); want (%q,%q,%v)",
				tc.raw, tc.insecureFlag, host, path, insecure, tc.wantHost, tc.wantPath, tc.wantInsecure)
		}
	}
}

func TestOTLPExporterProviderRejectsBadProto(t *testing.T) {
	t.Parallel()
	if _, err := otlpExporterProvider(cli.Config{LogOTELProto: "thrift"}); err == nil {
		t.Fatal("expected error for unknown proto")
	}
	if _, err := otlpExporterProvider(cli.Config{LogOTELProto: "grpc"}); err == nil {
		t.Fatal("expected error for empty endpoint")
	}
	if _, err := otlpExporterProvider(cli.Config{LogOTELProto: "grpc", LogOTELEndpoint: "http://otel:4317"}); err == nil {
		t.Fatal("expected error for grpc endpoint with scheme")
	}
}
