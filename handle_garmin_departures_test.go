package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// -- fixture helpers --

// garminUpstreamMux routes GET /stops/{id}/departures to a per-stop-ID
// handler, returning 404 for unknown IDs. This is what newTestStack /
// newCachedTestStack wrap for multi-stop tests.
func garminUpstreamMux(handlers map[string]http.HandlerFunc) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /stops/{id}/departures", func(w http.ResponseWriter, r *http.Request) {
		h, ok := handlers[r.PathValue("id")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		h(w, r)
	})
	return mux
}

// garminUpstreamDep builds a fixture upstream departure.
func garminUpstreamDep(when, plannedWhen string, delay *float64, cancelled bool, direction, lineName, lineProduct, stopName string, remarkTypes ...string) garminUpstreamDeparture {
	d := garminUpstreamDeparture{
		When:        when,
		PlannedWhen: plannedWhen,
		Delay:       delay,
		Cancelled:   cancelled,
		Direction:   direction,
	}
	d.Line.Name = lineName
	d.Line.Product = lineProduct
	d.Stop.Name = stopName
	for _, rt := range remarkTypes {
		d.Remarks = append(d.Remarks, struct {
			Type string `json:"type"`
		}{Type: rt})
	}
	return d
}

func f64(v float64) *float64 { return &v }

// mustRFC3339 formats t as RFC3339 and immediately re-parses the result,
// returning the formatted string and its epoch seconds. Expected epoch
// values in tests must always come from this round-trip, never from
// t.Unix() directly, so they reflect exactly what the production code
// (which parses the formatted string) will compute.
func mustRFC3339(t time.Time) (string, int64) {
	s := t.Format(time.RFC3339)
	parsed, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return s, parsed.Unix()
}

// queryRecorder captures the last query string seen by a fake upstream
// handler; guarded by a mutex so tests can read it safely under -race.
type queryRecorder struct {
	mu    sync.Mutex
	query url.Values
}

func (q *queryRecorder) handler(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q.mu.Lock()
		q.query = r.URL.Query()
		q.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		io.WriteString(w, body) //nolint:errcheck
	}
}

func (q *queryRecorder) get() url.Values {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.query
}

// -- tests --

func TestGarminDepartures_Success(t *testing.T) {
	base := time.Now()
	t0Str, t0Epoch := mustRFC3339(base.Add(10 * time.Minute)) // delay 90 -> dl:2, warning
	t1Str, t1Epoch := mustRFC3339(base.Add(5 * time.Minute))  // cancelled, time via plannedWhen
	t2Str, t2Epoch := mustRFC3339(base.Add(15 * time.Minute)) // delay -90 -> dl:-1
	pastWhen, _ := mustRFC3339(time.Date(2000, 1, 1, 10, 0, 0, 0, time.UTC))
	realtimeUpdated := base.Add(-1 * time.Minute).Unix()

	deps := []garminUpstreamDeparture{
		garminUpstreamDep(t0Str, "", f64(90), false, "S+U Pankow", "U2", "subway", "S+U Alexanderplatz", "info", "warning"),
		garminUpstreamDep("", t1Str, nil, true, "Hauptbahnhof", "M4", "tram", ""),
		garminUpstreamDep(t2Str, "", f64(-90), false, "S Flughafen BER Terminal 1-2", "S9", "suburban", ""),
		garminUpstreamDep(pastWhen, "", nil, false, "Nowhere", "X1", "bus", ""), // dropped: in the past
		garminUpstreamDep("", "", nil, false, "Nowhere", "X2", "bus", ""),       // dropped: no parseable time
	}
	body, err := json.Marshal(garminUpstreamResponse{Departures: deps, RealtimeDataUpdatedAt: realtimeUpdated})
	if err != nil {
		t.Fatal(err)
	}

	upstream := garminUpstreamMux(map[string]http.HandlerFunc{
		"900100003": respondWith(http.StatusOK, string(body)),
	})
	srvURL, cleanup := newTestStack(upstream, 5*time.Second)
	defer cleanup()

	resp, err := http.Get(srvURL + "/garmin/departures?stops=900100003")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}

	respBody, _ := io.ReadAll(resp.Body)

	expected := garminResponse{
		AsOf:    realtimeUpdated,
		Partial: 0,
		Boards: []garminBoard{
			{
				ID:   "900100003",
				Name: "S+U Alexanderplatz",
				Departures: []garminDeparture{
					{Line: "M4", Product: "tram", Direction: "Hauptbahnhof", Time: t1Epoch, Delay: 0, Cancelled: 1, Warning: 0},
					{Line: "U2", Product: "subway", Direction: "Pankow", Time: t0Epoch, Delay: 2, Cancelled: 0, Warning: 1},
					{Line: "S9", Product: "suburban", Direction: "Flughafen BER Terminal 1", Time: t2Epoch, Delay: -1, Cancelled: 0, Warning: 0},
				},
			},
		},
	}
	expectedBody, err := json.Marshal(expected)
	if err != nil {
		t.Fatal(err)
	}

	if string(respBody) != string(expectedBody) {
		t.Errorf("body mismatch\n got:  %s\n want: %s", respBody, expectedBody)
	}
}

func TestGarminDepartures_BadRequest(t *testing.T) {
	srvURL, cleanup := newTestStack(respondWith(http.StatusOK, `{"departures":[]}`), 5*time.Second)
	defer cleanup()

	cases := []string{
		"/garmin/departures",
		"/garmin/departures?stops=",
		"/garmin/departures?stops=,,,",
		"/garmin/departures?stops=abc",
		"/garmin/departures?stops=1234567890123", // 13 digits
		"/garmin/departures?stops=1,2,3,4,5",     // 5 IDs
	}
	for _, path := range cases {
		t.Run(path, func(t *testing.T) {
			resp, err := http.Get(srvURL + path)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close() //nolint:errcheck

			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("expected 400, got %d", resp.StatusCode)
			}
			if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
				t.Errorf("expected Content-Type application/json, got %q", ct)
			}
			body, _ := io.ReadAll(resp.Body)
			if string(body) != garminErrBadRequest {
				t.Errorf("unexpected body: %s", body)
			}
		})
	}
}

func TestGarminDepartures_UpstreamQueryParams(t *testing.T) {
	var rec queryRecorder
	upstream := garminUpstreamMux(map[string]http.HandlerFunc{
		"123": rec.handler(http.StatusOK, `{"departures":[]}`),
	})
	srvURL, cleanup := newTestStack(upstream, 5*time.Second)
	defer cleanup()

	resp, err := http.Get(srvURL + "/garmin/departures?stops=123&duration=999")
	if err != nil {
		t.Fatal(err)
	}
	io.ReadAll(resp.Body) //nolint:errcheck
	resp.Body.Close()     //nolint:errcheck

	q := rec.get()
	if got := q.Get("duration"); got != "120" {
		t.Errorf("expected duration=120 (clamped), got %q", got)
	}
	assertGarminFixedParams(t, q)

	resp, err = http.Get(srvURL + "/garmin/departures?stops=123")
	if err != nil {
		t.Fatal(err)
	}
	io.ReadAll(resp.Body) //nolint:errcheck
	resp.Body.Close()     //nolint:errcheck

	q = rec.get()
	if got := q.Get("duration"); got != "30" {
		t.Errorf("expected duration=30 (default), got %q", got)
	}
	assertGarminFixedParams(t, q)
}

func assertGarminFixedParams(t *testing.T, q url.Values) {
	t.Helper()
	want := map[string]string{
		"results":  "20",
		"remarks":  "true",
		"language": "en",
		"suburban": "true",
		"subway":   "true",
		"tram":     "true",
		"bus":      "true",
		"ferry":    "true",
		"regional": "true",
		"express":  "false",
		"pretty":   "false",
	}
	for k, v := range want {
		if got := q.Get(k); got != v {
			t.Errorf("expected %s=%s, got %q", k, v, got)
		}
	}
}

func TestGarminDepartures_Limit(t *testing.T) {
	base := time.Now()
	var deps []garminUpstreamDeparture
	for i := 0; i < 8; i++ {
		when, _ := mustRFC3339(base.Add(time.Duration(i+1) * time.Minute))
		deps = append(deps, garminUpstreamDep(when, "", nil, false, "X", fmt.Sprintf("L%d", i), "bus", ""))
	}
	body, err := json.Marshal(garminUpstreamResponse{Departures: deps})
	if err != nil {
		t.Fatal(err)
	}

	upstream := garminUpstreamMux(map[string]http.HandlerFunc{
		"123": respondWith(http.StatusOK, string(body)),
	})
	srvURL, cleanup := newTestStack(upstream, 5*time.Second)
	defer cleanup()

	cases := []struct {
		query string
		want  int
	}{
		{"", 6},
		{"&limit=2", 2},
		{"&limit=99", 8},
	}
	for _, c := range cases {
		resp, err := http.Get(srvURL + "/garmin/departures?stops=123" + c.query)
		if err != nil {
			t.Fatal(err)
		}
		var got garminResponse
		if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		resp.Body.Close() //nolint:errcheck

		if len(got.Boards) != 1 || len(got.Boards[0].Departures) != c.want {
			t.Errorf("query %q: expected %d departures, got %d", c.query, c.want, len(got.Boards[0].Departures))
		}
	}
}

func TestGarminDepartures_StopOrderPreserved(t *testing.T) {
	base := time.Now()
	whenA, _ := mustRFC3339(base.Add(5 * time.Minute))
	whenB, _ := mustRFC3339(base.Add(5 * time.Minute))

	bodyA, err := json.Marshal(garminUpstreamResponse{Departures: []garminUpstreamDeparture{
		garminUpstreamDep(whenA, "", nil, false, "X", "A1", "bus", "Stop A"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	bodyB, err := json.Marshal(garminUpstreamResponse{Departures: []garminUpstreamDeparture{
		garminUpstreamDep(whenB, "", nil, false, "X", "B1", "bus", "Stop B"),
	}})
	if err != nil {
		t.Fatal(err)
	}

	slowRespond := func(body string, delay time.Duration) http.HandlerFunc {
		h := respondWith(http.StatusOK, body)
		return func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(delay)
			h(w, r)
		}
	}

	upstream := garminUpstreamMux(map[string]http.HandlerFunc{
		"111": slowRespond(string(bodyA), 30*time.Millisecond), // requested first, finishes last
		"222": respondWith(http.StatusOK, string(bodyB)),
	})
	srvURL, cleanup := newTestStack(upstream, 5*time.Second)
	defer cleanup()

	resp, err := http.Get(srvURL + "/garmin/departures?stops=111,222")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	var got garminResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got.Boards) != 2 {
		t.Fatalf("expected 2 boards, got %d", len(got.Boards))
	}
	if got.Boards[0].ID != "111" || got.Boards[1].ID != "222" {
		t.Errorf("expected boards in requested order [111,222], got [%s,%s]", got.Boards[0].ID, got.Boards[1].ID)
	}
}

func TestGarminDepartures_PartialFailure(t *testing.T) {
	base := time.Now()
	when, _ := mustRFC3339(base.Add(5 * time.Minute))
	bodyA, err := json.Marshal(garminUpstreamResponse{Departures: []garminUpstreamDeparture{
		garminUpstreamDep(when, "", nil, false, "X", "A1", "bus", "Stop A"),
	}})
	if err != nil {
		t.Fatal(err)
	}

	upstream := garminUpstreamMux(map[string]http.HandlerFunc{
		"111": respondWith(http.StatusOK, string(bodyA)),
		"222": respondWith(http.StatusInternalServerError, `{}`),
	})
	srvURL, cleanup := newTestStack(upstream, 5*time.Second)
	defer cleanup()

	resp, err := http.Get(srvURL + "/garmin/departures?stops=111,222")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var got garminResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Partial != 1 {
		t.Errorf("expected partial=1, got %d", got.Partial)
	}
	if len(got.Boards) != 1 || got.Boards[0].ID != "111" {
		t.Errorf("expected only board 111, got %+v", got.Boards)
	}
}

func TestGarminDepartures_TotalFailure(t *testing.T) {
	upstream := garminUpstreamMux(map[string]http.HandlerFunc{
		"111": respondWith(http.StatusInternalServerError, `{}`),
	})
	srvURL, cleanup := newTestStack(upstream, 5*time.Second)
	defer cleanup()

	resp, err := http.Get(srvURL + "/garmin/departures?stops=111")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != garminErrUpstream {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestGarminDepartures_NetworkError(t *testing.T) {
	srvURL, cleanup := newUnreachableStack(5 * time.Second)
	defer cleanup()

	resp, err := http.Get(srvURL + "/garmin/departures?stops=111")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != garminErrUpstream {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestGarminDepartures_Timeout(t *testing.T) {
	upstream := garminUpstreamMux(map[string]http.HandlerFunc{
		"111": respondSlow(50 * time.Millisecond),
	})
	srvURL, cleanup := newTestStack(upstream, 1*time.Millisecond)
	defer cleanup()

	resp, err := http.Get(srvURL + "/garmin/departures?stops=111")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != garminErrUpstream {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestGarminDepartures_StaleFallback(t *testing.T) {
	base := time.Now()
	when, _ := mustRFC3339(base.Add(5 * time.Minute))
	body, err := json.Marshal(garminUpstreamResponse{Departures: []garminUpstreamDeparture{
		garminUpstreamDep(when, "", nil, false, "X", "A1", "bus", "Stop A"),
	}})
	if err != nil {
		t.Fatal(err)
	}

	upstream := garminUpstreamMux(map[string]http.HandlerFunc{
		"111": respondOnce(string(body)),
	})
	srvURL, cleanup := newCachedTestStack(upstream, 5*time.Second, 0, 10)
	defer cleanup()

	resp, err := http.Get(srvURL + "/garmin/departures?stops=111")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on first request, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("X-Cache"); got != "" {
		t.Errorf("expected no X-Cache header on first request, got %q", got)
	}
	firstBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close() //nolint:errcheck

	resp, err = http.Get(srvURL + "/garmin/departures?stops=111")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on stale fallback, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("X-Cache"); got != "HIT" {
		t.Errorf("expected X-Cache: HIT, got %q", got)
	}
	secondBody, _ := io.ReadAll(resp.Body)
	if string(secondBody) != string(firstBody) {
		t.Errorf("expected byte-identical cached body\n got:  %s\n want: %s", secondBody, firstBody)
	}
}

func TestGarminDepartures_PartialNotCached(t *testing.T) {
	base := time.Now()
	when, _ := mustRFC3339(base.Add(5 * time.Minute))
	bodyA, err := json.Marshal(garminUpstreamResponse{Departures: []garminUpstreamDeparture{
		garminUpstreamDep(when, "", nil, false, "X", "A1", "bus", "Stop A"),
	}})
	if err != nil {
		t.Fatal(err)
	}

	upstream := garminUpstreamMux(map[string]http.HandlerFunc{
		"111": respondOnce(string(bodyA)),
		"222": respondWith(http.StatusServiceUnavailable, ``),
	})
	srvURL, cleanup := newCachedTestStack(upstream, 5*time.Second, 0, 10)
	defer cleanup()

	resp, err := http.Get(srvURL + "/garmin/departures?stops=111,222")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on first request, got %d", resp.StatusCode)
	}
	var got garminResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close() //nolint:errcheck
	if got.Partial != 1 {
		t.Errorf("expected partial=1 on first request, got %d", got.Partial)
	}

	resp, err = http.Get(srvURL + "/garmin/departures?stops=111,222")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("expected 502 (partial response must not have been cached), got %d", resp.StatusCode)
	}
}

func TestGarminDepartures_EmptyNotCached(t *testing.T) {
	upstream := garminUpstreamMux(map[string]http.HandlerFunc{
		"111": respondOnce(`{"departures":[]}`),
	})
	srvURL, cleanup := newCachedTestStack(upstream, 5*time.Second, 0, 10)
	defer cleanup()

	resp, err := http.Get(srvURL + "/garmin/departures?stops=111")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on first request, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close() //nolint:errcheck

	if !strings.Contains(string(body), `"d":[]`) {
		t.Errorf("expected \"d\":[] in body, got %s", body)
	}
	if !strings.Contains(string(body), `"n":"111"`) {
		t.Errorf("expected board name to fall back to stop id, got %s", body)
	}

	resp, err = http.Get(srvURL + "/garmin/departures?stops=111")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("expected 502 (empty response must not have been cached), got %d", resp.StatusCode)
	}
}

func TestGarminStripDirection(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"S+U Pankow", "Pankow"},
		{"U Ruhleben", "Ruhleben"},
		{"S Süd", "Süd"},
		{"Spandau", "Spandau"},
		{strings.Repeat("ä", 30), strings.Repeat("ä", 24)},
		{"", ""},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			if got := garminStripDirection(c.in); got != c.want {
				t.Errorf("garminStripDirection(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestHealthz(t *testing.T) {
	srvURL, cleanup := newTestStack(respondWith(http.StatusOK, `{}`), 5*time.Second)
	defer cleanup()

	resp, err := http.Get(srvURL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/plain" {
		t.Errorf("expected Content-Type text/plain, got %q", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ok" {
		t.Errorf("expected body \"ok\", got %q", body)
	}
}
