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
