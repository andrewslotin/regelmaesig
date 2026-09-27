package main

import (
	"strings"
	"testing"
	"time"
)

// TestRun_InvalidListenAddressReturnsError verifies that a listener/startup
// failure surfaces as a non-nil error from run(), so main() can os.Exit(1)
// after deferred tracing shutdown runs — rather than logging and exiting 0.
func TestRun_InvalidListenAddressReturnsError(t *testing.T) {
	clearOTELEnv(t)

	saved := config
	t.Cleanup(func() { config = saved })

	config.ListenAddr = "127.0.0.1:-1"
	config.Timeout = 100 * time.Millisecond
	config.StaticCacheSize = 0
	config.DynamicCacheSize = 0

	done := make(chan error, 1)
	go func() { done <- run() }()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("run() returned nil, want error for invalid listen address")
		}
		if !strings.Contains(err.Error(), "server") {
			t.Fatalf("run() error = %q, want it wrapped with %q", err.Error(), "server")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run() did not return within 5s for invalid listen address")
	}
}
