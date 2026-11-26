package main

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/galendai/homepi-mon/internal/protocol"
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

func TestKioskFlagsHomeOnly(t *testing.T) {
	for key, value := range map[string]string{
		"HOMEPI_NODE_URL":       "https://node.lan:8443",
		"HOMEPI_DEVICE_ID":      "pi-kiosk",
		"HOMEPI_SOURCE_NODE_ID": "dev-mac",
		"HOMEPI_DEVICE_TOKEN":   "device-token",
		"HOMEPI_HOME_ONLY":      "true",
	} {
		t.Setenv(key, value)
	}
	f, err := parseKioskFlags("run", nil)
	if err != nil || !f.homeOnly {
		t.Fatalf("home-only flags = %+v, err = %v", f, err)
	}
	t.Setenv("HOMEPI_HOME_ONLY", "sometimes")
	if _, err := parseKioskFlags("run", nil); err == nil {
		t.Fatal("invalid home-only value was accepted")
	}
}

func TestKioskFlagsPageOnly(t *testing.T) {
	for key, value := range map[string]string{
		"HOMEPI_NODE_URL":       "https://node.lan:8443",
		"HOMEPI_DEVICE_ID":      "pi-kiosk",
		"HOMEPI_SOURCE_NODE_ID": "dev-mac",
		"HOMEPI_DEVICE_TOKEN":   "device-token",
		"HOMEPI_HOME_ONLY":      "false",
	} {
		t.Setenv(key, value)
	}
	f, err := parseKioskFlags("run", []string{"-page-only", "coding"})
	if err != nil || f.pageOnly != ui.PageCoding || f.homeOnly {
		t.Fatalf("page-only flags = %+v, err = %v", f, err)
	}
	if _, err := parseKioskFlags("run", []string{"-page-only", "unknown"}); err == nil {
		t.Fatal("unknown page-only value was accepted")
	}
	t.Setenv("HOMEPI_HOME_ONLY", "true")
	if _, err := parseKioskFlags("run", []string{"-page-only", "CODING"}); err == nil {
		t.Fatal("home-only and page-only conflict was accepted")
	}
}

func TestKioskFlagsSelectTwoPages(t *testing.T) {
	for key, value := range map[string]string{
		"HOMEPI_NODE_URL":       "https://node.lan:8443",
		"HOMEPI_DEVICE_ID":      "pi-kiosk",
		"HOMEPI_SOURCE_NODE_ID": "dev-mac",
		"HOMEPI_DEVICE_TOKEN":   "device-token",
		"HOMEPI_HOME_ONLY":      "false",
	} {
		t.Setenv(key, value)
	}
	f, err := parseKioskFlags("run", []string{"-pages", "CODING,API"})
	if err != nil || !f.customPages || len(f.rotation.Order) != 2 ||
		f.rotation.Order[0] != ui.PageCoding || f.rotation.Order[1] != ui.PageAPI ||
		f.rotation.Dwell[ui.PageCoding] != 15*time.Second {
		t.Fatalf("selected pages = %+v, err = %v", f, err)
	}
	three, err := parseKioskFlags("run", []string{"-pages", "HOME,CODING,API"})
	if err != nil || len(three.rotation.Order) != 3 || three.rotation.Order[0] != ui.PageHome ||
		three.rotation.Dwell[ui.PageHome] != ui.HomeDwell {
		t.Fatalf("three selected pages = %+v, err = %v", three, err)
	}
	for _, args := range [][]string{
		{"-pages", "CODING,CODING"}, {"-pages", "UNKNOWN"},
		{"-pages", "CODING,API", "-page-only", "API"},
	} {
		if _, err := parseKioskFlags("run", args); err == nil {
			t.Fatalf("invalid page selection was accepted: %v", args)
		}
	}
	t.Setenv("HOMEPI_HOME_ONLY", "true")
	if _, err := parseKioskFlags("run", []string{"-pages", "CODING,API"}); err == nil {
		t.Fatal("home-only and pages conflict was accepted")
	}
}

func TestKioskFlagsOpenClashRuntimePages(t *testing.T) {
	for key, value := range map[string]string{
		"HOMEPI_NODE_URL":       "https://node.lan:8443",
		"HOMEPI_DEVICE_ID":      "pi-kiosk",
		"HOMEPI_SOURCE_NODE_ID": "dev-mac",
		"HOMEPI_DEVICE_TOKEN":   "device-token",
		"HOMEPI_HOME_ONLY":      "false",
	} {
		t.Setenv(key, value)
	}
	f, err := parseKioskFlags("run", []string{"-pages", "HOME,CODING,API,OPENCLASH"})
	if err != nil || len(f.rotation.Order) != 4 || f.rotation.Order[3] != ui.PageOpenClash ||
		f.rotation.Dwell[ui.PageOpenClash] != 15*time.Second {
		t.Fatalf("four-page rotation = %+v, err = %v", f.rotation, err)
	}
	only, err := parseKioskFlags("run", []string{"-page-only", "openclash"})
	if err != nil || !only.customPages || only.pageOnly != ui.PageOpenClash ||
		len(only.rotation.Order) != 1 || only.rotation.Dwell[ui.PageOpenClash] != 15*time.Second {
		t.Fatalf("OpenClash page-only = %+v, err = %v", only, err)
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

func TestScreenDrawUsesCompactOverviewAndSkipsUnchangedTicks(t *testing.T) {
	for _, style := range []ui.Style{ui.StyleASCII, ui.StyleRich} {
		t.Run(string(style), func(t *testing.T) {
			percent, age := 42, 33*time.Second
			vm := ui.ViewModel{
				Page: string(ui.PageCoding), NodeLabel: "DEV-MAC", Link: protocol.LinkLive,
				Now: time.Date(2026, 10, 10, 9, 30, 10, 0, time.Local), HasSnapshot: true,
				Coding:     []ui.Card{{Name: "Codex", PercentLeft: &percent, Status: protocol.DisplayOK}},
				API:        []ui.Card{{Name: "DeepSeek API", Amount: "CNY 8.20", Status: protocol.DisplayOK}},
				PageNumber: 1, PageCount: 5, DwellSeconds: 15, SyncedAgo: &age,
			}
			var out bytes.Buffer
			s := newScreen(&out, style)
			s.draw(vm)
			if !strings.Contains(out.String(), "OVERVIEW") || !strings.Contains(out.String(), "CNY 8.20") {
				t.Fatal("production screen did not select the complete compact overview")
			}
			beforeTick := out.Len()
			vm.Now = vm.Now.Add(time.Second)
			age += time.Second
			s.draw(vm)
			if out.Len() != beforeTick {
				t.Fatal("unchanged paused overview was rewritten on a timer tick")
			}
			vm.API[0].Amount = "CNY 8.19"
			s.draw(vm)
			if out.Len() == beforeTick || !strings.Contains(s.last, "CNY 8.19") {
				t.Fatal("a changed balance did not reach the terminal")
			}
		})
	}
}
