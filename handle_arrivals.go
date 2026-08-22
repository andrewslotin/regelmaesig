package main

import (
	"net/http"
)

func handleArrivals(providers []DataProvider, cache *Cache, metrics *Metrics) http.HandlerFunc {
	return newStandardHandler(providers, `{"arrivals":[]}`, cache, arrivalsExpiry, metrics)
}
