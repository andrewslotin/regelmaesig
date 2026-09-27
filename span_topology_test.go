package main

import (
	"net/http"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// setTestTracerProvider installs an SDK TracerProvider backed by an in-memory span
// recorder as the global TracerProvider, restoring the previous one on test cleanup.
// Because proxy.go resolves its tracer via getTracer() (a fresh otel.Tracer(...) call
// on every use) rather than a package-level variable cached at init time, swapping the
// global provider here takes effect on the very next span started anywhere in the
// package — no extra wiring needed.
func setTestTracerProvider(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()

	rec := tracetest.NewSpanRecorder()
	// WithResource(resource.Empty()) avoids triggering resource.Default()'s process-wide,
	// memoized env-based detection — doing so here would freeze the "unknown_service:..."
	// resource before other tests (e.g. TestSetupTracing_HonorsServiceName) get a chance
	// to set their own OTEL_SERVICE_NAME env var and observe it take effect.
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec), sdktrace.WithResource(resource.Empty()))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	return rec
}

// findSpan returns the first ended span with the given name, or nil.
func findSpan(spans []sdktrace.ReadOnlySpan, name string) sdktrace.ReadOnlySpan {
	for _, s := range spans {
		if s.Name() == name {
			return s
		}
	}
	return nil
}

func TestProviderSpanIsParentOfHTTPClientSpan(t *testing.T) {
	rec := setTestTracerProvider(t)

	srvURL, cleanup := newTestStack(respondWith(http.StatusOK, `{"departures":[]}`), 2*time.Second)
	defer cleanup()

	resp, err := http.Get(srvURL + "/stops/900000001/departures")
	if err != nil {
		t.Fatalf("GET /stops/{id}/departures: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got status %d, want %d", resp.StatusCode, http.StatusOK)
	}

	spans := rec.Ended()

	providerSpan := findSpan(spans, "provider.transport_rest")
	if providerSpan == nil {
		t.Fatalf("no provider.transport_rest span recorded; spans: %v", spanNames(spans))
	}

	clientSpan := findSpan(spans, "HTTP GET")
	if clientSpan == nil {
		t.Fatalf("no HTTP GET client span recorded; spans: %v", spanNames(spans))
	}

	if clientSpan.Parent().SpanID() != providerSpan.SpanContext().SpanID() {
		t.Fatalf("HTTP GET span's parent (%s) is not provider.transport_rest's span ID (%s)",
			clientSpan.Parent().SpanID(), providerSpan.SpanContext().SpanID())
	}
	if clientSpan.Parent().TraceID() != providerSpan.SpanContext().TraceID() {
		t.Fatalf("HTTP GET span's trace ID (%s) does not match provider.transport_rest's trace ID (%s)",
			clientSpan.Parent().TraceID(), providerSpan.SpanContext().TraceID())
	}
}

func TestCompactDeparturesEmitsProviderSpans(t *testing.T) {
	rec := setTestTracerProvider(t)

	srvURL, cleanup := newTestStack(respondWith(http.StatusOK, `{"departures":[]}`), 2*time.Second)
	defer cleanup()

	resp, err := http.Get(srvURL + "/compact/departures?stops=900000001")
	if err != nil {
		t.Fatalf("GET /compact/departures: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got status %d, want %d", resp.StatusCode, http.StatusOK)
	}

	spans := rec.Ended()
	if findSpan(spans, "provider.transport_rest") == nil {
		t.Fatalf("no provider.transport_rest span recorded for compact departures; spans: %v", spanNames(spans))
	}
}

func spanNames(spans []sdktrace.ReadOnlySpan) []string {
	names := make([]string, len(spans))
	for i, s := range spans {
		names[i] = s.Name()
	}
	return names
}
