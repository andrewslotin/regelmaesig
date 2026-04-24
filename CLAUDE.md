# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Is

A reliability proxy for the [VBB API v6](https://v6.vbb.transport.rest/api.html) (`github.com/andrewslotin/regelmaesig`). The VBB public transit API is unreliable; this service sits in front of it so polling clients (e.g. TRMNL) don't get put into degraded mode.

## Commands

```bash
# Build
go build ./...

# Run (default: listen on :8080, 10s upstream timeout)
go run main.go [-l <addr>] [-t <duration>]

# Install
go install github.com/andrewslotin/regelmaesig

# Lint
golangci-lint run

# Test
go test ./...

# Single test
go test ./... -run TestName
```

## Architecture

| File | Purpose |
|---|---|
| `main.go` | Config flags (`-l` listen addr, `-t` timeout), server startup |
| `mux.go` | `newMux(upstreamURL, timeout)` — registers all routes, injectable for tests |
| `proxy.go` | Shared helpers: `forward`, `copyUpstreamResponse`, `writeEmptyJSON` |
| `handle_<resource>.go` | One file per resource; each handler forwards to upstream or returns a typed empty response |
| `testhelpers_test.go` | `newTestStack`, `newUnreachableStack`, `respondWith`, `respondSlow` |

## Adding a New Endpoint

1. Add a handler function in the relevant `handle_<resource>.go` (or create a new file)
2. Register the route in `newMux()` in `mux.go`
3. Add tests in `handle_<resource>_test.go` covering: success, upstream error, network error, timeout
