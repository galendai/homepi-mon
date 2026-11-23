package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreviewIsOfflineAndDoesNotRequireKioskConfiguration(t *testing.T) {
	for _, key := range []string{"HOMEPI_NODE_URL", "HOMEPI_DEVICE_ID", "HOMEPI_SOURCE_NODE_ID", "HOMEPI_DEVICE_TOKEN"} {
		t.Setenv(key, "")
	}
	path := filepath.Join(t.TempDir(), "snapshot.json")
	if err := os.WriteFile(path, []byte(`{"schema_version":"1.1","source_epoch":"preview-test","source_node":"dev-mac","source_node_label":"DEV-MAC","generated_at":"2026-08-10T14:32:00Z","metrics":[],"connector_health":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	args := []string{"--snapshot", path, "--style", "ascii", "--at", "2026-08-10T14:32:00Z"}
	if err := preview(args, &output); err != nil {
		t.Fatal(err)
	}
	frame := output.String()
	if !strings.Contains(frame, "OVERVIEW") || !strings.Contains(frame, "OFFLINE") || strings.Contains(frame, "\x1b") {
		t.Fatalf("unexpected offline preview:\n%s", frame)
	}
	if len(strings.Split(strings.TrimSuffix(frame, "\n"), "\n")) != 20 {
		t.Fatal("preview does not emit exactly one 20-row frame")
	}
	output.Reset()
	if err := preview(append(args, "--layout", "current"), &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "CODING PLANS") || strings.Contains(output.String(), "OVERVIEW") {
		t.Fatal("current layout does not preserve the CODING page")
	}
	output.Reset()
	if err := preview(append(args, "--connected"), &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "LIVE") || strings.Contains(output.String(), "OFFLINE") {
		t.Fatal("explicit connected replay did not change the connection state")
	}
}

func TestPreviewRejectsInvalidParametersAndSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invalid.json")
	if err := os.WriteFile(path, []byte(`{"schema_version":"99.0"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, args := range map[string][]string{
		"missing": nil,
		"layout":  {"--snapshot", path, "--layout", "unknown"},
		"style":   {"--snapshot", path, "--style", "unknown"},
		"time":    {"--snapshot", path, "--at", "yesterday"},
		"schema":  {"--snapshot", path},
	} {
		t.Run(name, func(t *testing.T) {
			var output bytes.Buffer
			if err := preview(args, &output); err == nil || output.Len() != 0 {
				t.Fatalf("invalid input produced a frame: error=%v", err)
			}
		})
	}
}
