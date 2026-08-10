package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func baseJSON() string {
	return `{
  "schema_version": 1,
  "source_node": {"id": "dev-mac"},
  "listen": {"addr": "127.0.0.1:8443"},
  "devices": [
    {"id": "pi-kiosk", "token_ref": "keyring:device:pi-kiosk"}
  ],
  "providers": [
    {
      "id": "mock-1",
      "type": "mock",
      "account_label": "demo",
      "region": "global",
      "interval": "60s",
      "stale_after": "5m",
      "secret_ref": "keyring:provider-key:mock-1"
    }
  ]
}`
}

func TestLoadValidConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(baseJSON()), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.SourceNode.ID != "dev-mac" {
		t.Errorf("SourceNode.ID = %q", c.SourceNode.ID)
	}
	if len(c.Providers) != 1 || c.Providers[0].ID != "mock-1" {
		t.Errorf("Providers = %v", c.Providers)
	}
	if !c.Providers[0].IsEnabled() {
		t.Error("default Enabled should be true")
	}
}

func TestValidateRejectsSchemaVersion(t *testing.T) {
	c := &Config{SchemaVersion: 2, SourceNode: SourceNodeConfig{ID: "x"}}
	if err := c.Validate(); err == nil {
		t.Error("Validate accepted wrong schema_version")
	}
}

func TestValidateRejectsBadSourceNode(t *testing.T) {
	cases := []string{"", "has space", strings.Repeat("a", 65), "with/slash"}
	for _, id := range cases {
		c := &Config{SchemaVersion: 1, SourceNode: SourceNodeConfig{ID: id}}
		if err := c.Validate(); err == nil {
			t.Errorf("Validate accepted source_node.id %q", id)
		}
	}
}

func TestValidateRejectsPublicBind(t *testing.T) {
	c := &Config{
		SchemaVersion: 1,
		SourceNode:    SourceNodeConfig{ID: "x"},
		Listen:        ListenConfig{Addr: "0.0.0.0:8443"},
	}
	if err := c.Validate(); err == nil {
		t.Error("Validate accepted 0.0.0.0 without allow_public_bind")
	}
	c.Listen.AllowPublicBind = true
	if err := c.Validate(); err != nil {
		t.Errorf("Validate rejected explicit allow_public_bind: %v", err)
	}
}

func TestValidateTLSCertificatePair(t *testing.T) {
	c := &Config{
		SchemaVersion: SchemaVersion,
		SourceNode:    SourceNodeConfig{ID: "x"},
		Listen: ListenConfig{
			Addr: "127.0.0.1:8443",
			TLS:  TLSConfig{Cert: "/tmp/cert.pem"},
		},
	}
	if err := c.Validate(); err == nil {
		t.Fatal("Validate accepted a certificate without a key")
	}
	c.Listen.TLS.Key = "auto"
	if err := c.Validate(); err == nil {
		t.Fatal("Validate accepted an explicit certificate with an auto key")
	}
	c.Listen.TLS.Key = "/tmp/key.pem"
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate rejected explicit certificate pair: %v", err)
	}
	c.Listen.TLS = TLSConfig{Cert: "auto", Key: "auto"}
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate rejected auto certificate pair: %v", err)
	}
}

func TestValidateDetectsDuplicateDeviceID(t *testing.T) {
	c := &Config{
		SchemaVersion: 1,
		SourceNode:    SourceNodeConfig{ID: "x"},
		Listen:        ListenConfig{Addr: "127.0.0.1:8443"},
		Devices: []DeviceConfig{
			{ID: "pi", TokenRef: "keyring:d:pi"},
			{ID: "pi", TokenRef: "keyring:d:pi2"},
		},
	}
	if err := c.Validate(); err == nil {
		t.Error("Validate accepted duplicate device IDs")
	}
}

func TestValidateRejectsMissingTokenRef(t *testing.T) {
	c := &Config{
		SchemaVersion: 1,
		SourceNode:    SourceNodeConfig{ID: "x"},
		Listen:        ListenConfig{Addr: "127.0.0.1:8443"},
		Devices:       []DeviceConfig{{ID: "pi", TokenRef: ""}},
	}
	if err := c.Validate(); err == nil {
		t.Error("Validate accepted empty token_ref for device")
	}
}

func TestValidateProviderRegionCustom(t *testing.T) {
	good := ProviderConfig{
		ID: "p1", Type: "mock", AccountLabel: "demo",
		Region: "custom", BaseURL: "https://api.example.com/v1",
		Interval: "60s", StaleAfter: "5m",
	}
	c := &Config{
		SchemaVersion: 1, SourceNode: SourceNodeConfig{ID: "x"},
		Listen:    ListenConfig{Addr: "127.0.0.1:8443"},
		Providers: []ProviderConfig{good},
	}
	if err := c.Validate(); err != nil {
		t.Errorf("Validate rejected good provider: %v", err)
	}

	c.Providers[0].BaseURL = "" // region=custom requires URL
	if err := c.Validate(); err == nil {
		t.Error("Validate accepted region=custom with no base_url")
	}

	c.Providers[0].BaseURL = "http://api.example.com/v1" // public http
	if err := c.Validate(); err == nil {
		t.Error("Validate accepted http on non-loopback")
	}

	c.Providers[0].BaseURL = "https://169.254.169.254/latest" // metadata
	if err := c.Validate(); err == nil {
		t.Error("Validate accepted cloud metadata IP")
	}

	c.Providers[0].BaseURL = "https://10.0.0.1/x" // RFC1918
	if err := c.Validate(); err == nil {
		t.Error("Validate accepted RFC1918 base URL")
	}
}

func TestValidateProviderIntervalAndStaleAfter(t *testing.T) {
	p := ProviderConfig{
		ID: "p", Type: "mock", AccountLabel: "demo",
		Region: "global", Interval: "1s", StaleAfter: "1s",
	}
	c := &Config{
		SchemaVersion: 1, SourceNode: SourceNodeConfig{ID: "x"},
		Listen:    ListenConfig{Addr: "127.0.0.1:8443"},
		Providers: []ProviderConfig{p},
	}
	if err := c.Validate(); err == nil {
		t.Error("Validate accepted sub-5s interval")
	}

	c.Providers[0].Interval = "60s"
	c.Providers[0].StaleAfter = "10s" // < interval
	if err := c.Validate(); err == nil {
		t.Error("Validate accepted stale_after < interval")
	}
}

func TestValidateRegistryKnownTypes(t *testing.T) {
	c := &Config{
		SchemaVersion: 1, SourceNode: SourceNodeConfig{ID: "x"},
		Listen: ListenConfig{Addr: "127.0.0.1:8443"},
		Providers: []ProviderConfig{{
			ID: "p", Type: "mystery", AccountLabel: "demo",
			Region: "global", Interval: "60s", StaleAfter: "5m",
		}},
	}
	known := map[string]bool{"mock": true}
	if err := c.ValidateWithRegistry(known); err == nil {
		t.Error("ValidateWithRegistry accepted unknown type")
	}
	c.Providers[0].Type = "mock"
	if err := c.ValidateWithRegistry(known); err != nil {
		t.Errorf("ValidateWithRegistry rejected mock: %v", err)
	}
}

func TestSaveAndReloadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	c, err := LoadFromBytes([]byte(baseJSON()))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("permission = %o, want 0600", info.Mode().Perm())
	}
	c2, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c2.SourceNode.ID != c.SourceNode.ID {
		t.Errorf("round-trip changed source_node.id")
	}
	if len(c2.Providers) != 1 {
		t.Errorf("round-trip changed providers")
	}
}

func TestLoadFromBytesIgnoresUnknownFields(t *testing.T) {
	raw := []byte(baseJSON())
	// Inject a harmless extra field.
	raw = []byte(strings.Replace(string(raw),
		`"interval": "60s"`, `"interval": "60s", "future_field": 42`, 1))
	c, err := LoadFromBytes(raw)
	if err != nil {
		t.Fatalf("LoadFromBytes: %v", err)
	}
	if len(c.Providers) != 1 {
		t.Errorf("providers = %d, want 1", len(c.Providers))
	}
}

func TestLoadFromBytesStrictRejectsUnknown(t *testing.T) {
	raw := []byte(strings.Replace(baseJSON(),
		`"interval": "60s"`, `"interval": "60s", "future_field": 42`, 1))
	if _, err := parse(raw); err == nil {
		t.Error("strict parse accepted unknown field")
	}
}

// LoadFromBytes is a test helper that does not require a file on disk.
func LoadFromBytes(raw []byte) (*Config, error) {
	c, err := ParseAny(raw)
	if err != nil {
		return nil, err
	}
	c.ApplyDefaults()
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return c, nil
}
