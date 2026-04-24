package main

import (
	"net/http"
	"net/http/httptest"
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
