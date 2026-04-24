package main

import (
	"net/http"
)

func handleJourneys(client *http.Client, upstream string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp, err := forward(client, upstream, r)
		if err != nil {
			writeEmptyJSON(w, `{"journeys":[]}`)
			return
		}
		defer resp.Body.Close() //nolint:errcheck
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			writeEmptyJSON(w, `{"journeys":[]}`)
			return
		}
		copyUpstreamResponse(w, resp)
	}
}

func handleRefreshJourney(client *http.Client, upstream string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp, err := forward(client, upstream, r)
		if err != nil {
			writeEmptyJSON(w, `{"journey":{}}`)
			return
		}
		defer resp.Body.Close() //nolint:errcheck
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			writeEmptyJSON(w, `{"journey":{}}`)
			return
		}
		copyUpstreamResponse(w, resp)
	}
}
