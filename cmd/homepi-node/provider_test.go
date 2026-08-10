package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/galendai/homepi-mon/internal/config"
)

func TestProviderEditParsesAllFieldsInOneFlagSet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := validCommandConfig()
	cfg.Providers = []config.ProviderConfig{{
		ID: "mock-demo", Type: "mock", AccountLabel: "demo", Region: "global",
		Interval: "60s", StaleAfter: "5m", Enabled: boolPtr(true),
		MockFixture: "fixture.json",
	}}
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	if err := providerEdit([]string{
		"-config", path, "-id", "mock-demo", "-interval", "10s", "-enabled", "false",
	}); err != nil {
		t.Fatal(err)
	}
	got, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Providers[0].Interval != "10s" || got.Providers[0].IsEnabled() {
		t.Fatalf("provider was not edited: %+v", got.Providers[0])
	}
}

func TestProviderAddDoesNotAcceptSecretArgv(t *testing.T) {
	if err := providerAdd([]string{"-secret", "plaintext"}); err == nil {
		t.Fatal("provider add accepted a plaintext -secret argv flag")
	}
}

func TestReadProviderSecretUsesEnvironmentOrStdin(t *testing.T) {
	t.Setenv("HOMEPI_PROVIDER_SECRET", "from-env")
	got, err := readProviderSecret(false, bytes.NewBufferString("ignored"))
	if err != nil || got != "from-env" {
		t.Fatalf("environment secret = %q, err=%v", got, err)
	}

	t.Setenv("HOMEPI_PROVIDER_SECRET", "")
	got, err = readProviderSecret(true, bytes.NewBufferString("from-stdin\n"))
	if err != nil || got != "from-stdin" {
		t.Fatalf("stdin secret = %q, err=%v", got, err)
	}
}

func TestExampleConfigStartsWithoutPlaceholderDevice(t *testing.T) {
	var cfg config.Config
	if err := json.Unmarshal([]byte(exampleConfig()), &cfg); err != nil {
		t.Fatal(err)
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Devices) != 0 {
		t.Fatalf("example config contains placeholder devices: %+v", cfg.Devices)
	}
	if len(cfg.Providers) != 0 {
		t.Fatalf("example config contains placeholder providers: %+v", cfg.Providers)
	}
}
