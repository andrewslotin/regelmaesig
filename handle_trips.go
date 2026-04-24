package main

import (
	"net/http"
)

func handleTrips(client *http.Client, upstream string, cache *Cache) http.HandlerFunc {
	return newStandardHandler(client, upstream, `{"trips":[]}`, cache, tripsExpiry)
}

func handleTrip(client *http.Client, upstream string, cache *Cache) http.HandlerFunc {
	return newStandardHandler(client, upstream, `{"trip":{}}`, cache, tripExpiry)
}
