package main

import (
	"net/http"
)

func handleArrivals(client *http.Client, upstream string, cache *Cache, metrics *Metrics) http.HandlerFunc {
	return newStandardHandler(client, upstream, `{"arrivals":[]}`, cache, arrivalsExpiry, metrics)
}
