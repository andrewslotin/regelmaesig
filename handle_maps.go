package main

import (
	"io"
	"net/http"
)

func handleMap(client *http.Client, upstream string, cache *Cache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.RequestURI()

		resp, err := forward(client, upstream, r)
		if err != nil {
			if entry, ok := cache.Get(key); ok {
				writeFromCache(w, entry)
				return
			}
			w.Header().Set("X-Cache", "MISS")
			http.Error(w, "bad gateway", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close() //nolint:errcheck

		// Cache 2xx and 3xx (redirects) as successful responses.
		if resp.StatusCode >= 200 && resp.StatusCode < 400 {
			body, _ := io.ReadAll(resp.Body)
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
		if entry, ok := cache.Get(key); ok {
			writeFromCache(w, entry)
			return
		}
		copyUpstreamResponse(w, resp)
	}
}

