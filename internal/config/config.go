// Package config loads, validates and persists the homepi-node
// configuration file.
//
// The configuration is plain JSON with a small schema (see Config). It
// stores Provider credentials only by reference (secret_ref); the actual
// keys live in the OS keyring (or a 0600-file fallback) managed by the
// secretstore package. This split keeps the on-disk config safe to back
// up and to inspect, and makes it impossible for the daemon to log a key
// by mistake.
//
// config.Load only checks the file shape and the values it knows about
// (durations, enums, single-source-node invariant). Semantic checks like
// "is this provider type registered" are performed by the registry at
// serve time; this keeps the package dependency-light and lets new
// connector types land without touching this file.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SchemaVersion is the current on-disk version. Loaders refuse older
// major versions; newer minor fields are tolerated and ignored.
const SchemaVersion = 1

// Config is the on-disk schema. Field tags define the JSON contract; the
// Validate method enforces the invariants.
type Config struct {
	SchemaVersion int              `json:"schema_version"`
	SourceNode    SourceNodeConfig `json:"source_node"`
	Listen        ListenConfig     `json:"listen"`
	Devices       []DeviceConfig   `json:"devices"`
	Providers     []ProviderConfig `json:"providers"`
	Metrics       MetricsConfig    `json:"metrics,omitempty"`
}

// SourceNodeConfig identifies the daemon to its paired devices.
type SourceNodeConfig struct {
	ID    string `json:"id"`
	Label string `json:"label,omitempty"`
}

// ListenConfig controls the bind address and TLS mode.
type ListenConfig struct {
	Addr            string    `json:"addr"`
	AllowPublicBind bool      `json:"allow_public_bind,omitempty"`
	TLS             TLSConfig `json:"tls,omitempty"`
}

// TLSConfig selects the certificate source. "auto" tells the daemon to
// generate a self-signed pair under the data directory.
type TLSConfig struct {
	Cert string `json:"cert,omitempty"`
	Key  string `json:"key,omitempty"`
}

// DeviceConfig pairs a display device with a secret reference that
// resolves to its bearer token.
type DeviceConfig struct {
	ID                string   `json:"id"`
	TokenRef          string   `json:"token_ref"`
	SubscribedMetrics []string `json:"subscribed_metrics,omitempty"`
}

// ProviderConfig declares one upstream data source.
type ProviderConfig struct {
	ID           string            `json:"id"`
	Type         string            `json:"type"`
	AccountLabel string            `json:"account_label"`
	Region       string            `json:"region,omitempty"`
	BaseURL      string            `json:"base_url,omitempty"`
	Interval     string            `json:"interval,omitempty"`
	StaleAfter   string            `json:"stale_after,omitempty"`
	Enabled      *bool             `json:"enabled,omitempty"`
	SecretRef    string            `json:"secret_ref,omitempty"`
	AuthFile     string            `json:"auth_file,omitempty"`    // codex_usage only
	MockFixture  string            `json:"mock_fixture,omitempty"` // mock-only
	Options      map[string]string `json:"options,omitempty"`
}

// MetricsConfig collects daemon-wide metrics knobs.
type MetricsConfig struct {
	MaxSnapshotBytes int `json:"max_snapshot_bytes,omitempty"`
}

// DefaultPath returns the canonical per-user config path. It honours
// HOMEPI_NODE_CONFIG when set, then OS conventions, then a fallback
// relative path that keeps the daemon runnable from a fresh clone.
func DefaultPath() string {
	if v := os.Getenv("HOMEPI_NODE_CONFIG"); v != "" {
		return v
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return filepath.Join(".", "homepi-node.json")
	}
	return filepath.Join(base, "homepi-node", "config.json")
}

// Load reads and validates the configuration at path. It applies defaults
// for any field the operator left blank so downstream code never has to
// handle "missing" in addition to "invalid".
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}
	c, err := parse(raw)
	if err != nil {
		return nil, err
	}
	c.ApplyDefaults()
	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("config: %s: %w", path, err)
	}
	return c, nil
}

// parse decodes raw without applying defaults. Tests use it directly.
func parse(raw []byte) (*Config, error) {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	var c Config
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("config: parse: %w", err)
	}
	return &c, nil
}

// ParseAny is like parse but allows unknown fields, mirroring the wire
// protocol's compatibility rule.
func ParseAny(raw []byte) (*Config, error) {
	var c Config
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("config: parse: %w", err)
	}
	return &c, nil
}

// ApplyDefaults fills in any optional field whose absence would break
// downstream code. It is exported so callers that synthesise a Config in
// memory (legacy flag mode in cmd/homepi-node) can rely on the same
// defaults that Load applies.
func (c *Config) ApplyDefaults() {
	if c.SchemaVersion == 0 {
		c.SchemaVersion = SchemaVersion
	}
	if c.Listen.Addr == "" {
		c.Listen.Addr = "127.0.0.1:8443"
	}
	if c.Listen.TLS.Cert == "" && c.Listen.TLS.Key == "" {
		c.Listen.TLS.Cert = "auto"
		c.Listen.TLS.Key = "auto"
	}
	if c.Providers == nil {
		c.Providers = []ProviderConfig{}
	}
	if c.Devices == nil {
		c.Devices = []DeviceConfig{}
	}
	for i := range c.Providers {
		if c.Providers[i].Region == "" {
			c.Providers[i].Region = "global"
		}
		if c.Providers[i].Interval == "" {
			c.Providers[i].Interval = "60s"
		}
		if c.Providers[i].StaleAfter == "" {
			c.Providers[i].StaleAfter = "5m"
		}
		if c.Providers[i].Enabled == nil {
			trueVal := true
			c.Providers[i].Enabled = &trueVal
		}
		if len(c.Providers[i].Options) == 0 {
			c.Providers[i].Options = nil
		}
	}
}

// Save writes the config back to path with 0600 permissions, atomically.
func (c *Config) Save(path string) error {
	if path == "" {
		return errors.New("config: empty save path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("config: mkdir: %w", err)
	}
	if c.SchemaVersion == 0 {
		c.SchemaVersion = SchemaVersion
	}
	body, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("config: marshal: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "config-*.json.tmp")
	if err != nil {
		return fmt.Errorf("config: create temp: %w", err)
	}
	cleanup := func() { _ = os.Remove(tmp.Name()) }
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		cleanup()
		return err
	}
	if dir, err := os.Open(filepath.Dir(path)); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}

// IntervalDuration returns the provider's collection interval as a
// parsed time.Duration. It exists so the daemon does not parse the same
// string twice.
func (p ProviderConfig) IntervalDuration() (time.Duration, error) {
	return parseDuration("interval", p.Interval)
}

// StaleAfterDuration returns the stale_after window as a Duration.
func (p ProviderConfig) StaleAfterDuration() (time.Duration, error) {
	return parseDuration("stale_after", p.StaleAfter)
}

// IsEnabled reports whether the provider should be collected. The
// pointer indirection makes "missing" equivalent to "true" in JSON.
func (p ProviderConfig) IsEnabled() bool {
	if p.Enabled == nil {
		return true
	}
	return *p.Enabled
}
