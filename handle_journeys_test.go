package main

import (
	"io"
	"net/http"
	"testing"
	"time"
)

func TestJourneys_Success(t *testing.T) {
	data := `{"journeys":[{"legs":[]}]}`
	srvURL, cleanup := newTestStack(respondWith(http.StatusOK, data), 5*time.Second)
	defer cleanup()

	resp, err := http.Get(srvURL + "/journeys")
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

func TestJourneys_UpstreamError(t *testing.T) {
	srvURL, cleanup := newTestStack(respondWith(http.StatusInternalServerError, `{"error":"oops"}`), 5*time.Second)
	defer cleanup()

	resp, err := http.Get(srvURL + "/journeys")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != `{"journeys":[]}` {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestJourneys_NetworkError(t *testing.T) {
	srvURL, cleanup := newUnreachableStack(5 * time.Second)
	defer cleanup()

	resp, err := http.Get(srvURL + "/journeys")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != `{"journeys":[]}` {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestJourneys_Timeout(t *testing.T) {
	srvURL, cleanup := newTestStack(respondSlow(50*time.Millisecond), 1*time.Millisecond)
	defer cleanup()

	resp, err := http.Get(srvURL + "/journeys")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != `{"journeys":[]}` {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestRefreshJourney_Success(t *testing.T) {
	data := `{"journey":{"legs":[]}}`
	srvURL, cleanup := newTestStack(respondWith(http.StatusOK, data), 5*time.Second)
	defer cleanup()

	resp, err := http.Get(srvURL + "/journeys/someRef")
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

func TestRefreshJourney_UpstreamError(t *testing.T) {
	srvURL, cleanup := newTestStack(respondWith(http.StatusInternalServerError, `{"error":"oops"}`), 5*time.Second)
	defer cleanup()

	resp, err := http.Get(srvURL + "/journeys/someRef")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != `{"journey":{}}` {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestRefreshJourney_NetworkError(t *testing.T) {
	srvURL, cleanup := newUnreachableStack(5 * time.Second)
	defer cleanup()

	resp, err := http.Get(srvURL + "/journeys/someRef")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != `{"journey":{}}` {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestRefreshJourney_Timeout(t *testing.T) {
	srvURL, cleanup := newTestStack(respondSlow(50*time.Millisecond), 1*time.Millisecond)
	defer cleanup()

	resp, err := http.Get(srvURL + "/journeys/someRef")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != `{"journey":{}}` {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestJourneys_CacheHit(t *testing.T) {
	data := `{"journeys":[{"legs":[{"arrival":"2099-01-01T10:00:00+01:00"}]}]}`
	srvURL, cleanup := newCachedTestStack(respondOnce(data), 5*time.Second, 0, 10)
	defer cleanup()

	resp, _ := http.Get(srvURL + "/journeys")
	io.ReadAll(resp.Body) //nolint:errcheck
	resp.Body.Close()    //nolint:errcheck

	resp, err := http.Get(srvURL + "/journeys")
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

func TestJourneys_CacheMiss(t *testing.T) {
	srvURL, cleanup := newCachedTestStack(respondWith(http.StatusServiceUnavailable, ``), 5*time.Second, 0, 10)
	defer cleanup()

	resp, err := http.Get(srvURL + "/journeys")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if got := resp.Header.Get("X-Cache"); got != "MISS" {
		t.Errorf("expected X-Cache: MISS, got %q", got)
	}
}

func TestRefreshJourney_CacheHit(t *testing.T) {
	data := `{"journey":{"legs":[{"arrival":"2099-01-01T10:00:00+01:00"}]}}`
	srvURL, cleanup := newCachedTestStack(respondOnce(data), 5*time.Second, 0, 10)
	defer cleanup()

	resp, _ := http.Get(srvURL + "/journeys/someRef")
	io.ReadAll(resp.Body) //nolint:errcheck
	resp.Body.Close()    //nolint:errcheck

	resp, err := http.Get(srvURL + "/journeys/someRef")
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

func TestRefreshJourney_CacheMiss(t *testing.T) {
	srvURL, cleanup := newCachedTestStack(respondWith(http.StatusServiceUnavailable, ``), 5*time.Second, 0, 10)
	defer cleanup()

	resp, err := http.Get(srvURL + "/journeys/someRef")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if got := resp.Header.Get("X-Cache"); got != "MISS" {
		t.Errorf("expected X-Cache: MISS, got %q", got)
	}
}
