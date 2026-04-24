package main

import (
	"net/http"
)

func handleReachableFrom(client *http.Client, upstream string, cache *Cache) http.HandlerFunc {
	return newStandardHandler(client, upstream, `{"reachable":[]}`, cache, reachableFromExpiry)
}

func handleStop(client *http.Client, upstream string, cache *Cache) http.HandlerFunc {
	return newStandardHandler(client, upstream, `{}`, cache, nil)
}
