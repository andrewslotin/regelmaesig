package main

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
)

// clearOTELEnv unsets the OTLP-related env vars this package reads, so tests
// aren't affected by ambient environment (e.g. a developer running the suite
// with OTEL_EXPORTER_OTLP_ENDPOINT already set).
func clearOTELEnv(t *testing.T) {
	t.Helper()
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
	t.Setenv("OTEL_TRACES_EXPORTER", "")
	t.Setenv("OTEL_SERVICE_NAME", "")
	t.Setenv("OTEL_EXPORTER_OTLP_INSECURE", "")
}

func TestSetupTracingNoEnv(t *testing.T) {
	clearOTELEnv(t)

	shutdown, err := setupTracing(context.Background())
	if err != nil {
		t.Fatalf("setupTracing() error = %v, want nil", err)
	}
	if shutdown == nil {
		t.Fatal("setupTracing() returned nil shutdown func")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown() error = %v, want nil", err)
	}
}

func TestSetupTracing_DisabledWhenExporterNone(t *testing.T) {
	clearOTELEnv(t)
	saved := otel.GetTracerProvider()
	t.Cleanup(func() { otel.SetTracerProvider(saved) })

	t.Setenv("OTEL_TRACES_EXPORTER", "none")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:4317")

	shutdown, err := setupTracing(context.Background())
	if err != nil {
		t.Fatalf("setupTracing() error = %v, want nil", err)
	}
	if shutdown == nil {
		t.Fatal("setupTracing() returned nil shutdown func")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown() error = %v, want nil", err)
	}

	if got := otel.GetTracerProvider(); got != saved {
		t.Fatalf("setupTracing() installed a tracer provider despite OTEL_TRACES_EXPORTER=none")
	}
}

// TestSetupTracing_HonorsServiceName exercises resourceAttrs directly rather
// than spinning up a real TracerProvider + OTLP exporter. This keeps the test
// hermetic: no goroutines, no network, no span emission, no batch-flush wait.
func TestSetupTracing_HonorsServiceName(t *testing.T) {
	clearOTELEnv(t)
	t.Setenv("OTEL_SERVICE_NAME", "custom-name")

	attrs := resourceAttrs()

	var got string
	for _, kv := range attrs {
		if kv.Key == semconv.ServiceNameKey {
			got = kv.Value.AsString()
		}
	}
	if got != "custom-name" {
		t.Fatalf("service.name = %q, want %q", got, "custom-name")
	}
}

func TestTracingEnabledFromEnv(t *testing.T) {
	tests := []struct {
		name         string
		env          map[string]string
		wantEndpoint string
		wantEnabled  bool
	}{
		{"nothing set", nil, "", false},
		{"exporter=none wins over endpoint", map[string]string{"OTEL_TRACES_EXPORTER": "none", "OTEL_EXPORTER_OTLP_ENDPOINT": "localhost:4317"}, "", false},
		{"general endpoint enables", map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "localhost:4317"}, "localhost:4317", true},
		{"traces endpoint alone enables", map[string]string{"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT": "traces:4317"}, "traces:4317", true},
		{"traces endpoint overrides general", map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "general:4317", "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT": "traces:4317"}, "traces:4317", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clearOTELEnv(t)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			gotEndpoint, gotEnabled, _ := tracingEnabledFromEnv()
			if gotEnabled != tc.wantEnabled {
				t.Fatalf("enabled = %v, want %v", gotEnabled, tc.wantEnabled)
			}
			if gotEndpoint != tc.wantEndpoint {
				t.Fatalf("endpoint = %q, want %q", gotEndpoint, tc.wantEndpoint)
			}
		})
	}
}

func TestResourceAttrs_DefaultServiceName(t *testing.T) {
	clearOTELEnv(t)

	attrs := resourceAttrs()

	var got string
	for _, kv := range attrs {
		if kv.Key == semconv.ServiceNameKey {
			got = kv.Value.AsString()
		}
	}
	if got != "regelmaesig" {
		t.Fatalf("service.name = %q, want %q", got, "regelmaesig")
	}
}
