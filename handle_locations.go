package main

import (
	"net/http"
)

func handleLocations(client *http.Client, upstream string, cache *Cache) http.HandlerFunc {
	return newStandardHandler(client, upstream, `[]`, cache, nil)
}

func handleNearby(client *http.Client, upstream string, cache *Cache) http.HandlerFunc {
	return newStandardHandler(client, upstream, `[]`, cache, nil)
}
