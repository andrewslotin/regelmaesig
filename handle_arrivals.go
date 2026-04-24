package main

import (
	"net/http"
)

func handleArrivals(client *http.Client, upstream string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp, err := forward(client, upstream, r)
		if err != nil {
			writeEmptyJSON(w, `{"arrivals":[]}`)
			return
		}
		defer resp.Body.Close() //nolint:errcheck
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			writeEmptyJSON(w, `{"arrivals":[]}`)
			return
		}
		copyUpstreamResponse(w, resp)
	}
}
