package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/galendai/homepi-mon/internal/webadmin"
)

func TestParseBuild(t *testing.T) {
	raw := []byte("homepi-node 0.1.0\ncommit:   df90365\nbuilt:    2026-08-12T09:23:27Z\nplatform: darwin/arm64\ngo:       go1.26.5\n")
	got, err := parseBuild(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != "0.1.0" || got.Commit != "df90365" ||
		got.Built != "2026-08-12T09:23:27Z" || got.Platform != "darwin/arm64" {
		t.Fatalf("parsed build = %+v", got)
	}
}

func TestParseBuildRejectsUntrustedOutput(t *testing.T) {
	for _, raw := range [][]byte{
		[]byte("other-binary 0.1.0\ncommit: df90365\n"),
		[]byte("homepi-node 0.1.0\ncommit: bad\tvalue\n"),
		[]byte("homepi-node 0.1.0\ncommit: \n"),
	} {
		if _, err := parseBuild(raw); err == nil {
			t.Fatalf("parseBuild accepted %q", raw)
		}
	}
}

func TestInspectBuildUsesDirectVersionCommand(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "homepi-node")
	body := "#!/bin/sh\nprintf '%s\\n' 'homepi-node 0.1.0' 'commit: test123' 'built: 2026-08-12T00:00:00Z' 'platform: darwin/arm64'\n"
	if err := os.WriteFile(binary, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	got, err := inspectBuild(context.Background(), binary)
	if err != nil {
		t.Fatal(err)
	}
	if got.Commit != "test123" {
		t.Fatalf("commit = %q, want test123", got.Commit)
	}
}

func TestClassifyRuntime(t *testing.T) {
	admin := webadmin.BuildSnapshot{Version: "0.1.0", Commit: "new123", Built: "2026-08-12T09:39:36Z"}
	tests := []struct {
		name       string
		service    webadmin.BuildSnapshot
		sameBinary bool
		want       string
	}{
		{name: "synced", service: admin, sameBinary: true, want: "synced"},
		{name: "different binary", service: admin, sameBinary: false, want: "different_binary"},
		{name: "version mismatch", service: webadmin.BuildSnapshot{Version: "0.1.0", Commit: "old123"}, sameBinary: false, want: "version_mismatch"},
		{name: "rebuilt same commit", service: webadmin.BuildSnapshot{Version: "0.1.0", Commit: "new123", Built: "2026-08-12T09:23:27Z"}, sameBinary: true, want: "version_mismatch"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := webadmin.RuntimeSnapshot{Admin: admin, Service: tt.service, ServiceInstalled: true, ServiceRunning: true}
			if got := classifyRuntime(admin, tt.service, tt.sameBinary, base); got.State != tt.want {
				t.Fatalf("state = %q, want %q", got.State, tt.want)
			}
		})
	}
}
