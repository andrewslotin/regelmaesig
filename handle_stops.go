package main

import (
	"net/http"
)

func handleReachableFrom(providers []DataProvider, cache *Cache, metrics *Metrics) http.HandlerFunc {
	return newStandardHandler(providers, `{"reachable":[]}`, cache, reachableFromExpiry, metrics)
}

func handleStop(providers []DataProvider, cache *Cache, metrics *Metrics) http.HandlerFunc {
	return newStandardHandler(providers, `{}`, cache, nil, metrics)
}
