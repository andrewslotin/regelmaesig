package main

import (
	"net/http"
)

func handleStations(client *http.Client, upstream string, cache *Cache) http.HandlerFunc {
	return newStandardHandler(client, upstream, `{}`, cache, nil)
}

func handleStation(client *http.Client, upstream string, cache *Cache) http.HandlerFunc {
	return newStandardHandler(client, upstream, `{}`, cache, nil)
}
