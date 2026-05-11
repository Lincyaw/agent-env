// Package tracing wires the OpenTelemetry tracer provider with an OTLP/gRPC
// exporter. It is intentionally collector-driven: configuration follows the
// standard OTEL_* environment variables so an external otel-collector can be
// pointed at it without code changes.
//
// Enable with OTEL_ENABLED=true (or OTEL_SDK_DISABLED=false). When disabled,
// Setup is a no-op and the global tracer provider remains the default no-op,
// so otel.Tracer(...).Start(...) calls are cheap and safe.
//
// Recognised environment variables (subset of the OTEL spec):
//   - OTEL_ENABLED                  -- "true" to enable (default: false)
//   - OTEL_SERVICE_NAME             -- overrides the service name passed to Setup
//   - OTEL_EXPORTER_OTLP_ENDPOINT   -- e.g. "otel-collector:4317" (gRPC)
//   - OTEL_EXPORTER_OTLP_INSECURE   -- "true" for plaintext (default: true)
//   - OTEL_RESOURCE_ATTRIBUTES      -- "key=value,key=value"
//   - OTEL_TRACES_SAMPLER_ARG       -- ratio in [0,1] for parent-based sampler
package tracing

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// ShutdownFunc flushes pending spans and releases exporter resources.
// Safe to call multiple times; safe to defer immediately after Setup.
type ShutdownFunc func(context.Context) error

// Setup initialises the global TracerProvider when OTEL is enabled.
// When disabled, returns a no-op shutdown so callers can defer it unconditionally.
func Setup(ctx context.Context, serviceName string) (ShutdownFunc, error) {
	if !enabled() {
		return func(context.Context) error { return nil }, nil
	}

	if v := os.Getenv("OTEL_SERVICE_NAME"); v != "" {
		serviceName = v
	}

	res, err := buildResource(ctx, serviceName)
	if err != nil {
		return nil, fmt.Errorf("build otel resource: %w", err)
	}

	exporter, err := buildExporter(ctx)
	if err != nil {
		return nil, fmt.Errorf("build otlp trace exporter: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter,
			sdktrace.WithBatchTimeout(5*time.Second),
		),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(buildSampler()),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return func(shutdownCtx context.Context) error {
		return tp.Shutdown(shutdownCtx)
	}, nil
}

func enabled() bool {
	if v := os.Getenv("OTEL_ENABLED"); v != "" {
		b, _ := strconv.ParseBool(v)
		return b
	}
	if v := os.Getenv("OTEL_SDK_DISABLED"); v != "" {
		b, _ := strconv.ParseBool(v)
		return !b
	}
	return false
}

func buildResource(ctx context.Context, serviceName string) (*resource.Resource, error) {
	return resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithProcess(),
		resource.WithHost(),
		resource.WithTelemetrySDK(),
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
		),
	)
}

func buildExporter(ctx context.Context) (sdktrace.SpanExporter, error) {
	opts := []otlptracegrpc.Option{}

	insecureFlag := true
	if v := os.Getenv("OTEL_EXPORTER_OTLP_INSECURE"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			insecureFlag = b
		}
	}
	if insecureFlag {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}

	if ep := os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"); ep != "" {
		opts = append(opts, otlptracegrpc.WithEndpoint(stripScheme(ep)))
	} else if ep := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"); ep != "" {
		opts = append(opts, otlptracegrpc.WithEndpoint(stripScheme(ep)))
	}

	return otlptracegrpc.New(ctx, opts...)
}

func buildSampler() sdktrace.Sampler {
	if v := os.Getenv("OTEL_TRACES_SAMPLER_ARG"); v != "" {
		if ratio, err := strconv.ParseFloat(v, 64); err == nil && ratio >= 0 && ratio <= 1 {
			return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(ratio))
		}
	}
	return sdktrace.ParentBased(sdktrace.AlwaysSample())
}

func stripScheme(endpoint string) string {
	for _, prefix := range []string{"http://", "https://", "grpc://"} {
		if len(endpoint) > len(prefix) && endpoint[:len(prefix)] == prefix {
			return endpoint[len(prefix):]
		}
	}
	return endpoint
}
