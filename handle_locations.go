package main

import (
	"net/http"
)

func handleLocations(client *http.Client, upstream string, cache *Cache, metrics *Metrics) http.HandlerFunc {
	return newStandardHandler(client, upstream, `[]`, cache, nil, metrics)
}

func handleNearby(client *http.Client, upstream string, cache *Cache, metrics *Metrics) http.HandlerFunc {
	return newStandardHandler(client, upstream, `[]`, cache, nil, metrics)
}
