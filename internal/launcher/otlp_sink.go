package launcher

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	sdklog "go.opentelemetry.io/otel/sdk/log"

	"github.com/qxip/gossipper/internal/cli"
	"github.com/qxip/gossipper/internal/eventlog"
)

// newOTLPSink builds an eventlog.Sink that exports records to an OTLP
// collector. The transport (gRPC vs HTTP) is selected by cfg.LogOTELProto.
//
// We resolve the exporter eagerly so connection problems surface during
// startup instead of silently dropping events at runtime.
var newOTLPSink = buildOTLPSink

func buildOTLPSink(cfg cli.Config, resource map[string]any) (eventlog.Sink, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	provider, err := otlpExporterProvider(cfg)
	if err != nil {
		return nil, err
	}
	return eventlog.NewOTLPSink(ctx, provider, resource)
}

func otlpExporterProvider(cfg cli.Config) (eventlog.OTLPProvider, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.LogOTELProto)) {
	case "grpc":
		return grpcExporterProvider(cfg)
	case "http":
		return httpExporterProvider(cfg)
	default:
		return nil, fmt.Errorf("unsupported OTLP protocol %q", cfg.LogOTELProto)
	}
}

func grpcExporterProvider(cfg cli.Config) (eventlog.OTLPProvider, error) {
	endpoint := strings.TrimSpace(cfg.LogOTELEndpoint)
	if endpoint == "" {
		return nil, errors.New("OTLP gRPC endpoint is empty")
	}
	if strings.HasPrefix(endpoint, "http://") || strings.HasPrefix(endpoint, "https://") {
		return nil, errors.New("OTLP gRPC endpoint must be host:port (no scheme)")
	}
	opts := []otlploggrpc.Option{otlploggrpc.WithEndpoint(endpoint)}
	if cfg.LogOTELInsecure {
		opts = append(opts, otlploggrpc.WithInsecure())
	}
	if len(cfg.LogOTELHeaders) > 0 {
		opts = append(opts, otlploggrpc.WithHeaders(cfg.LogOTELHeaders))
	}
	return func(ctx context.Context) (sdklog.Exporter, error) {
		return otlploggrpc.New(ctx, opts...)
	}, nil
}

func httpExporterProvider(cfg cli.Config) (eventlog.OTLPProvider, error) {
	endpoint := strings.TrimSpace(cfg.LogOTELEndpoint)
	if endpoint == "" {
		return nil, errors.New("OTLP HTTP endpoint is empty")
	}
	host, urlPath, insecure, err := parseHTTPEndpoint(endpoint, cfg.LogOTELInsecure)
	if err != nil {
		return nil, err
	}
	opts := []otlploghttp.Option{otlploghttp.WithEndpoint(host)}
	if urlPath != "" {
		opts = append(opts, otlploghttp.WithURLPath(urlPath))
	}
	if insecure {
		opts = append(opts, otlploghttp.WithInsecure())
	}
	if len(cfg.LogOTELHeaders) > 0 {
		opts = append(opts, otlploghttp.WithHeaders(cfg.LogOTELHeaders))
	}
	return func(ctx context.Context) (sdklog.Exporter, error) {
		return otlploghttp.New(ctx, opts...)
	}, nil
}

// parseHTTPEndpoint normalises endpoints in the forms `host:port`,
// `http://host:port`, `https://host:port[/path]` into the (host:port, path,
// insecure) tuple expected by otlploghttp.
func parseHTTPEndpoint(raw string, insecureFlag bool) (string, string, bool, error) {
	if !strings.Contains(raw, "://") {
		return raw, "", insecureFlag, nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", false, fmt.Errorf("invalid OTLP HTTP endpoint %q: %w", raw, err)
	}
	if u.Host == "" {
		return "", "", false, fmt.Errorf("invalid OTLP HTTP endpoint %q: missing host", raw)
	}
	insecure := insecureFlag
	switch strings.ToLower(u.Scheme) {
	case "http":
		insecure = true
	case "https":
		// keep insecure as configured by flag
	default:
		return "", "", false, fmt.Errorf("invalid OTLP HTTP endpoint scheme %q", u.Scheme)
	}
	return u.Host, u.Path, insecure, nil
}
