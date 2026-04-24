package main

import (
	"net/http"
)

func handleLines(client *http.Client, upstream string, cache *Cache, metrics *Metrics) http.HandlerFunc {
	return newStandardHandler(client, upstream, `[]`, cache, nil, metrics)
}

func handleLine(client *http.Client, upstream string, cache *Cache, metrics *Metrics) http.HandlerFunc {
	return newStandardHandler(client, upstream, `{}`, cache, nil, metrics)
}
