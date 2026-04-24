package main

import (
	"net/http"
)

func handleJourneys(client *http.Client, upstream string, cache *Cache) http.HandlerFunc {
	return newStandardHandler(client, upstream, `{"journeys":[]}`, cache, journeysExpiry)
}

func handleRefreshJourney(client *http.Client, upstream string, cache *Cache) http.HandlerFunc {
	return newStandardHandler(client, upstream, `{"journey":{}}`, cache, refreshJourneyExpiry)
}
