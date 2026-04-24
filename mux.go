package main

import (
	"net/http"
	"time"
)

// newMux creates an http.ServeMux with all routes registered.
// upstreamURL and timeout are injected so tests can use a local server.
// staticCap and dynamicCap control the LRU capacity for each cache tier;
// 0 disables that tier.
func newMux(upstreamURL string, timeout time.Duration, staticCap, dynamicCap int) *http.ServeMux {
	client := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	staticCache := NewCache(staticCap)
	dynamicCache := NewCache(dynamicCap)
	_ = staticCache

	mux := http.NewServeMux()

	mux.HandleFunc("GET /stops/reachable-from", handleReachableFrom(client, upstreamURL))
	mux.HandleFunc("GET /stops/{id}/departures", handleDepartures(client, upstreamURL, dynamicCache))
	mux.HandleFunc("GET /stops/{id}/arrivals", handleArrivals(client, upstreamURL, dynamicCache))
	mux.HandleFunc("GET /stops/{id}", handleStop(client, upstreamURL))

	mux.HandleFunc("GET /journeys/{ref}", handleRefreshJourney(client, upstreamURL, dynamicCache))
	mux.HandleFunc("GET /journeys", handleJourneys(client, upstreamURL, dynamicCache))

	mux.HandleFunc("GET /trips/{id}", handleTrip(client, upstreamURL, dynamicCache))
	mux.HandleFunc("GET /trips", handleTrips(client, upstreamURL, dynamicCache))

	mux.HandleFunc("GET /locations/nearby", handleNearby(client, upstreamURL))
	mux.HandleFunc("GET /locations", handleLocations(client, upstreamURL))

	mux.HandleFunc("GET /radar", handleRadar(client, upstreamURL))

	mux.HandleFunc("GET /stations/{id}", handleStation(client, upstreamURL))
	mux.HandleFunc("GET /stations", handleStations(client, upstreamURL))

	mux.HandleFunc("GET /lines/{id}", handleLine(client, upstreamURL))
	mux.HandleFunc("GET /lines", handleLines(client, upstreamURL))

	mux.HandleFunc("GET /shapes/{id}", handleShape(client, upstreamURL))

	mux.HandleFunc("GET /maps/{type}", handleMap(client, upstreamURL))

	return mux
}
