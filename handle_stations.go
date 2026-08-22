package main

import (
	"net/http"
)

func handleStations(providers []DataProvider, cache *Cache, metrics *Metrics) http.HandlerFunc {
	return newStandardHandler(providers, `{}`, cache, nil, metrics)
}

func handleStation(providers []DataProvider, cache *Cache, metrics *Metrics) http.HandlerFunc {
	return newStandardHandler(providers, `{}`, cache, nil, metrics)
}
