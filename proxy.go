package main

import (
	"io"
	"net/http"
)

// forward builds an upstream request from r, executes it with client, and returns the response.
// The caller is responsible for closing the response body.
func forward(client *http.Client, upstream string, r *http.Request) (*http.Response, error) {
	url := upstream + r.URL.RequestURI()
	req, err := http.NewRequestWithContext(r.Context(), r.Method, url, r.Body)
	if err != nil {
		return nil, err
	}
	req.Header = r.Header.Clone()
	return client.Do(req)
}

// copyUpstreamResponse writes the upstream response headers, status code, and body to w.
// The caller is responsible for closing resp.Body before or after calling this.
func copyUpstreamResponse(w http.ResponseWriter, resp *http.Response) {
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body) //nolint:errcheck
}

// writeEmptyJSON writes an HTTP 200 response with body as the JSON payload.
func writeEmptyJSON(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, body) //nolint:errcheck
}
