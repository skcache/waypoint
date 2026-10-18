package main

import (
	"strings"
	"testing"
)

// The fixture serves UNAUTHENTICATED mutation endpoints (checkout, capture).
// It must bind to loopback only — a wildcard or configurable host would be a
// public exposure path. This asserts the bind address stays on 127.0.0.1.
func TestListenAddrIsLoopbackOnly(t *testing.T) {
	if got := listenAddr(defaultPort); got != "127.0.0.1:8090" {
		t.Fatalf("listenAddr(%d) = %q, want %q", defaultPort, got, "127.0.0.1:8090")
	}
	if got := listenAddr(9101); got != "127.0.0.1:9101" {
		t.Fatalf("listenAddr(9101) = %q, want 127.0.0.1:9101", got)
	}
	got := listenAddr(defaultPort)
	if strings.HasPrefix(got, ":") || strings.Contains(got, "0.0.0.0") || strings.Contains(got, "::") {
		t.Fatalf("listen address %q must never expose a wildcard interface", got)
	}
}
