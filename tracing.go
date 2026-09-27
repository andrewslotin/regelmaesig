package main

import (
	"context"
	"log/slog"
	"os"
	"strconv"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
)

// setupTracing configures the global OTel tracer provider to export spans via OTLP/gRPC,
// honoring the standard OTEL_EXPORTER_OTLP_* env vars. It returns a shutdown func that
// flushes and stops the tracer provider; callers must always call it, even when tracing
// is disabled (in which case shutdown is a no-op).
func setupTracing(ctx context.Context) (shutdown func(context.Context) error, err error) {
	noop := func(context.Context) error { return nil }

	endpoint, enabled, reason := tracingEnabledFromEnv()
	if !enabled {
		slog.Info("tracing disabled: " + reason)
		return noop, nil
	}

	exporter, err := otlptracegrpc.New(ctx)
	if err != nil {
		return noop, err
	}

	res, err := resource.Merge(resource.Default(), resource.NewSchemaless(resourceAttrs()...))
	if err != nil {
		return noop, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(samplerRatio()))),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	slog.Info("tracing enabled", "endpoint", endpoint)
	return tp.Shutdown, nil
}

// tracingEnabledFromEnv applies the same guard as setupTracing without touching
// any SDK types, so the guard can be unit-tested hermetically.
func tracingEnabledFromEnv() (endpoint string, enabled bool, disableReason string) {
	if os.Getenv("OTEL_TRACES_EXPORTER") == "none" {
		return "", false, "OTEL_TRACES_EXPORTER=none"
	}
	traces := os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT")
	general := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if traces == "" && general == "" {
		return "", false, "no OTLP endpoint configured"
	}
	if traces != "" {
		return traces, true, ""
	}
	return general, true, ""
}

// resourceAttrs returns the resource attributes passed as the higher-precedence
// argument to resource.Merge in setupTracing. It resolves service.name itself
// (honoring OTEL_SERVICE_NAME when set, falling back to "regelmaesig" otherwise)
// rather than omitting it and relying on resource.Default()'s own OTEL_SERVICE_NAME
// detection: that detection is memoized process-wide the first time
// resource.Default() runs, so a later-set env var would otherwise not be reflected.
func resourceAttrs() []attribute.KeyValue {
	name := os.Getenv("OTEL_SERVICE_NAME")
	if name == "" {
		name = "regelmaesig"
	}
	return []attribute.KeyValue{
		semconv.ServiceName(name),
		semconv.ServiceVersion(serviceVersion()),
	}
}

func serviceVersion() string {
	if v := os.Getenv("VERSION"); v != "" {
		return v
	}
	return "dev"
}

func samplerRatio() float64 {
	if v := os.Getenv("OTEL_TRACES_SAMPLER_ARG"); v != "" {
		if ratio, err := strconv.ParseFloat(v, 64); err == nil {
			return ratio
		}
	}
	return 1.0
}
