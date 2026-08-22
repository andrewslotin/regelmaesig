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
| `mux.go` | `newMux(upstreamURL, timeout, staticCap, dynamicCap, metrics, hafas)` — registers all routes (including `GET /metrics`), builds provider chains with optional HAFAS fallback, injectable for tests |
| `proxy.go` | `DataProvider` func type, `TransportRESTClient` (wraps transport.rest upstream), shared helpers: `forward`, `copyUpstreamResponse`, `writeEmptyJSON`, `newStandardHandler` (accepts `[]DataProvider`), `newPassthroughHandler`, `serveFallback` |
| `metrics.go` | `Metrics` struct + `NewMetrics(reg)` — four Prometheus counters/histogram with `upstream` label on three vectors; `errorReason` classifies network errors; `routePath` extracts low-cardinality path labels from `r.Pattern` |
| `hafas.go` | `HAFASClient` — HAFAS mgate.exe client with `DataProvider` methods (`Departures`, `Arrivals`, `Locations`, `Nearby`, `Stop`); HAFAS request/response structs; helpers (`parseHAFASTime`, `hafasProductName`, `hafasProductsMap`) |
| `hafas_translate.go` | Translates HAFAS responses to transport.rest-compatible JSON: `translateDepartures`, `translateArrivals`, `translateLocations`, `translateNearbyLocations`, `translateStop`; REST response structs |
| `handle_<resource>.go` | One file per resource; each handler accepts `[]DataProvider` and delegates to `newStandardHandler` |
| `testhelpers_test.go` | `newTestStack`, `newUnreachableStack`, `respondWith`, `respondSlow` |

## Memory

Project memory lives at `.claude/memory/` in this repository. Read and write all memory files there — this overrides the default global memory path.

## Adding a New Endpoint

1. Add a handler function in the relevant `handle_<resource>.go` (or create a new file); accept `providers []DataProvider` and pass it to `newStandardHandler` (or `newPassthroughHandler` for no-cache handlers that only use transport.rest)
2. Register the route in `newMux()` in `mux.go`; use `restOnly` for transport.rest-only routes, or build a provider list with HAFAS fallback if supported
3. Add tests in `handle_<resource>_test.go` covering: success, upstream error, network error, timeout
4. HAFAS fallback: add a `DataProvider` method on `HAFASClient` in `hafas.go` and a translation function in `hafas_translate.go`; wire it into the provider list in `mux.go`
