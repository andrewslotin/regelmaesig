package main

import (
	"io"
	"net/http"
	"testing"
	"time"
)

func TestLines_Success(t *testing.T) {
	data := `[{"id":"u1","name":"U1"}]`
	srvURL, cleanup := newTestStack(respondWith(http.StatusOK, data), 5*time.Second)
	defer cleanup()

	resp, err := http.Get(srvURL + "/lines")
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

func TestLines_UpstreamError(t *testing.T) {
	srvURL, cleanup := newTestStack(respondWith(http.StatusInternalServerError, `{"error":"oops"}`), 5*time.Second)
	defer cleanup()

	resp, err := http.Get(srvURL + "/lines")
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

func TestLines_NetworkError(t *testing.T) {
	srvURL, cleanup := newUnreachableStack(5 * time.Second)
	defer cleanup()

	resp, err := http.Get(srvURL + "/lines")
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

func TestLines_Timeout(t *testing.T) {
	srvURL, cleanup := newTestStack(respondSlow(50*time.Millisecond), 1*time.Millisecond)
	defer cleanup()

	resp, err := http.Get(srvURL + "/lines")
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

func TestLine_Success(t *testing.T) {
	data := `{"id":"u1","name":"U1"}`
	srvURL, cleanup := newTestStack(respondWith(http.StatusOK, data), 5*time.Second)
	defer cleanup()

	resp, err := http.Get(srvURL + "/lines/u1")
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

func TestLine_UpstreamError(t *testing.T) {
	srvURL, cleanup := newTestStack(respondWith(http.StatusNotFound, `{"error":"not found"}`), 5*time.Second)
	defer cleanup()

	resp, err := http.Get(srvURL + "/lines/unknown")
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

func TestLine_NetworkError(t *testing.T) {
	srvURL, cleanup := newUnreachableStack(5 * time.Second)
	defer cleanup()

	resp, err := http.Get(srvURL + "/lines/u1")
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

func TestLine_Timeout(t *testing.T) {
	srvURL, cleanup := newTestStack(respondSlow(50*time.Millisecond), 1*time.Millisecond)
	defer cleanup()

	resp, err := http.Get(srvURL + "/lines/u1")
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

func TestLines_CacheHit(t *testing.T) {
	data := `[{"id":"u1","name":"U1"}]`
	srvURL, cleanup := newCachedTestStack(respondOnce(data), 5*time.Second, 10, 0)
	defer cleanup()

	resp, _ := http.Get(srvURL + "/lines")
	io.ReadAll(resp.Body) //nolint:errcheck
	resp.Body.Close()    //nolint:errcheck

	resp, err := http.Get(srvURL + "/lines")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if got := resp.Header.Get("X-Cache"); got != "HIT" {
		t.Errorf("expected X-Cache: HIT, got %q", got)
	}
}

func TestLines_CacheMiss(t *testing.T) {
	srvURL, cleanup := newCachedTestStack(respondWith(http.StatusServiceUnavailable, ``), 5*time.Second, 10, 0)
	defer cleanup()

	resp, err := http.Get(srvURL + "/lines")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if got := resp.Header.Get("X-Cache"); got != "MISS" {
		t.Errorf("expected X-Cache: MISS, got %q", got)
	}
}

func TestLine_CacheHit(t *testing.T) {
	data := `{"id":"u1","name":"U1"}`
	srvURL, cleanup := newCachedTestStack(respondOnce(data), 5*time.Second, 10, 0)
	defer cleanup()

	resp, _ := http.Get(srvURL + "/lines/u1")
	io.ReadAll(resp.Body) //nolint:errcheck
	resp.Body.Close()    //nolint:errcheck

	resp, err := http.Get(srvURL + "/lines/u1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if got := resp.Header.Get("X-Cache"); got != "HIT" {
		t.Errorf("expected X-Cache: HIT, got %q", got)
	}
}

func TestLine_CacheMiss(t *testing.T) {
	srvURL, cleanup := newCachedTestStack(respondWith(http.StatusServiceUnavailable, ``), 5*time.Second, 10, 0)
	defer cleanup()

	resp, err := http.Get(srvURL + "/lines/u1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if got := resp.Header.Get("X-Cache"); got != "MISS" {
		t.Errorf("expected X-Cache: MISS, got %q", got)
	}
}
