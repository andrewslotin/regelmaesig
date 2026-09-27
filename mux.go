package main

import (
	"io"
	"net/http"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// newMux creates an http.Handler with all routes registered.
// upstreamURL and timeout are injected so tests can use a local server.
// staticCap and dynamicCap control the LRU capacity for each cache tier;
// 0 disables that tier.
func newMux(upstreamURL string, timeout time.Duration, staticCap, dynamicCap int, metrics *Metrics, hafas *HAFASClient) http.Handler {
	client := instrumentedHTTPClient(timeout)
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}

	rest := &TransportRESTClient{Client: client, Upstream: upstreamURL}
	restOnly := []DataProvider{{Name: "transport_rest", Fetch: rest.Forward}}

	depProviders := restOnly
	arrProviders := restOnly
	locProviders := restOnly
	nearbyProviders := restOnly
	stopProviders := restOnly
	if hafas != nil {
		hafasProvider := "hafas"
		restProvider := DataProvider{Name: "transport_rest", Fetch: rest.Forward}
		depProviders = []DataProvider{restProvider, {Name: hafasProvider, Fetch: hafas.Departures}}
		arrProviders = []DataProvider{restProvider, {Name: hafasProvider, Fetch: hafas.Arrivals}}
		locProviders = []DataProvider{restProvider, {Name: hafasProvider, Fetch: hafas.Locations}}
		nearbyProviders = []DataProvider{restProvider, {Name: hafasProvider, Fetch: hafas.Nearby}}
		stopProviders = []DataProvider{restProvider, {Name: hafasProvider, Fetch: hafas.Stop}}
	}

	staticCache := NewCache(staticCap)
	dynamicCache := NewCache(dynamicCap)

	mux := http.NewServeMux()

	mux.Handle("GET /metrics", metrics.Handler())

	mux.HandleFunc("GET /stops/reachable-from", handleReachableFrom(restOnly, dynamicCache, metrics))
	mux.HandleFunc("GET /stops/{id}/departures", handleDepartures(depProviders, dynamicCache, metrics))
	mux.HandleFunc("GET /stops/{id}/arrivals", handleArrivals(arrProviders, dynamicCache, metrics))
	mux.HandleFunc("GET /stops/{id}", handleStop(stopProviders, staticCache, metrics))

	mux.HandleFunc("GET /journeys/{ref}", handleRefreshJourney(restOnly, dynamicCache, metrics))
	mux.HandleFunc("GET /journeys", handleJourneys(restOnly, dynamicCache, metrics))

	mux.HandleFunc("GET /trips/{id}", handleTrip(restOnly, dynamicCache, metrics))
	mux.HandleFunc("GET /trips", handleTrips(restOnly, dynamicCache, metrics))

	mux.HandleFunc("GET /locations/nearby", handleNearby(nearbyProviders, staticCache, metrics))
	mux.HandleFunc("GET /locations", handleLocations(locProviders, staticCache, metrics))

	mux.HandleFunc("GET /radar", handleRadar(client, upstreamURL, metrics))

	mux.HandleFunc("GET /stations/{id}", handleStation(restOnly, staticCache, metrics))
	mux.HandleFunc("GET /stations", handleStations(restOnly, staticCache, metrics))

	mux.HandleFunc("GET /lines/{id}", handleLine(restOnly, staticCache, metrics))
	mux.HandleFunc("GET /lines", handleLines(restOnly, staticCache, metrics))

	mux.HandleFunc("GET /shapes/{id}", handleShape(client, upstreamURL, staticCache, metrics))

	mux.HandleFunc("GET /maps/{type}", handleMap(client, upstreamURL, staticCache, metrics))

	mux.HandleFunc("GET /compact/departures", handleCompactDepartures(client, upstreamURL, dynamicCache, metrics, hafas))
	mux.HandleFunc("GET /healthz", handleHealthz)

	return otelhttp.NewHandler(mux, "regelmaesig",
		otelhttp.WithFilter(func(r *http.Request) bool {
			return r.URL.Path != "/metrics"
		}),
		otelhttp.WithSpanNameFormatter(func(operation string, r *http.Request) string {
			if r.Pattern != "" {
				return r.Pattern
			}
			return operation
		}),
	)
}

// handleHealthz is a trivial liveness probe handler.
func handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, "ok") //nolint:errcheck
}
