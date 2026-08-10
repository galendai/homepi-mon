package main

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestResolveHTTPClientPreservesTimeouts(t *testing.T) {
	parts := make([]string, 32)
	for i := range parts {
		parts[i] = "00"
	}
	client, err := resolveHTTPClient("https://127.0.0.1:8443", "sha256:"+strings.Join(parts, ":"), false)
	if err != nil {
		t.Fatal(err)
	}
	if client.Timeout != 30*time.Second {
		t.Fatalf("client timeout = %v, want 30s", client.Timeout)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T", client.Transport)
	}
	if transport.DialContext == nil || transport.TLSHandshakeTimeout <= 0 ||
		transport.ResponseHeaderTimeout <= 0 {
		t.Fatalf("transport timeouts are incomplete: %+v", transport)
	}
}
