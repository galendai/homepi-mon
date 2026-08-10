package main

import (
	"context"
	"crypto/tls"
	"encoding/pem"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/galendai/homepi-mon/internal/config"
	"github.com/galendai/homepi-mon/internal/secretstore"
	"github.com/galendai/homepi-mon/internal/tlsconfig"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestLegacyServeConfigAcceptsFlagToken(t *testing.T) {
	t.Setenv("HOMEPI_NODE_CONFIG", filepath.Join(t.TempDir(), "missing.json"))
	f, err := parseServeFlags([]string{
		"-node-id", "dev-mac",
		"-device-id", "pi-kiosk",
		"-device-token", "legacy-token-0123456789",
		"-mock-fixture", "fixture.json",
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg, fromFile, err := loadServeConfig(f)
	if err != nil {
		t.Fatal(err)
	}
	if fromFile {
		t.Fatal("legacy flags unexpectedly loaded a file")
	}
	if err := validateServeConfig(cfg, true); err != nil {
		t.Fatalf("legacy config validation failed: %v", err)
	}
	devices, err := resolveDevices(cfg, memoryStore{}, f.deviceTok, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 || devices[0].Token != f.deviceTok {
		t.Fatalf("resolved devices = %+v", devices)
	}
}

func TestFileServeConfigAppliesExplicitOverrides(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := validCommandConfig()
	cfg.Providers = []config.ProviderConfig{{
		ID: "mock-demo", Type: "mock", AccountLabel: "demo", Region: "global",
		Interval: "60s", StaleAfter: "5m", MockFixture: "fixture.json",
	}}
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	f, err := parseServeFlags([]string{
		"-config", path,
		"-addr", "127.0.0.1:9443",
		"-node-id", "overridden",
		"-node-label", "OVERRIDDEN",
		"-interval", "10s",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, fromFile, err := loadServeConfig(f)
	if err != nil {
		t.Fatal(err)
	}
	if !fromFile || got.Listen.Addr != "127.0.0.1:9443" ||
		got.SourceNode.ID != "overridden" || got.SourceNode.Label != "OVERRIDDEN" ||
		got.Providers[0].Interval != "10s" {
		t.Fatalf("overrides not applied: %+v", got)
	}
}

func TestResolveTLSConfigLoadsExplicitPair(t *testing.T) {
	certDir := t.TempDir()
	certPEM, _, err := tlsconfig.EnsureCert(certDir, "explicit-cert", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	dataDir := t.TempDir()
	cfg := validCommandConfig()
	cfg.Listen.TLS = config.TLSConfig{
		Cert: filepath.Join(certDir, "cert.pem"),
		Key:  filepath.Join(certDir, "key.pem"),
	}
	tlsConf, err := resolveTLSConfig(cfg, dataDir, false, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(certPEM)
	if block == nil || len(tlsConf.Certificates) != 1 ||
		string(tlsConf.Certificates[0].Certificate[0]) != string(block.Bytes) {
		t.Fatal("resolveTLSConfig did not load the configured certificate")
	}
	if _, err := os.Stat(filepath.Join(dataDir, "cert.pem")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("explicit TLS unexpectedly generated another certificate: %v", err)
	}
}

func TestListenAndServeUsesTLSWhenConfigured(t *testing.T) {
	dataDir := t.TempDir()
	cfg := validCommandConfig()
	tlsConf, err := resolveTLSConfig(cfg, dataDir, false, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := probe.Addr().String()
	_ = probe.Close()

	srv := &http.Server{
		Addr: addr, TLSConfig: tlsConf,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
	}
	done := make(chan error, 1)
	go func() { done <- listenAndServe(srv) }()
	t.Cleanup(func() { _ = srv.Close() })

	dialer := &net.Dialer{Timeout: 100 * time.Millisecond}
	var conn *tls.Conn
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: true, // Test trusts the freshly generated local certificate.
		})
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("TLS handshake failed: %v", err)
	}
	_ = conn.Close()
	_ = srv.Close()
	select {
	case err := <-done:
		if !errors.Is(err, http.ErrServerClosed) {
			t.Fatalf("server returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("TLS server did not stop")
	}
}

type memoryStore map[string]string

func (m memoryStore) Get(_ context.Context, ref string) (string, error) {
	v, ok := m[ref]
	if !ok {
		return "", secretstore.ErrNotFound
	}
	return v, nil
}
func (m memoryStore) Set(_ context.Context, ref, value string) error {
	m[ref] = value
	return nil
}
func (m memoryStore) Delete(_ context.Context, ref string) error {
	if _, ok := m[ref]; !ok {
		return secretstore.ErrNotFound
	}
	delete(m, ref)
	return nil
}
func (m memoryStore) Backend() string { return "memory" }

func validCommandConfig() *config.Config {
	cfg := &config.Config{
		SchemaVersion: config.SchemaVersion,
		SourceNode:    config.SourceNodeConfig{ID: "dev-mac", Label: "DEV-MAC"},
		Listen:        config.ListenConfig{Addr: "127.0.0.1:8443"},
	}
	cfg.ApplyDefaults()
	return cfg
}
