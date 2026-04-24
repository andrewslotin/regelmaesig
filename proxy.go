package main

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// forward builds an upstream request from r, executes it with client, and returns the response.
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

// newStandardHandler returns a handler that forwards to upstream, caches successful responses,
// and falls back to a cached response (or empty JSON with X-Cache: MISS) on failure.
//
// When expiry is nil the response is cached indefinitely (static routes).
// When expiry is non-nil it is called with the buffered body; a zero return means "do not cache"
// (e.g. an empty-array response).
func newStandardHandler(client *http.Client, upstream, emptyBody string, cache *Cache, expiry func([]byte) time.Time, metrics *Metrics) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.RequestURI()
		path := routePath(r)

		start := time.Now()
		resp, err := forward(client, upstream, r)
		if err != nil {
			metrics.UpstreamErrorsTotal.WithLabelValues(r.Method, path, errorReason(err)).Inc()
			serveFallback(w, cache, key, emptyBody, metrics, r)
			return
		}
		defer resp.Body.Close() //nolint:errcheck

		duration := time.Since(start)
		metrics.UpstreamRequestDuration.WithLabelValues(r.Method, path).Observe(duration.Seconds())
		metrics.UpstreamRequestsTotal.WithLabelValues(r.Method, path, strconv.Itoa(resp.StatusCode)).Inc()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			metrics.UpstreamErrorsTotal.WithLabelValues(r.Method, path, httpErrorReason(resp.StatusCode)).Inc()
			serveFallback(w, cache, key, emptyBody, metrics, r)
			return
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
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
				statusCode: resp.StatusCode,
				header:     resp.Header.Clone(),
				body:       body,
				expiresAt:  expiresAt,
			})
		}

		for k, vs := range resp.Header {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		w.Write(body) //nolint:errcheck
	}
}

// newPassthroughHandler returns a handler that forwards to upstream and falls back to
// emptyBody JSON on any error or non-2xx response. Unlike newStandardHandler, it does not cache.
func newPassthroughHandler(client *http.Client, upstream, emptyBody string, metrics *Metrics) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := routePath(r)

		start := time.Now()
		resp, err := forward(client, upstream, r)
		if err != nil {
			metrics.UpstreamErrorsTotal.WithLabelValues(r.Method, path, errorReason(err)).Inc()
			metrics.FallbackResponsesTotal.WithLabelValues(r.Method, path).Inc()
			writeEmptyJSON(w, emptyBody)
			return
		}
		defer resp.Body.Close() //nolint:errcheck

		duration := time.Since(start)
		metrics.UpstreamRequestDuration.WithLabelValues(r.Method, path).Observe(duration.Seconds())
		metrics.UpstreamRequestsTotal.WithLabelValues(r.Method, path, strconv.Itoa(resp.StatusCode)).Inc()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			metrics.UpstreamErrorsTotal.WithLabelValues(r.Method, path, httpErrorReason(resp.StatusCode)).Inc()
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
