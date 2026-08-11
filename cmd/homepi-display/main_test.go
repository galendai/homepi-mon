package main

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/galendai/homepi-mon/internal/ui"
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

func TestKioskFlagsUseEnvironmentWithoutTokenArgv(t *testing.T) {
	t.Setenv("HOMEPI_NODE_URL", "https://node.lan:8443")
	t.Setenv("HOMEPI_DEVICE_ID", "pi-kiosk")
	t.Setenv("HOMEPI_SOURCE_NODE_ID", "dev-mac")
	t.Setenv("HOMEPI_NODE_CERT_PIN", "sha256:test")
	t.Setenv("HOMEPI_DEVICE_TOKEN", "device-token")
	t.Setenv("HOMEPI_DISPLAY_DATA_DIR", "/var/lib/homepi-display")
	t.Setenv("HOMEPI_DISPLAY_STYLE", "ascii")
	f, err := parseKioskFlags("run", nil)
	if err != nil {
		t.Fatal(err)
	}
	if f.baseURL != "https://node.lan:8443" || f.deviceID != "pi-kiosk" ||
		f.nodeID != "dev-mac" || f.token != "device-token" ||
		f.dataDir != "/var/lib/homepi-display" || f.style != ui.StyleASCII {
		t.Fatalf("environment flags = %+v", f)
	}
}

func TestKioskFlagsDefaultToRichAndRejectUnknownStyle(t *testing.T) {
	for key, value := range map[string]string{
		"HOMEPI_NODE_URL":       "https://node.lan:8443",
		"HOMEPI_DEVICE_ID":      "pi-kiosk",
		"HOMEPI_SOURCE_NODE_ID": "dev-mac",
		"HOMEPI_DEVICE_TOKEN":   "device-token",
	} {
		t.Setenv(key, value)
	}
	t.Setenv("HOMEPI_DISPLAY_STYLE", "")
	f, err := parseKioskFlags("run", nil)
	if err != nil {
		t.Fatal(err)
	}
	if f.style != ui.StyleRich {
		t.Fatalf("default style = %q, want rich", f.style)
	}
	if _, err := parseKioskFlags("run", []string{"-style", "rainbow"}); err == nil ||
		!strings.Contains(err.Error(), "want rich or ascii") {
		t.Fatalf("unknown style error = %v", err)
	}
}

func TestSmallTerminalDiagnosticFitsAndUsesASCII(t *testing.T) {
	frame := sizeDiagnostic(32, 6)
	lines := strings.Split(frame, "\n")
	if len(lines) != 6 {
		t.Fatalf("rows = %d", len(lines))
	}
	for _, line := range lines {
		if len(line) != 32 {
			t.Fatalf("line width = %d: %q", len(line), line)
		}
		for _, b := range []byte(line) {
			if b < 0x20 || b > 0x7e {
				t.Fatalf("non-ASCII byte 0x%x", b)
			}
		}
	}
}

func TestScreenIsOutputOnlyAndRestoresCursor(t *testing.T) {
	var out bytes.Buffer
	s := newScreen(&out, ui.StyleRich)
	s.enter()
	frame := strings.Repeat(" ", ui.Cols)
	s.drawRaw(frame)
	beforeDuplicate := out.Len()
	s.drawRaw(frame)
	if out.Len() != beforeDuplicate {
		t.Fatal("identical frame was redrawn")
	}
	s.restore()
	got := out.String()
	if !strings.Contains(got, seqHideCursor) ||
		!strings.HasSuffix(got, seqResetStyle+seqShowCursor+seqLeaveAlt) {
		t.Fatalf("screen lifecycle output = %q", got)
	}
	for _, mouseMode := range []string{"\x1b[?1000h", "\x1b[?1002h", "\x1b[?1006h"} {
		if strings.Contains(got, mouseMode) {
			t.Fatalf("screen enabled mouse mode %q", mouseMode)
		}
	}
}

func TestScreenDrawUsesSelectedStyle(t *testing.T) {
	vm := ui.ViewModel{}
	var richOut bytes.Buffer
	newScreen(&richOut, ui.StyleRich).draw(vm)
	if got := richOut.String(); !strings.Contains(got, "┌") ||
		!strings.Contains(got, "\x1b[1;36m") {
		t.Fatalf("rich screen missing glyph or colour: %q", got)
	}

	var asciiOut bytes.Buffer
	newScreen(&asciiOut, ui.StyleASCII).draw(vm)
	if got := asciiOut.String(); strings.Contains(got, "┌") ||
		strings.Contains(got, "\x1b[1;") {
		t.Fatalf("ASCII screen contains rich decoration: %q", got)
	}
}
