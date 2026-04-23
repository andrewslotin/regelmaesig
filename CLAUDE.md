# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Is

A reliability proxy for the [VBB API v6](https://v6.vbb.transport.rest/api.html) (`github.com/andrewslotin/regelmaesig`). The VBB public transit API is unreliable; this service sits in front of it so polling clients (e.g. TRMNL) don't get put into degraded mode.

## Commands

```bash
# Build
go build ./...

# Run
go run main.go

# Install
go install github.com/andrewslotin/regelmaesig

# Test
go test ./...

# Single package test
go test ./path/to/pkg

# Single test
go test ./... -run TestName
```
