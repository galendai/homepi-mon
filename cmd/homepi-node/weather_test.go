package main

import (
	"path/filepath"
	"testing"

	"github.com/galendai/homepi-mon/internal/config"
	"github.com/galendai/homepi-mon/internal/connector"
	"github.com/galendai/homepi-mon/internal/providermeta"
)

func TestProviderAddOpenMeteoUsesBeijingDefaultsWithoutSecret(t *testing.T) {
	t.Setenv("HOMEPI_PROVIDER_SECRET", "")
	path := filepath.Join(t.TempDir(), "config.json")
	if err := validCommandConfig().Save(path); err != nil {
		t.Fatal(err)
	}
	if err := providerAdd([]string{
		"-config", path, "-id", "beijing-weather", "-type", "openmeteo",
		"-account-label", "beijing", "-interval", "10m", "-stale-after", "30m",
	}); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	provider := cfg.Providers[0]
	if provider.SecretRef != "" || provider.AuthFile != "" || len(provider.Options) != 0 {
		t.Fatalf("Open-Meteo credentials/options = %+v", provider)
	}
	if !connector.HasType("openmeteo") {
		t.Fatal("Open-Meteo connector is not registered")
	}
	meta, ok := providermeta.Lookup("openmeteo")
	if !ok || meta.RequiresSecret || meta.DefaultRegion != "global" {
		t.Fatalf("Open-Meteo metadata = %+v, ok=%v", meta, ok)
	}
}
