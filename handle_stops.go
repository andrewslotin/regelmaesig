package main

import (
	"net/http"
)

func handleReachableFrom(client *http.Client, upstream string, cache *Cache, metrics *Metrics) http.HandlerFunc {
	return newStandardHandler(client, upstream, `{"reachable":[]}`, cache, reachableFromExpiry, metrics)
}

func handleStop(client *http.Client, upstream string, cache *Cache, metrics *Metrics) http.HandlerFunc {
	return newStandardHandler(client, upstream, `{}`, cache, nil, metrics)
}
