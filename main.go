package main

import (
	"flag"
	"log/slog"
	"net/http"
	"os"
	"time"
)

const (
	DefaultListenAddr = ":8080"
	DefaultTimeout    = 10 * time.Second
	upstreamURL       = "https://v6.vbb.transport.rest"
)

var config struct {
	ListenAddr string
	Timeout    time.Duration
}

func main() {
	flag.StringVar(&config.ListenAddr, "l", os.Getenv("VBB_LISTEN_ADDR"), "Server listen address")
	flag.DurationVar(&config.Timeout, "t", envDuration("VBB_TIMEOUT", DefaultTimeout), "Upstream request timeout")
	flag.Parse()

	if config.ListenAddr == "" {
		config.ListenAddr = DefaultListenAddr
	}

	mux := newMux(upstreamURL, config.Timeout)

	slog.Info("starting server", "listenAddr", config.ListenAddr, "timeout", config.Timeout)
	if err := http.ListenAndServe(config.ListenAddr, mux); err != nil {
		slog.Error("failed to start server", "error", err)
		os.Exit(1)
	}
}

func envDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
