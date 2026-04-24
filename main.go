package main

import (
	"flag"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"
)

const (
	DefaultListenAddr       = ":8080"
	DefaultTimeout          = 10 * time.Second
	DefaultStaticCacheSize  = 512
	DefaultDynamicCacheSize = 2048
	upstreamURL             = "https://v6.vbb.transport.rest"
)

var config struct {
	ListenAddr       string
	Timeout          time.Duration
	StaticCacheSize  int
	DynamicCacheSize int
}

func main() {
	flag.StringVar(&config.ListenAddr, "l", os.Getenv("VBB_LISTEN_ADDR"), "Server listen address")
	flag.DurationVar(&config.Timeout, "t", envDuration("VBB_TIMEOUT", DefaultTimeout), "Upstream request timeout")
	flag.IntVar(&config.StaticCacheSize, "static-cache-size", envInt("VBB_STATIC_CACHE_SIZE", DefaultStaticCacheSize), "LRU capacity for static-route cache (0 disables)")
	flag.IntVar(&config.DynamicCacheSize, "dynamic-cache-size", envInt("VBB_DYNAMIC_CACHE_SIZE", DefaultDynamicCacheSize), "LRU capacity for dynamic-route cache (0 disables)")
	flag.Parse()

	if config.ListenAddr == "" {
		config.ListenAddr = DefaultListenAddr
	}

	mux := newMux(upstreamURL, config.Timeout, config.StaticCacheSize, config.DynamicCacheSize)

	slog.Info("starting server", "listenAddr", config.ListenAddr, "timeout", config.Timeout,
		"staticCacheSize", config.StaticCacheSize, "dynamicCacheSize", config.DynamicCacheSize)
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

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
