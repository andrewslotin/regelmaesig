package main

import (
	"net/http"
)

func handleDepartures(client *http.Client, upstream string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp, err := forward(client, upstream, r)
		if err != nil {
			writeEmptyJSON(w, `{"departures":[]}`)
			return
		}
		defer resp.Body.Close() //nolint:errcheck
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			writeEmptyJSON(w, `{"departures":[]}`)
			return
		}
		copyUpstreamResponse(w, resp)
	}
}
