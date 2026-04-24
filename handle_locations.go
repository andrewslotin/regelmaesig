package main

import (
	"net/http"
)

func handleLocations(client *http.Client, upstream string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp, err := forward(client, upstream, r)
		if err != nil {
			writeEmptyJSON(w, `[]`)
			return
		}
		defer resp.Body.Close() //nolint:errcheck
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			writeEmptyJSON(w, `[]`)
			return
		}
		copyUpstreamResponse(w, resp)
	}
}

func handleNearby(client *http.Client, upstream string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp, err := forward(client, upstream, r)
		if err != nil {
			writeEmptyJSON(w, `[]`)
			return
		}
		defer resp.Body.Close() //nolint:errcheck
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			writeEmptyJSON(w, `[]`)
			return
		}
		copyUpstreamResponse(w, resp)
	}
}
