package main

import (
	"net/http"
)

func handleMap(client *http.Client, upstream string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp, err := forward(client, upstream, r)
		if err != nil {
			http.Error(w, "bad gateway", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close() //nolint:errcheck //nolint:errcheck
		copyUpstreamResponse(w, resp)
	}
}
