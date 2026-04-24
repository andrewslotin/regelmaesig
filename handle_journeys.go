package main

import (
	"net/http"
)

func handleJourneys(client *http.Client, upstream string, cache *Cache, metrics *Metrics) http.HandlerFunc {
	return newStandardHandler(client, upstream, `{"journeys":[]}`, cache, journeysExpiry, metrics)
}

func handleRefreshJourney(client *http.Client, upstream string, cache *Cache, metrics *Metrics) http.HandlerFunc {
	return newStandardHandler(client, upstream, `{"journey":{}}`, cache, refreshJourneyExpiry, metrics)
}
