package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// DataProviderFunc fetches data for r, returning the response body and headers on success.
type DataProviderFunc func(ctx context.Context, r *http.Request, metrics *Metrics) (body []byte, header http.Header, err error)

// DataProvider pairs a DataProviderFunc with a Name used to label its span in traces.
type DataProvider struct {
	Name  string
	Fetch DataProviderFunc
}

// getTracer returns the package tracer, resolved against the current global
// TracerProvider on every call (rather than cached once at init) so that
// swapping the global TracerProvider — as tests do — takes effect immediately.
func getTracer() trace.Tracer {
	return otel.Tracer("github.com/andrewslotin/regelmaesig")
}

// instrumentedHTTPClient returns an *http.Client with the given timeout whose transport
// is wrapped for OTel HTTP client instrumentation.
func instrumentedHTTPClient(timeout time.Duration) *http.Client {
	base := http.DefaultTransport
	return &http.Client{
		Timeout:   timeout,
		Transport: otelhttp.NewTransport(base),
	}
}

// TransportRESTClient forwards requests to a transport.rest-compatible upstream.
type TransportRESTClient struct {
	Client   *http.Client
	Upstream string
}

// Forward implements DataProviderFunc by forwarding r to the upstream and recording metrics
// with "transport_rest" as the upstream label.
func (c *TransportRESTClient) Forward(ctx context.Context, r *http.Request, metrics *Metrics) ([]byte, http.Header, error) {
	path := routePath(r)
	start := time.Now()
	// Build the outbound request from a shallow copy of r carrying ctx (which holds the
	// provider span from callProvider), not r's own context, so the instrumented client
	// transport nests its HTTP span under the provider span rather than under the server span.
	resp, err := forward(c.Client, c.Upstream, r.WithContext(ctx))
	if err != nil {
		metrics.UpstreamErrorsTotal.WithLabelValues("transport_rest", r.Method, path, errorReason(err)).Inc()
		return nil, nil, err
	}
	defer resp.Body.Close() //nolint:errcheck

	duration := time.Since(start)
	metrics.UpstreamRequestDuration.WithLabelValues("transport_rest", r.Method, path).Observe(duration.Seconds())
	metrics.UpstreamRequestsTotal.WithLabelValues("transport_rest", r.Method, path, strconv.Itoa(resp.StatusCode)).Inc()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		metrics.UpstreamErrorsTotal.WithLabelValues("transport_rest", r.Method, path, httpErrorReason(resp.StatusCode)).Inc()
		return nil, nil, fmt.Errorf("upstream returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}
	return body, resp.Header.Clone(), nil
}

// forward builds an upstream request from r, executes it with client, and returns the response.
// The outbound request is built with r.Context(), so callers that need the request to carry a
// specific context (e.g. one holding a provider span, so the instrumented client transport
// creates its HTTP span as that span's child) must pass r.WithContext(ctx) rather than r itself.
// The caller is responsible for closing the response body.
func forward(client *http.Client, upstream string, r *http.Request) (*http.Response, error) {
	url := upstream + r.URL.RequestURI()
	req, err := http.NewRequestWithContext(r.Context(), r.Method, url, r.Body)
	if err != nil {
		return nil, err
	}
	req.Header = r.Header.Clone()
	return client.Do(req)
}

// copyUpstreamResponse writes the upstream response headers, status code, and body to w.
// The caller is responsible for closing resp.Body before or after calling this.
func copyUpstreamResponse(w http.ResponseWriter, resp *http.Response) {
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body) //nolint:errcheck
}

// writeEmptyJSON writes an HTTP 200 response with body as the JSON payload.
func writeEmptyJSON(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, body) //nolint:errcheck
}

// newStandardHandler returns a handler that fetches data from the first successful provider,
// caches successful responses, and falls back to a cached response (or empty JSON with
// X-Cache: MISS) if all providers fail.
//
// When expiry is nil the response is cached indefinitely (static routes).
// When expiry is non-nil it is called with the buffered body; a zero return means "do not cache"
// (e.g. an empty-array response).
func newStandardHandler(providers []DataProvider, emptyBody string, cache *Cache, expiry func([]byte) time.Time, metrics *Metrics) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.RequestURI()

		var body []byte
		var header http.Header
		for _, p := range providers {
			var err error
			body, header, err = callProvider(r.Context(), p, r, metrics)
			if err == nil {
				break
			}
			body = nil
			header = nil
		}

		if body == nil {
			serveFallback(w, cache, key, emptyBody, metrics, r)
			return
		}

		var expiresAt time.Time
		if expiry != nil {
			expiresAt = expiry(body)
		}
		// Cache when: static route (expiry==nil), or dynamic with a future expiry time.
		if expiry == nil || (!expiresAt.IsZero() && expiresAt.After(time.Now())) {
			cache.Set(key, &cacheEntry{
				statusCode: http.StatusOK,
				header:     header,
				body:       body,
				expiresAt:  expiresAt,
			})
		}

		for k, vs := range header {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(http.StatusOK)
		w.Write(body) //nolint:errcheck
	}
}

// callProvider invokes p.Fetch inside a span named "provider.<p.Name>", recording the
// error (if any) on the span.
func callProvider(ctx context.Context, p DataProvider, r *http.Request, metrics *Metrics) ([]byte, http.Header, error) {
	ctx, span := getTracer().Start(ctx, "provider."+p.Name)
	defer span.End()

	body, header, err := p.Fetch(ctx, r, metrics)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	return body, header, err
}

// callProviderRaw wraps fn in a span named "provider.<name>", recording any error fn
// returns. Unlike callProvider, fn is not constrained to the DataProviderFunc signature:
// it receives the span-carrying ctx and returns only an error, so callers with a
// different fetch signature (e.g. the compact-departures fan-out) can still get a
// provider span that parents the outbound HTTP client span.
func callProviderRaw(ctx context.Context, name string, fn func(ctx context.Context) error) error {
	ctx, span := getTracer().Start(ctx, "provider."+name)
	defer span.End()

	err := fn(ctx)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	return err
}

// newPassthroughHandler returns a handler that forwards to upstream and falls back to
// emptyBody JSON on any error or non-2xx response. Unlike newStandardHandler, it does not cache.
func newPassthroughHandler(client *http.Client, upstream, emptyBody string, metrics *Metrics) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := routePath(r)

		start := time.Now()
		resp, err := forward(client, upstream, r)
		if err != nil {
			metrics.UpstreamErrorsTotal.WithLabelValues("transport_rest", r.Method, path, errorReason(err)).Inc()
			metrics.FallbackResponsesTotal.WithLabelValues(r.Method, path).Inc()
			writeEmptyJSON(w, emptyBody)
			return
		}
		defer resp.Body.Close() //nolint:errcheck

		duration := time.Since(start)
		metrics.UpstreamRequestDuration.WithLabelValues("transport_rest", r.Method, path).Observe(duration.Seconds())
		metrics.UpstreamRequestsTotal.WithLabelValues("transport_rest", r.Method, path, strconv.Itoa(resp.StatusCode)).Inc()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			metrics.UpstreamErrorsTotal.WithLabelValues("transport_rest", r.Method, path, httpErrorReason(resp.StatusCode)).Inc()
			metrics.FallbackResponsesTotal.WithLabelValues(r.Method, path).Inc()
			writeEmptyJSON(w, emptyBody)
			return
		}

		copyUpstreamResponse(w, resp)
	}
}

// writeFromCache writes a cached entry to w with X-Cache: HIT.
func writeFromCache(w http.ResponseWriter, entry *cacheEntry) {
	w.Header().Set("X-Cache", "HIT")
	for k, vs := range entry.header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(entry.statusCode)
	w.Write(entry.body) //nolint:errcheck
}

// serveFallback writes a cached response with X-Cache: HIT, or the empty JSON fallback
// with X-Cache: MISS when no cached entry is available. It increments fallback_responses_total
// only when no cached data is available.
func serveFallback(w http.ResponseWriter, cache *Cache, key, emptyBody string, metrics *Metrics, r *http.Request) {
	_, span := getTracer().Start(r.Context(), "fallback")
	defer span.End()

	if entry, ok := cache.Get(key); ok {
		writeFromCache(w, entry)
		return
	}
	metrics.FallbackResponsesTotal.WithLabelValues(r.Method, routePath(r)).Inc()
	w.Header().Set("X-Cache", "MISS")
	writeEmptyJSON(w, emptyBody)
}

// httpErrorReason returns an upstream_errors_total reason label for non-2xx HTTP responses.
func httpErrorReason(statusCode int) string {
	return fmt.Sprintf("http_%dxx", statusCode/100)
}
