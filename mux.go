package main

import (
	"io"
	"net/http"
	"time"
)

// newMux creates an http.ServeMux with all routes registered.
// upstreamURL and timeout are injected so tests can use a local server.
// staticCap and dynamicCap control the LRU capacity for each cache tier;
// 0 disables that tier.
func newMux(upstreamURL string, timeout time.Duration, staticCap, dynamicCap int, metrics *Metrics, hafas *HAFASClient) *http.ServeMux {
	client := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	rest := &TransportRESTClient{Client: client, Upstream: upstreamURL}
	restOnly := []DataProvider{rest.Forward}

	depProviders := restOnly
	arrProviders := restOnly
	locProviders := restOnly
	nearbyProviders := restOnly
	stopProviders := restOnly
	if hafas != nil {
		depProviders = []DataProvider{rest.Forward, hafas.Departures}
		arrProviders = []DataProvider{rest.Forward, hafas.Arrivals}
		locProviders = []DataProvider{rest.Forward, hafas.Locations}
		nearbyProviders = []DataProvider{rest.Forward, hafas.Nearby}
		stopProviders = []DataProvider{rest.Forward, hafas.Stop}
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

	return mux
}

// handleHealthz is a trivial liveness probe handler.
func handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, "ok") //nolint:errcheck
}
