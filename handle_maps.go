package main

import (
	"io"
	"net/http"
	"strconv"
	"time"
)

func handleMap(client *http.Client, upstream string, cache *Cache, metrics *Metrics) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.RequestURI()
		path := routePath(r)

		start := time.Now()
		resp, err := forward(client, upstream, r)
		if err != nil {
			metrics.UpstreamErrorsTotal.WithLabelValues(r.Method, path, errorReason(err)).Inc()
			if entry, ok := cache.Get(key); ok {
				writeFromCache(w, entry)
				return
			}
			w.Header().Set("X-Cache", "MISS")
			http.Error(w, "bad gateway", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close() //nolint:errcheck

		duration := time.Since(start)
		metrics.UpstreamRequestDuration.WithLabelValues(r.Method, path).Observe(duration.Seconds())
		metrics.UpstreamRequestsTotal.WithLabelValues(r.Method, path, strconv.Itoa(resp.StatusCode)).Inc()

		// Cache 2xx and 3xx (redirects) as successful responses.
		if resp.StatusCode >= 200 && resp.StatusCode < 400 {
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				if entry, ok := cache.Get(key); ok {
					writeFromCache(w, entry)
					return
				}
				w.Header().Set("X-Cache", "MISS")
				http.Error(w, "bad gateway", http.StatusBadGateway)
				return
			}
			cache.Set(key, &cacheEntry{
				statusCode: resp.StatusCode,
				header:     resp.Header.Clone(),
				body:       body,
			})
			for k, vs := range resp.Header {
				for _, v := range vs {
					w.Header().Add(k, v)
				}
			}
			w.WriteHeader(resp.StatusCode)
			w.Write(body) //nolint:errcheck
			return
		}

		// Non-2xx/3xx: serve from cache if available, otherwise pass through.
		metrics.UpstreamErrorsTotal.WithLabelValues(r.Method, path, httpErrorReason(resp.StatusCode)).Inc()
		if entry, ok := cache.Get(key); ok {
			writeFromCache(w, entry)
			return
		}
		copyUpstreamResponse(w, resp)
	}
}
