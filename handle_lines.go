package main

import (
	"net/http"
)

func handleLines(providers []DataProvider, cache *Cache, metrics *Metrics) http.HandlerFunc {
	return newStandardHandler(providers, `[]`, cache, nil, metrics)
}

func handleLine(providers []DataProvider, cache *Cache, metrics *Metrics) http.HandlerFunc {
	return newStandardHandler(providers, `{}`, cache, nil, metrics)
}
