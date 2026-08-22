package main

import (
	"net/http"
)

func handleJourneys(providers []DataProvider, cache *Cache, metrics *Metrics) http.HandlerFunc {
	return newStandardHandler(providers, `{"journeys":[]}`, cache, journeysExpiry, metrics)
}

func handleRefreshJourney(providers []DataProvider, cache *Cache, metrics *Metrics) http.HandlerFunc {
	return newStandardHandler(providers, `{"journey":{}}`, cache, refreshJourneyExpiry, metrics)
}
