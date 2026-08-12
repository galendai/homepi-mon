package displayconfig

import (
	"strings"
	"testing"
)

const testPin = "sha256:BF:D2:93:64:1C:0E:CA:36:16:61:4E:84:FE:E0:71:84:C6:D1:17:4D:56:79:96:A9:0A:01:C5:27:35:54:06:02"

func validEnvironment() Environment {
	return Environment{NodeURL: "https://192.168.31.107:8443", DeviceID: "pi-kiosk", SourceNode: "dev-mac", CertPin: testPin, DeviceToken: "device-token", DataDir: DefaultDataDir, Style: "rich"}
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
	if got.Style != "rich" || got.DeviceToken != "device-token" || len(strings.Split(strings.TrimSpace(string(raw)), "\n")) != 7 {
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
	tests := []Environment{validEnvironment(), validEnvironment(), validEnvironment(), validEnvironment()}
	tests[0].NodeURL = "http://192.168.1.2:8443"
	tests[1].CertPin = "invalid"
	tests[2].DataDir = "/tmp/display"
	tests[3].Style = "neon"
	for _, env := range tests {
		if err := env.Validate(); err == nil {
			t.Fatalf("accepted invalid environment %+v", env)
		}
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
