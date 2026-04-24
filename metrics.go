package main

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds Prometheus metrics for observing upstream request behavior.
type Metrics struct {
	reg                    prometheus.Gatherer
	UpstreamRequestsTotal  *prometheus.CounterVec
	UpstreamRequestDuration *prometheus.HistogramVec
	UpstreamErrorsTotal    *prometheus.CounterVec
	FallbackResponsesTotal *prometheus.CounterVec
}

// NewMetrics registers all metrics with reg and returns the Metrics struct.
func NewMetrics(reg *prometheus.Registry) *Metrics {
	factory := promauto.With(reg)
	return &Metrics{
		reg: reg,
		UpstreamRequestsTotal: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "upstream_requests_total",
			Help: "Total requests forwarded to upstream, by method, path pattern, and HTTP status.",
		}, []string{"method", "path", "status"}),
		UpstreamRequestDuration: factory.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "upstream_request_duration_seconds",
			Help:    "Upstream response latency in seconds.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "path"}),
		UpstreamErrorsTotal: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "upstream_errors_total",
			Help: "Upstream failures by method, path pattern, and reason (timeout, connection_refused, http_5xx, etc.).",
		}, []string{"method", "path", "reason"}),
		FallbackResponsesTotal: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "fallback_responses_total",
			Help: "How often the proxy returned an empty fallback instead of upstream data.",
		}, []string{"method", "path"}),
	}
}

// Handler returns an HTTP handler that exposes the collected metrics.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{})
}

// errorReason classifies a forward error into a label value for upstream_errors_total.
func errorReason(err error) string {
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Timeout() {
		return "timeout"
	}
	if strings.Contains(err.Error(), "connection refused") {
		return "connection_refused"
	}
	return "unknown"
}

// routePath returns the route pattern path for use as a metric label (high-cardinality
// path values are avoided by using the pattern rather than the literal URL).
func routePath(r *http.Request) string {
	return strings.TrimPrefix(r.Pattern, r.Method+" ")
}
