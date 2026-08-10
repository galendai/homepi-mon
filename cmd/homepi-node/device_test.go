package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/galendai/homepi-mon/internal/config"
	"github.com/galendai/homepi-mon/internal/deviceacl"
	"github.com/galendai/homepi-mon/internal/secretstore"
)

func TestRevokeDeviceRemovesConfigAndKeepsDurableRevocation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	ref := "keyring:device-token:pi-kiosk"
	token := "device-token-0123456789"
	cfg := validCommandConfig()
	cfg.Devices = []config.DeviceConfig{{ID: "pi-kiosk", TokenRef: ref}}
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	store := memoryStore{ref: token}
	if err := revokeDevice(path, "pi-kiosk", dir, store); err != nil {
		t.Fatal(err)
	}
	got, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Devices) != 0 {
		t.Fatalf("revoked device remains configured: %+v", got.Devices)
	}
	acl, err := deviceacl.Load(filepath.Join(dir, "revoked.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !acl.IsRevoked(token) {
		t.Fatal("revoked token hash was not persisted")
	}
	if _, err := store.Get(context.Background(), ref); err != secretstore.ErrNotFound {
		t.Fatalf("secret was not deleted: %v", err)
	}
}

func TestInitialConfigAcceptsFirstProviderAndDevice(t *testing.T) {
	t.Setenv("HOMEPI_PROVIDER_SECRET", "")
	path := filepath.Join(t.TempDir(), "config.json")
	if err := configInit([]string{"-out", path}); err != nil {
		t.Fatal(err)
	}
	ref := "keyring:device-token:pi-kiosk"
	store := memoryStore{}
	if err := addDevice(path, "pi-kiosk", ref, "device-token-0123456789", store); err != nil {
		t.Fatal(err)
	}
	if err := providerAdd([]string{
		"-config", path,
		"-id", "mock-demo",
		"-type", "mock",
		"-account-label", "demo",
		"-mock-fixture", "examples/mock-fixture.json",
	}); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Devices) != 1 || len(cfg.Providers) != 1 {
		t.Fatalf("first-run config = %+v", cfg)
	}
}
