package main

import (
	"net/http"
)

func handleLines(client *http.Client, upstream string, cache *Cache) http.HandlerFunc {
	return newStandardHandler(client, upstream, `[]`, cache, nil)
}

func handleLine(client *http.Client, upstream string, cache *Cache) http.HandlerFunc {
	return newStandardHandler(client, upstream, `{}`, cache, nil)
}
