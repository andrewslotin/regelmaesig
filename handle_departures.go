package main

import (
	"net/http"
)

func handleDepartures(providers []DataProvider, cache *Cache, metrics *Metrics) http.HandlerFunc {
	return newStandardHandler(providers, `{"departures":[]}`, cache, departuresExpiry, metrics)
}
