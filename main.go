package main

import (
	"flag"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	DefaultListenAddr       = ":8080"
	DefaultTimeout          = 10 * time.Second
	DefaultStaticCacheSize  = 512
	DefaultDynamicCacheSize = 2048
	DefaultHAFASVersion     = "1.45"
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

	reg := prometheus.NewRegistry()
	metrics := NewMetrics(reg)

	var hafas *HAFASClient
	if endpoint, aid := os.Getenv("HAFAS_ENDPOINT"), os.Getenv("HAFAS_AUTH_AID"); endpoint != "" && aid != "" {
		version := os.Getenv("HAFAS_VERSION")
		if version == "" {
			version = DefaultHAFASVersion
		}
		hafasClient := &http.Client{Timeout: config.Timeout}
		hafas = NewHAFASClient(hafasClient, endpoint, aid, version)
		slog.Info("HAFAS fallback enabled", "endpoint", endpoint)
	} else {
		slog.Info("HAFAS fallback disabled (set HAFAS_ENDPOINT and HAFAS_AUTH_AID to enable)")
	}

	mux := newMux(upstreamURL, config.Timeout, config.StaticCacheSize, config.DynamicCacheSize, metrics, hafas)

	slog.Info("starting server", "listenAddr", config.ListenAddr, "timeout", config.Timeout,
		"staticCacheSize", config.StaticCacheSize, "dynamicCacheSize", config.DynamicCacheSize,
		"hafas", hafas != nil)
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
