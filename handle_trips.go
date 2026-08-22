package main

import (
	"net/http"
)

func handleTrips(providers []DataProvider, cache *Cache, metrics *Metrics) http.HandlerFunc {
	return newStandardHandler(providers, `{"trips":[]}`, cache, tripsExpiry, metrics)
}

func handleTrip(providers []DataProvider, cache *Cache, metrics *Metrics) http.HandlerFunc {
	return newStandardHandler(providers, `{"trip":{}}`, cache, tripExpiry, metrics)
}
