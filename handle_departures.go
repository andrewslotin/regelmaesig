package main

import (
	"net/http"
)

func handleDepartures(client *http.Client, upstream string, cache *Cache) http.HandlerFunc {
	return newStandardHandler(client, upstream, `{"departures":[]}`, cache, departuresExpiry)
}
