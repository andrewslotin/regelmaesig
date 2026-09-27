package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
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

	if err := run(); err != nil {
		slog.Error("fatal error", "error", err)
		os.Exit(1)
	}
}

// run wires up tracing, HTTP server, and signal-driven shutdown. It returns a
// non-nil error only when the process should exit non-zero; in all cases its
// deferred tracing shutdown runs before returning so buffered spans are flushed.
func run() error {
	tracingShutdown, err := setupTracing(context.Background())
	if err != nil {
		return fmt.Errorf("set up tracing: %w", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := tracingShutdown(ctx); err != nil {
			slog.Error("failed to shut down tracing", "error", err)
		}
	}()

	reg := prometheus.NewRegistry()
	metrics := NewMetrics(reg)

	var hafas *HAFASClient
	if endpoint, aid := os.Getenv("HAFAS_ENDPOINT"), os.Getenv("HAFAS_AUTH_AID"); endpoint != "" && aid != "" {
		version := os.Getenv("HAFAS_VERSION")
		if version == "" {
			version = DefaultHAFASVersion
		}
		hafasClient := instrumentedHTTPClient(config.Timeout)
		hafas = NewHAFASClient(hafasClient, endpoint, aid, version)
		slog.Info("HAFAS fallback enabled", "endpoint", endpoint)
	} else {
		slog.Info("HAFAS fallback disabled (set HAFAS_ENDPOINT and HAFAS_AUTH_AID to enable)")
	}

	mux := newMux(upstreamURL, config.Timeout, config.StaticCacheSize, config.DynamicCacheSize, metrics, hafas)

	srv := &http.Server{
		Addr:    config.ListenAddr,
		Handler: mux,
	}

	serveErr := make(chan error, 1)
	go func() {
		slog.Info("starting server", "listenAddr", config.ListenAddr, "timeout", config.Timeout,
			"staticCacheSize", config.StaticCacheSize, "dynamicCacheSize", config.DynamicCacheSize,
			"hafas", hafas != nil)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	select {
	case <-ctx.Done():
		slog.Info("shutdown signal received, stopping server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("server shutdown: %w", err)
		}
		return nil
	case err := <-serveErr:
		if err != nil {
			return fmt.Errorf("server: %w", err)
		}
		return nil
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
