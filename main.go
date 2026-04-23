package main

import (
	"flag"
	"net/http"
	"os"

	"log/slog"
)

const DefaultListenAddr = ":8080"

var config struct {
	ListenAddr string
}

func main() {
	flag.StringVar(&config.ListenAddr, "l", os.Getenv("VBB_LISTEN_ADDR"), "Server listen address")
	flag.Parse()

	if config.ListenAddr == "" {
		config.ListenAddr = DefaultListenAddr
	}

	slog.Info("starting server", "listenAddr", config.ListenAddr)
	if err := http.ListenAndServe(config.ListenAddr, nil); err != nil {
		slog.Error("failed to start server", "error", err)
		os.Exit(1)
	}
}
