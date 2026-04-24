package main

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"time"
)

// newTestStack spins up a fake upstream and a proxy mux pointing to it.
// Returns the proxy server URL and a cleanup func.
func newTestStack(upstreamHandler http.Handler, timeout time.Duration) (srvURL string, cleanup func()) {
	upstream := httptest.NewServer(upstreamHandler)
	mux := newMux(upstream.URL, timeout, 0, 0)
	srv := httptest.NewServer(mux)
	return srv.URL, func() {
		srv.Close()
		upstream.Close()
	}
}

// newUnreachableStack spins up a proxy mux whose upstream is already closed.
func newUnreachableStack(timeout time.Duration) (srvURL string, cleanup func()) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	upstreamURL := upstream.URL
	upstream.Close()
	mux := newMux(upstreamURL, timeout, 0, 0)
	srv := httptest.NewServer(mux)
	return srv.URL, srv.Close
}

func respondWith(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		w.Write([]byte(body)) //nolint:errcheck
	}
}

func respondSlow(delay time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(delay)
		w.WriteHeader(http.StatusOK)
	}
}

// newCachedTestStack is like newTestStack but with caches enabled.
func newCachedTestStack(upstreamHandler http.Handler, timeout time.Duration, staticCap, dynamicCap int) (srvURL string, cleanup func()) {
	upstream := httptest.NewServer(upstreamHandler)
	mux := newMux(upstream.URL, timeout, staticCap, dynamicCap)
	srv := httptest.NewServer(mux)
	return srv.URL, func() {
		srv.Close()
		upstream.Close()
	}
}

// noRedirectClient is an http.Client that does not follow redirects, for testing redirect caching.
var noRedirectClient = &http.Client{
	CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// respondRedirectOnce serves a redirect on the first call; subsequent calls return 503.
func respondRedirectOnce(status int, location string) http.HandlerFunc {
	var mu sync.Mutex
	served := false
	return func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		first := !served
		served = true
		mu.Unlock()
		if first {
			w.Header().Set("Location", location)
			w.WriteHeader(status)
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}
}

// respondOnce serves a successful response on the first call; subsequent calls return 503.
func respondOnce(body string) http.HandlerFunc {
	var mu sync.Mutex
	served := false
	return func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		first := !served
		served = true
		mu.Unlock()
		if first {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(body)) //nolint:errcheck
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}
}
