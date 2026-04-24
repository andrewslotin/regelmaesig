package main

import (
	"io"
	"net/http"
	"testing"
	"time"
)

func TestLocations_Success(t *testing.T) {
	data := `[{"id":"1","name":"Berlin Hbf"}]`
	srvURL, cleanup := newTestStack(respondWith(http.StatusOK, data), 5*time.Second)
	defer cleanup()

	resp, err := http.Get(srvURL + "/locations?query=Berlin")
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

func TestLocations_UpstreamError(t *testing.T) {
	srvURL, cleanup := newTestStack(respondWith(http.StatusInternalServerError, `{"error":"oops"}`), 5*time.Second)
	defer cleanup()

	resp, err := http.Get(srvURL + "/locations?query=Berlin")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != `[]` {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestLocations_NetworkError(t *testing.T) {
	srvURL, cleanup := newUnreachableStack(5 * time.Second)
	defer cleanup()

	resp, err := http.Get(srvURL + "/locations?query=Berlin")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != `[]` {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestLocations_Timeout(t *testing.T) {
	srvURL, cleanup := newTestStack(respondSlow(50*time.Millisecond), 1*time.Millisecond)
	defer cleanup()

	resp, err := http.Get(srvURL + "/locations?query=Berlin")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != `[]` {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestNearby_Success(t *testing.T) {
	data := `[{"id":"1","name":"S Warschauer Str."}]`
	srvURL, cleanup := newTestStack(respondWith(http.StatusOK, data), 5*time.Second)
	defer cleanup()

	resp, err := http.Get(srvURL + "/locations/nearby?latitude=52.5&longitude=13.4")
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

func TestNearby_UpstreamError(t *testing.T) {
	srvURL, cleanup := newTestStack(respondWith(http.StatusInternalServerError, `{"error":"oops"}`), 5*time.Second)
	defer cleanup()

	resp, err := http.Get(srvURL + "/locations/nearby?latitude=52.5&longitude=13.4")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != `[]` {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestNearby_NetworkError(t *testing.T) {
	srvURL, cleanup := newUnreachableStack(5 * time.Second)
	defer cleanup()

	resp, err := http.Get(srvURL + "/locations/nearby?latitude=52.5&longitude=13.4")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != `[]` {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestNearby_Timeout(t *testing.T) {
	srvURL, cleanup := newTestStack(respondSlow(50*time.Millisecond), 1*time.Millisecond)
	defer cleanup()

	resp, err := http.Get(srvURL + "/locations/nearby?latitude=52.5&longitude=13.4")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != `[]` {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestLocations_CacheHit(t *testing.T) {
	data := `[{"id":"1","name":"Berlin Hbf"}]`
	srvURL, cleanup := newCachedTestStack(respondOnce(data), 5*time.Second, 10, 0)
	defer cleanup()

	resp, _ := http.Get(srvURL + "/locations?query=Berlin")
	io.ReadAll(resp.Body) //nolint:errcheck
	resp.Body.Close()    //nolint:errcheck

	resp, err := http.Get(srvURL + "/locations?query=Berlin")
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

func TestLocations_CacheMiss(t *testing.T) {
	srvURL, cleanup := newCachedTestStack(respondWith(http.StatusServiceUnavailable, ``), 5*time.Second, 10, 0)
	defer cleanup()

	resp, err := http.Get(srvURL + "/locations?query=Berlin")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if got := resp.Header.Get("X-Cache"); got != "MISS" {
		t.Errorf("expected X-Cache: MISS, got %q", got)
	}
}

func TestNearby_CacheHit(t *testing.T) {
	data := `[{"id":"1","name":"S Warschauer Str."}]`
	srvURL, cleanup := newCachedTestStack(respondOnce(data), 5*time.Second, 10, 0)
	defer cleanup()

	resp, _ := http.Get(srvURL + "/locations/nearby?latitude=52.5&longitude=13.4")
	io.ReadAll(resp.Body) //nolint:errcheck
	resp.Body.Close()    //nolint:errcheck

	resp, err := http.Get(srvURL + "/locations/nearby?latitude=52.5&longitude=13.4")
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

func TestNearby_CacheMiss(t *testing.T) {
	srvURL, cleanup := newCachedTestStack(respondWith(http.StatusServiceUnavailable, ``), 5*time.Second, 10, 0)
	defer cleanup()

	resp, err := http.Get(srvURL + "/locations/nearby?latitude=52.5&longitude=13.4")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if got := resp.Header.Get("X-Cache"); got != "MISS" {
		t.Errorf("expected X-Cache: MISS, got %q", got)
	}
}
