package main

import (
	"io"
	"net/http"
	"testing"
	"time"
)

func TestStations_Success(t *testing.T) {
	data := `{"900000100001":{"id":"900000100001","name":"S+U Zoologischer Garten"}}`
	srvURL, cleanup := newTestStack(respondWith(http.StatusOK, data), 5*time.Second)
	defer cleanup()

	resp, err := http.Get(srvURL + "/stations?query=Zoo")
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

func TestStations_UpstreamError(t *testing.T) {
	srvURL, cleanup := newTestStack(respondWith(http.StatusInternalServerError, `{"error":"oops"}`), 5*time.Second)
	defer cleanup()

	resp, err := http.Get(srvURL + "/stations?query=Zoo")
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

func TestStations_NetworkError(t *testing.T) {
	srvURL, cleanup := newUnreachableStack(5 * time.Second)
	defer cleanup()

	resp, err := http.Get(srvURL + "/stations?query=Zoo")
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

func TestStations_Timeout(t *testing.T) {
	srvURL, cleanup := newTestStack(respondSlow(50*time.Millisecond), 1*time.Millisecond)
	defer cleanup()

	resp, err := http.Get(srvURL + "/stations?query=Zoo")
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

func TestStation_Success(t *testing.T) {
	data := `{"id":"900000100001","name":"S+U Zoologischer Garten"}`
	srvURL, cleanup := newTestStack(respondWith(http.StatusOK, data), 5*time.Second)
	defer cleanup()

	resp, err := http.Get(srvURL + "/stations/900000100001")
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

func TestStation_UpstreamError(t *testing.T) {
	srvURL, cleanup := newTestStack(respondWith(http.StatusNotFound, `{"error":"not found"}`), 5*time.Second)
	defer cleanup()

	resp, err := http.Get(srvURL + "/stations/unknown")
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

func TestStation_NetworkError(t *testing.T) {
	srvURL, cleanup := newUnreachableStack(5 * time.Second)
	defer cleanup()

	resp, err := http.Get(srvURL + "/stations/123")
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

func TestStation_Timeout(t *testing.T) {
	srvURL, cleanup := newTestStack(respondSlow(50*time.Millisecond), 1*time.Millisecond)
	defer cleanup()

	resp, err := http.Get(srvURL + "/stations/123")
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
