package main

import (
	"io"
	"net/http"
	"testing"
	"time"
)

const emptyDepartures = `{"departures":[]}`

func TestDepartures_Success(t *testing.T) {
	data := `{"departures":[{"id":"1"}],"realtimeDataUpdatedAt":1}`
	srvURL, cleanup := newTestStack(respondWith(http.StatusOK, data), 5*time.Second)
	defer cleanup()

	resp, err := http.Get(srvURL + "/stops/123/departures")
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

func TestDepartures_UpstreamError(t *testing.T) {
	srvURL, cleanup := newTestStack(respondWith(http.StatusInternalServerError, `{"error":"oops"}`), 5*time.Second)
	defer cleanup()

	resp, err := http.Get(srvURL + "/stops/123/departures")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != emptyDepartures {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestDepartures_NetworkError(t *testing.T) {
	srvURL, cleanup := newUnreachableStack(5 * time.Second)
	defer cleanup()

	resp, err := http.Get(srvURL + "/stops/123/departures")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != emptyDepartures {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestDepartures_Timeout(t *testing.T) {
	srvURL, cleanup := newTestStack(respondSlow(50*time.Millisecond), 1*time.Millisecond)
	defer cleanup()

	resp, err := http.Get(srvURL + "/stops/123/departures")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != emptyDepartures {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestDepartures_CacheHit(t *testing.T) {
	data := `{"departures":[{"when":"2099-01-01T10:00:00+01:00"}]}`
	srvURL, cleanup := newCachedTestStack(respondOnce(data), 5*time.Second, 0, 10)
	defer cleanup()

	// First request — upstream succeeds, populates cache.
	resp, err := http.Get(srvURL + "/stops/123/departures")
	if err != nil {
		t.Fatal(err)
	}
	io.ReadAll(resp.Body) //nolint:errcheck
	resp.Body.Close()    //nolint:errcheck

	// Second request — upstream returns 503, cache should be served.
	resp, err = http.Get(srvURL + "/stops/123/departures")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if got := resp.Header.Get("X-Cache"); got != "HIT" {
		t.Errorf("expected X-Cache: HIT, got %q", got)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != data {
		t.Errorf("expected cached body, got %s", body)
	}
}

func TestDepartures_CacheMiss(t *testing.T) {
	srvURL, cleanup := newCachedTestStack(respondWith(http.StatusServiceUnavailable, ``), 5*time.Second, 0, 10)
	defer cleanup()

	resp, err := http.Get(srvURL + "/stops/123/departures")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if got := resp.Header.Get("X-Cache"); got != "MISS" {
		t.Errorf("expected X-Cache: MISS, got %q", got)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != emptyDepartures {
		t.Errorf("expected empty departures, got %s", body)
	}
}

func TestDepartures_EmptyResponseNotCached(t *testing.T) {
	// Upstream returns empty departures on first call, then fails.
	// The empty response must not be cached; second request should be MISS.
	srvURL, cleanup := newCachedTestStack(respondOnce(emptyDepartures), 5*time.Second, 0, 10)
	defer cleanup()

	resp, _ := http.Get(srvURL + "/stops/123/departures")
	io.ReadAll(resp.Body) //nolint:errcheck
	resp.Body.Close()    //nolint:errcheck

	resp, err := http.Get(srvURL + "/stops/123/departures")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if got := resp.Header.Get("X-Cache"); got != "MISS" {
		t.Errorf("expected X-Cache: MISS for empty response, got %q", got)
	}
}
