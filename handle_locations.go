package main

import (
	"net/http"
)

func handleLocations(providers []DataProvider, cache *Cache, metrics *Metrics) http.HandlerFunc {
	return newStandardHandler(providers, `[]`, cache, nil, metrics)
}

func handleNearby(providers []DataProvider, cache *Cache, metrics *Metrics) http.HandlerFunc {
	return newStandardHandler(providers, `[]`, cache, nil, metrics)
}
