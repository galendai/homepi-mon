package displayconfig

import (
	"strings"
	"testing"
)

const testPin = "sha256:BF:D2:93:64:1C:0E:CA:36:16:61:4E:84:FE:E0:71:84:C6:D1:17:4D:56:79:96:A9:0A:01:C5:27:35:54:06:02"

func validEnvironment() Environment {
	return Environment{NodeURL: "https://192.168.31.107:8443", DeviceID: "pi-kiosk", SourceNode: "dev-mac", CertPin: testPin, DeviceToken: "device-token", DataDir: DefaultDataDir, Style: "rich", PageOrder: "CODING,API,HOMELAB,SERVICES,SYSTEM", PageDwellSeconds: "CODING:15,API:15,HOMELAB:15,SERVICES:15,SYSTEM:15", HomeOnly: "true"}
}

func TestEnvironmentRoundTrip(t *testing.T) {
	raw, err := validEnvironment().Render()
	if err != nil {
		t.Fatal(err)
	}
	got, err := Parse(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	if got.Style != "rich" || got.DeviceToken != "device-token" || got.HomeOnly != "true" || len(strings.Split(strings.TrimSpace(string(raw)), "\n")) != 10 {
		t.Fatalf("round trip = %+v", got)
	}
}

func TestEnvironmentRejectsUnsafeAndUnknownValues(t *testing.T) {
	tests := []string{
		"UNKNOWN=value\n",
		"HOMEPI_NODE_URL=https://example.test\nHOMEPI_NODE_URL=https://example.test\n",
		strings.ReplaceAll(mustRender(t), "pi-kiosk", "pi$(id)"),
		strings.ReplaceAll(mustRender(t), "rich", "rich\nBAD=value"),
	}
	for _, raw := range tests {
		if _, err := Parse(strings.NewReader(raw)); err == nil {
			t.Fatalf("accepted unsafe environment %q", raw)
		}
	}
}

func TestEnvironmentRejectsInvalidSemanticValues(t *testing.T) {
	tests := []Environment{validEnvironment(), validEnvironment(), validEnvironment(), validEnvironment(), validEnvironment(), validEnvironment(), validEnvironment()}
	tests[0].NodeURL = "http://192.168.1.2:8443"
	tests[1].CertPin = "invalid"
	tests[2].DataDir = "/tmp/display"
	tests[3].Style = "neon"
	tests[4].PageOrder = "CODING,API,HOMELAB,SERVICES,CODING"
	tests[5].PageDwellSeconds = "CODING:4,API:15,HOMELAB:15,SERVICES:15,SYSTEM:15"
	tests[6].HomeOnly = "sometimes"
	for _, env := range tests {
		if err := env.Validate(); err == nil {
			t.Fatalf("accepted invalid environment %+v", env)
		}
	}
}

func TestEnvironmentDefaultsHomeOnlyToFalse(t *testing.T) {
	env := validEnvironment()
	env.HomeOnly = ""
	if err := env.Validate(); err != nil {
		t.Fatal(err)
	}
	raw, err := env.Render()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "HOMEPI_HOME_ONLY=false\n") {
		t.Fatalf("missing home-only default:\n%s", raw)
	}
}

func mustRender(t *testing.T) string {
	t.Helper()
	raw, err := validEnvironment().Render()
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestOptionalStartupSettingsRoundTrip(t *testing.T) {
	for _, tc := range []struct{ subset, lock string }{
		{"API,HOME,OPENCLASH", ""}, {"", "OPENCLASH"}, {"", ""},
	} {
		env := validEnvironment()
		env.HomeOnly, env.PageSubset, env.PageLock = "false", tc.subset, tc.lock
		raw, err := env.Render()
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "HOMEPI_ROTATION_PAGES=") != (tc.subset != "") ||
			strings.Contains(string(raw), "HOMEPI_PAGE_LOCK=") != (tc.lock != "") {
			t.Fatal("unexpected optional keys")
		}
		got, err := Parse(strings.NewReader(string(raw)))
		if err != nil || got.PageSubset != tc.subset || got.PageLock != tc.lock {
			t.Fatalf("mode round trip: %v", err)
		}
	}
	for _, tc := range []struct{ home, subset, lock string }{
		{"false", "HOME,HOME", ""}, {"false", "HOME,", ""}, {"false", "BAD", ""}, {"false", "", "BAD"},
		{"true", "HOME,API", ""}, {"true", "", "HOME"}, {"false", "API", "API"},
	} {
		env := validEnvironment()
		env.HomeOnly, env.PageSubset, env.PageLock = tc.home, tc.subset, tc.lock
		if _, err := env.Render(); err == nil {
			t.Errorf("accepted invalid selection %+v", tc)
		}
	}
}
