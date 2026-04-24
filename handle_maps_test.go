package main

import (
	"net/http"
	"testing"
	"time"
)

func TestMap_Success(t *testing.T) {
	srvURL, cleanup := newTestStack(respondWith(http.StatusOK, `{}`), 5*time.Second)
	defer cleanup()

	resp, err := http.Get(srvURL + "/maps/bvg")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

// Maps proxies upstream errors as-is (no empty fallback).
func TestMap_UpstreamError(t *testing.T) {
	srvURL, cleanup := newTestStack(respondWith(http.StatusInternalServerError, `error`), 5*time.Second)
	defer cleanup()

	resp, err := http.Get(srvURL + "/maps/bvg")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", resp.StatusCode)
	}
}

func TestMap_NetworkError(t *testing.T) {
	srvURL, cleanup := newUnreachableStack(5 * time.Second)
	defer cleanup()

	resp, err := http.Get(srvURL + "/maps/bvg")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", resp.StatusCode)
	}
}

func TestMap_Timeout(t *testing.T) {
	srvURL, cleanup := newTestStack(respondSlow(50*time.Millisecond), 1*time.Millisecond)
	defer cleanup()

	resp, err := http.Get(srvURL + "/maps/bvg")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", resp.StatusCode)
	}
}

// Maps should forward redirects to the client.
func TestMap_Redirect(t *testing.T) {
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://example.com/map.pdf", http.StatusFound)
	})
	srvURL, cleanup := newTestStack(upstream, 5*time.Second)
	defer cleanup()

	client := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Get(srvURL + "/maps/bvg")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusFound {
		t.Errorf("expected 302, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Location"); got != "https://example.com/map.pdf" {
		t.Errorf("unexpected Location: %s", got)
	}
}
