package main

import (
	"net/http"
)

func handleTrips(client *http.Client, upstream string, cache *Cache, metrics *Metrics) http.HandlerFunc {
	return newStandardHandler(client, upstream, `{"trips":[]}`, cache, tripsExpiry, metrics)
}

func handleTrip(client *http.Client, upstream string, cache *Cache, metrics *Metrics) http.HandlerFunc {
	return newStandardHandler(client, upstream, `{"trip":{}}`, cache, tripExpiry, metrics)
}
