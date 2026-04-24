package main

import (
	"io"
	"net/http"
)

func handleShape(client *http.Client, upstream string, cache *Cache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.RequestURI()

		resp, err := forward(client, upstream, r)
		if err != nil {
			serveFallback(w, cache, key, `{}`)
			return
		}
		defer resp.Body.Close() //nolint:errcheck

		// Shapes return redirects (3xx); treat 2xx and 3xx as cacheable success.
		if resp.StatusCode >= 200 && resp.StatusCode < 400 {
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				serveFallback(w, cache, key, `{}`)
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

		serveFallback(w, cache, key, `{}`)
	}
}
