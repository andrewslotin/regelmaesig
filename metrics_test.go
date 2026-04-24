package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// stackWithMetrics creates a test proxy + upstream pair and returns the registry so
// callers can inspect metric values after making requests.
func stackWithMetrics(t *testing.T, upstream http.Handler, timeout time.Duration) (srvURL string, reg *prometheus.Registry, cleanup func()) {
	t.Helper()
	upstreamSrv := httptest.NewServer(upstream)
	reg = prometheus.NewRegistry()
	m := NewMetrics(reg)
	mux := newMux(upstreamSrv.URL, timeout, 0, 0, m)
	srv := httptest.NewServer(mux)
	return srv.URL, reg, func() {
		srv.Close()
		upstreamSrv.Close()
	}
}

// unreachableStackWithMetrics creates a proxy whose upstream is already closed.
func unreachableStackWithMetrics(t *testing.T, timeout time.Duration) (srvURL string, reg *prometheus.Registry, cleanup func()) {
	t.Helper()
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := stub.URL
	stub.Close()
	reg = prometheus.NewRegistry()
	m := NewMetrics(reg)
	mux := newMux(url, timeout, 0, 0, m)
	srv := httptest.NewServer(mux)
	return srv.URL, reg, srv.Close
}

// histogramCount returns the sample count across all label combinations for a HistogramVec.
func histogramCount(t *testing.T, reg *prometheus.Registry, name string) uint64 {
	t.Helper()
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() == name {
			var total uint64
			for _, m := range mf.GetMetric() {
				total += m.GetHistogram().GetSampleCount()
			}
			return total
		}
	}
	return 0
}

// --- /metrics endpoint ---

func TestMetricsEndpoint(t *testing.T) {
	srvURL, _, cleanup := stackWithMetrics(t, respondWith(http.StatusOK, `[]`), 5*time.Second)
	defer cleanup()

	// Trigger a proxy request so counters are populated.
	r, _ := http.Get(srvURL + "/lines")
	io.ReadAll(r.Body) //nolint:errcheck
	r.Body.Close()    //nolint:errcheck

	resp, err := http.Get(srvURL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "upstream_requests_total") {
		t.Error("expected upstream_requests_total in /metrics output")
	}
}

// --- upstream_requests_total ---

func TestMetrics_UpstreamRequestsTotal_Success(t *testing.T) {
	srvURL, reg, cleanup := stackWithMetrics(t, respondWith(http.StatusOK, `[]`), 5*time.Second)
	defer cleanup()

	resp, _ := http.Get(srvURL + "/lines")
	io.ReadAll(resp.Body) //nolint:errcheck
	resp.Body.Close()    //nolint:errcheck

	want := `
		# HELP upstream_requests_total Total requests forwarded to upstream, by method, path pattern, and HTTP status.
		# TYPE upstream_requests_total counter
		upstream_requests_total{method="GET",path="/lines",status="200"} 1
	`
	if err := testutil.GatherAndCompare(reg, strings.NewReader(want), "upstream_requests_total"); err != nil {
		t.Error(err)
	}
}

func TestMetrics_UpstreamRequestsTotal_UpstreamError(t *testing.T) {
	srvURL, reg, cleanup := stackWithMetrics(t, respondWith(http.StatusInternalServerError, ``), 5*time.Second)
	defer cleanup()

	resp, _ := http.Get(srvURL + "/lines")
	io.ReadAll(resp.Body) //nolint:errcheck
	resp.Body.Close()    //nolint:errcheck

	want := `
		# HELP upstream_requests_total Total requests forwarded to upstream, by method, path pattern, and HTTP status.
		# TYPE upstream_requests_total counter
		upstream_requests_total{method="GET",path="/lines",status="500"} 1
	`
	if err := testutil.GatherAndCompare(reg, strings.NewReader(want), "upstream_requests_total"); err != nil {
		t.Error(err)
	}
}

func TestMetrics_UpstreamRequestsTotal_NetworkError(t *testing.T) {
	srvURL, reg, cleanup := unreachableStackWithMetrics(t, 5*time.Second)
	defer cleanup()

	resp, _ := http.Get(srvURL + "/lines")
	io.ReadAll(resp.Body) //nolint:errcheck
	resp.Body.Close()    //nolint:errcheck

	// Network errors never reach upstream, so no upstream_requests_total entry.
	if got := testutil.CollectAndCount(reg, "upstream_requests_total"); got != 0 {
		t.Errorf("expected 0 upstream_requests_total samples, got %d", got)
	}
}

// --- upstream_request_duration_seconds ---

func TestMetrics_UpstreamRequestDuration_RecordedOnSuccess(t *testing.T) {
	srvURL, reg, cleanup := stackWithMetrics(t, respondWith(http.StatusOK, `[]`), 5*time.Second)
	defer cleanup()

	resp, _ := http.Get(srvURL + "/lines")
	io.ReadAll(resp.Body) //nolint:errcheck
	resp.Body.Close()    //nolint:errcheck

	if got := histogramCount(t, reg, "upstream_request_duration_seconds"); got != 1 {
		t.Errorf("expected 1 histogram observation, got %d", got)
	}
}

func TestMetrics_UpstreamRequestDuration_NotRecordedOnNetworkError(t *testing.T) {
	srvURL, reg, cleanup := unreachableStackWithMetrics(t, 5*time.Second)
	defer cleanup()

	resp, _ := http.Get(srvURL + "/lines")
	io.ReadAll(resp.Body) //nolint:errcheck
	resp.Body.Close()    //nolint:errcheck

	if got := histogramCount(t, reg, "upstream_request_duration_seconds"); got != 0 {
		t.Errorf("expected 0 histogram observations on network error, got %d", got)
	}
}

// --- upstream_errors_total ---

func TestMetrics_UpstreamErrorsTotal_ConnectionRefused(t *testing.T) {
	srvURL, reg, cleanup := unreachableStackWithMetrics(t, 5*time.Second)
	defer cleanup()

	resp, _ := http.Get(srvURL + "/lines")
	io.ReadAll(resp.Body) //nolint:errcheck
	resp.Body.Close()    //nolint:errcheck

	want := `
		# HELP upstream_errors_total Upstream failures by method, path pattern, and reason (timeout, connection_refused, http_5xx, etc.).
		# TYPE upstream_errors_total counter
		upstream_errors_total{method="GET",path="/lines",reason="connection_refused"} 1
	`
	if err := testutil.GatherAndCompare(reg, strings.NewReader(want), "upstream_errors_total"); err != nil {
		t.Error(err)
	}
}

func TestMetrics_UpstreamErrorsTotal_Timeout(t *testing.T) {
	srvURL, reg, cleanup := stackWithMetrics(t, respondSlow(50*time.Millisecond), 1*time.Millisecond)
	defer cleanup()

	resp, _ := http.Get(srvURL + "/lines")
	io.ReadAll(resp.Body) //nolint:errcheck
	resp.Body.Close()    //nolint:errcheck

	want := `
		# HELP upstream_errors_total Upstream failures by method, path pattern, and reason (timeout, connection_refused, http_5xx, etc.).
		# TYPE upstream_errors_total counter
		upstream_errors_total{method="GET",path="/lines",reason="timeout"} 1
	`
	if err := testutil.GatherAndCompare(reg, strings.NewReader(want), "upstream_errors_total"); err != nil {
		t.Error(err)
	}
}

func TestMetrics_UpstreamErrorsTotal_HTTP5xx(t *testing.T) {
	srvURL, reg, cleanup := stackWithMetrics(t, respondWith(http.StatusServiceUnavailable, ``), 5*time.Second)
	defer cleanup()

	resp, _ := http.Get(srvURL + "/lines")
	io.ReadAll(resp.Body) //nolint:errcheck
	resp.Body.Close()    //nolint:errcheck

	want := `
		# HELP upstream_errors_total Upstream failures by method, path pattern, and reason (timeout, connection_refused, http_5xx, etc.).
		# TYPE upstream_errors_total counter
		upstream_errors_total{method="GET",path="/lines",reason="http_5xx"} 1
	`
	if err := testutil.GatherAndCompare(reg, strings.NewReader(want), "upstream_errors_total"); err != nil {
		t.Error(err)
	}
}

// --- fallback_responses_total ---

func TestMetrics_FallbackResponsesTotal_NetworkError(t *testing.T) {
	srvURL, reg, cleanup := unreachableStackWithMetrics(t, 5*time.Second)
	defer cleanup()

	resp, _ := http.Get(srvURL + "/lines")
	io.ReadAll(resp.Body) //nolint:errcheck
	resp.Body.Close()    //nolint:errcheck

	want := `
		# HELP fallback_responses_total How often the proxy returned an empty fallback instead of upstream data.
		# TYPE fallback_responses_total counter
		fallback_responses_total{method="GET",path="/lines"} 1
	`
	if err := testutil.GatherAndCompare(reg, strings.NewReader(want), "fallback_responses_total"); err != nil {
		t.Error(err)
	}
}

func TestMetrics_FallbackResponsesTotal_UpstreamError(t *testing.T) {
	srvURL, reg, cleanup := stackWithMetrics(t, respondWith(http.StatusServiceUnavailable, ``), 5*time.Second)
	defer cleanup()

	resp, _ := http.Get(srvURL + "/lines")
	io.ReadAll(resp.Body) //nolint:errcheck
	resp.Body.Close()    //nolint:errcheck

	want := `
		# HELP fallback_responses_total How often the proxy returned an empty fallback instead of upstream data.
		# TYPE fallback_responses_total counter
		fallback_responses_total{method="GET",path="/lines"} 1
	`
	if err := testutil.GatherAndCompare(reg, strings.NewReader(want), "fallback_responses_total"); err != nil {
		t.Error(err)
	}
}

func TestMetrics_FallbackResponsesTotal_NotIncrementedOnSuccess(t *testing.T) {
	srvURL, reg, cleanup := stackWithMetrics(t, respondWith(http.StatusOK, `[]`), 5*time.Second)
	defer cleanup()

	resp, _ := http.Get(srvURL + "/lines")
	io.ReadAll(resp.Body) //nolint:errcheck
	resp.Body.Close()    //nolint:errcheck

	if got := testutil.CollectAndCount(reg, "fallback_responses_total"); got != 0 {
		t.Errorf("expected no fallback_responses_total on success, got %d entries", got)
	}
}
