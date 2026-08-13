package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/galendai/homepi-mon/internal/protocol"
	"github.com/galendai/homepi-mon/internal/secretstore"
)

func TestParseRemoteActions(t *testing.T) {
	tests := []struct {
		action string
		args   []string
		kind   protocol.CommandKind
	}{
		{"show-page", []string{"--duration", "20s", "API"}, protocol.CommandShowPage},
		{"next-page", []string{"--duration", "20s"}, protocol.CommandNextPage},
		{"previous-page", nil, protocol.CommandPreviousPage},
		{"set-rotation", []string{"--interval", "10s", "--duration", "60s", "on"}, protocol.CommandSetRotation},
		{"refresh", []string{"mock", "prometheus-main"}, protocol.CommandRefreshData},
		{"show-message", []string{"--severity", "warning", "--duration", "10s", "maintenance", "soon"}, protocol.CommandShowMessage},
		{"set-brightness", []string{"50"}, protocol.CommandSetBrightness},
	}
	for _, test := range tests {
		t.Run(test.action, func(t *testing.T) {
			request, err := parseRemoteAction(test.action, test.args)
			if err != nil {
				t.Fatal(err)
			}
			if request.Kind != test.kind {
				t.Fatalf("kind=%s", request.Kind)
			}
		})
	}
}

func TestParseRemoteActionsRejectsUnsafeInputs(t *testing.T) {
	tests := []struct {
		action string
		args   []string
	}{
		{"show-page", []string{"OTHER"}},
		{"show-message", []string{"bad\x1b[2J"}},
		{"set-brightness", []string{"101"}},
		{"refresh", []string{"bad/id"}},
		{"next-page", []string{"--ttl", "301s"}},
		{"refresh", []string{"--duration", "30s", "mock"}},
		{"set-brightness", []string{"--severity", "warning", "50"}},
		{"exec-shell", []string{"rm", "-rf"}},
	}
	for _, test := range tests {
		if _, err := parseRemoteAction(test.action, test.args); err == nil {
			t.Errorf("accepted %s %q", test.action, test.args)
		}
	}
}

func TestLocalControlURLUsesLoopbackForWildcard(t *testing.T) {
	got, err := localControlURL("0.0.0.0:8443", false)
	if err != nil || got != "https://127.0.0.1:8443" {
		t.Fatalf("got=%q err=%v", got, err)
	}
	if _, err := localControlURL("192.0.2.1:8443", true); err == nil {
		t.Fatal("insecure non-loopback control URL was accepted")
	}
}

func TestEnsureControlTokenGeneratesAndReusesDedicatedCredential(t *testing.T) {
	store, err := secretstore.NewFileBackend(filepath.Join(t.TempDir(), "secrets"))
	if err != nil {
		t.Fatal(err)
	}
	first, err := ensureControlToken(context.Background(), store)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ensureControlToken(context.Background(), store)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 64 || first != second {
		t.Fatalf("credential length=%d stable=%v", len(first), first == second)
	}
	stored, err := store.Get(context.Background(), controlTokenRef)
	if err != nil || stored != first {
		t.Fatalf("stored credential mismatch: %v", err)
	}
}
