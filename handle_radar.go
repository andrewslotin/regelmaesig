package main

import (
	"net/http"
)

func handleRadar(client *http.Client, upstream string, metrics *Metrics) http.HandlerFunc {
	return newPassthroughHandler(client, upstream, `{"movements":[]}`, metrics)
}
