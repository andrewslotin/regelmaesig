package main

import (
	"net/http"
)

func handleStations(client *http.Client, upstream string, cache *Cache, metrics *Metrics) http.HandlerFunc {
	return newStandardHandler(client, upstream, `{}`, cache, nil, metrics)
}

func handleStation(client *http.Client, upstream string, cache *Cache, metrics *Metrics) http.HandlerFunc {
	return newStandardHandler(client, upstream, `{}`, cache, nil, metrics)
}
