package main

import (
	"io"
	"net/http"
	"testing"
	"time"
)

func TestShape_Success(t *testing.T) {
	data := `{"id":"1","coordinates":[[13.4,52.5]]}`
	srvURL, cleanup := newTestStack(respondWith(http.StatusOK, data), 5*time.Second)
	defer cleanup()

	resp, err := http.Get(srvURL + "/shapes/someId")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != data {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestShape_UpstreamError(t *testing.T) {
	srvURL, cleanup := newTestStack(respondWith(http.StatusNotFound, `{"error":"not found"}`), 5*time.Second)
	defer cleanup()

	resp, err := http.Get(srvURL + "/shapes/unknown")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != `{}` {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestShape_NetworkError(t *testing.T) {
	srvURL, cleanup := newUnreachableStack(5 * time.Second)
	defer cleanup()

	resp, err := http.Get(srvURL + "/shapes/someId")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != `{}` {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestShape_Timeout(t *testing.T) {
	srvURL, cleanup := newTestStack(respondSlow(50*time.Millisecond), 1*time.Millisecond)
	defer cleanup()

	resp, err := http.Get(srvURL + "/shapes/someId")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != `{}` {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestShape_RedirectCacheHit(t *testing.T) {
	srvURL, cleanup := newCachedTestStack(respondRedirectOnce(http.StatusFound, "https://example.com/shape"), 5*time.Second, 10, 0)
	defer cleanup()

	// First request — upstream returns 302, proxy caches and serves it.
	resp, _ := noRedirectClient.Get(srvURL + "/shapes/someId")
	io.ReadAll(resp.Body) //nolint:errcheck
	resp.Body.Close()    //nolint:errcheck

	// Second request — upstream returns 503, proxy serves cached 302.
	resp, err := noRedirectClient.Get(srvURL + "/shapes/someId")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if got := resp.Header.Get("X-Cache"); got != "HIT" {
		t.Errorf("expected X-Cache: HIT, got %q", got)
	}
	if resp.StatusCode != http.StatusFound {
		t.Errorf("expected 302, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Location"); got != "https://example.com/shape" {
		t.Errorf("expected Location header, got %q", got)
	}
}

func TestShape_CacheMiss(t *testing.T) {
	srvURL, cleanup := newCachedTestStack(respondWith(http.StatusServiceUnavailable, ``), 5*time.Second, 10, 0)
	defer cleanup()

	resp, err := http.Get(srvURL + "/shapes/someId")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if got := resp.Header.Get("X-Cache"); got != "MISS" {
		t.Errorf("expected X-Cache: MISS, got %q", got)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != `{}` {
		t.Errorf("expected empty fallback, got %s", body)
	}
}
