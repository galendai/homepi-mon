package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/galendai/homepi-mon/internal/config"
	"github.com/galendai/homepi-mon/internal/connector"
	"github.com/galendai/homepi-mon/internal/secretstore"
)

func runProvider(args []string) error {
	if len(args) == 0 {
		return errors.New("provider: expected one of list|add|edit|test|remove")
	}
	switch args[0] {
	case "list":
		return providerList(args[1:])
	case "add":
		return providerAdd(args[1:])
	case "edit":
		return providerEdit(args[1:])
	case "test":
		return providerTest(args[1:])
	case "remove":
		return providerRemove(args[1:])
	}
	return fmt.Errorf("provider: unknown subcommand %q", args[0])
}

func providerList(args []string) error {
	fs := flag.NewFlagSet("provider list", flag.ContinueOnError)
	path := fs.String("config", config.DefaultPath(), "path to config.json")
	if err := fs.Parse(args); err != nil {
		return err
	}
	c, err := loadOrEmpty(*path)
	if err != nil {
		return err
	}
	if len(c.Providers) == 0 {
		fmt.Println("(no providers configured)")
		return nil
	}
	fmt.Printf("%-20s %-15s %-8s %-7s %-12s %s\n",
		"ID", "TYPE", "REGION", "ENABLED", "INTERVAL", "SECRET_REF")
	for _, p := range c.Providers {
		fmt.Printf("%-20s %-15s %-8s %-7t %-12s %s\n",
			p.ID, p.Type, p.Region, p.IsEnabled(), p.Interval, maskRef(p.SecretRef))
	}
	return nil
}

func providerAdd(args []string) error {
	fs := flag.NewFlagSet("provider add", flag.ContinueOnError)
	path := fs.String("config", config.DefaultPath(), "path to config.json")
	id := fs.String("id", "", "provider ID (required)")
	typ := fs.String("type", "", "provider type: mock|minimax_coding|codex_usage|kimi_coding|deepseek_api|kimi_api")
	account := fs.String("account-label", "", "operator-readable alias")
	region := fs.String("region", "global", "global|cn|custom")
	baseURL := fs.String("base-url", "", "required when region=custom")
	interval := fs.String("interval", "60s", "collection interval")
	staleAfter := fs.String("stale-after", "5m", "freshness budget")
	secretRef := fs.String("secret-ref", "", "keyring reference; defaults to keyring:provider-key:<id>")
	secretStdin := fs.Bool("secret-stdin", false,
		"read the secret from standard input instead of argv")
	mockFixture := fs.String("mock-fixture", "",
		"path to the mock fixture file (type=mock only)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *id == "" || *typ == "" || *account == "" {
		return errors.New("provider add: -id, -type and -account-label are required")
	}
	if !connector.HasType(*typ) {
		return fmt.Errorf("provider add: type %q is not registered", *typ)
	}
	if *region == "custom" && *baseURL == "" {
		return errors.New("provider add: region=custom requires -base-url")
	}
	c, err := loadOrEmpty(*path)
	if err != nil {
		return err
	}
	for _, existing := range c.Providers {
		if existing.ID == *id {
			return fmt.Errorf("provider add: id %q already exists", *id)
		}
	}
	ref := *secretRef
	if ref == "" {
		ref = "keyring:provider-key:" + *id
	}
	if !strings.HasPrefix(ref, "keyring:") {
		return fmt.Errorf("provider add: secret-ref %q must start with keyring:", ref)
	}
	secretValue, err := readProviderSecret(*secretStdin, os.Stdin)
	if err != nil {
		return err
	}
	if secretValue != "" {
		store, err := secretstore.OpenFromEnv(context.Background())
		if err != nil {
			return err
		}
		if err := store.Set(context.Background(), ref, secretValue); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr,
			"stored secret in %s backend\n", store.Backend())
	}
	c.Providers = append(c.Providers, config.ProviderConfig{
		ID:           *id,
		Type:         *typ,
		AccountLabel: *account,
		Region:       *region,
		BaseURL:      *baseURL,
		Interval:     *interval,
		StaleAfter:   *staleAfter,
		Enabled:      boolPtr(true),
		SecretRef:    ref,
		MockFixture:  *mockFixture,
	})
	c.ApplyDefaults()
	if err := c.ValidateWithRegistry(stringSet(connector.KnownTypes())); err != nil {
		return err
	}
	if err := c.Save(*path); err != nil {
		return err
	}
	fmt.Printf("added provider %q\n", *id)
	return nil
}

func providerEdit(args []string) error {
	fs := flag.NewFlagSet("provider edit", flag.ContinueOnError)
	path := fs.String("config", config.DefaultPath(), "path to config.json")
	id := fs.String("id", "", "provider ID to edit (required)")
	region := fs.String("region", "", "global|cn|custom")
	baseURL := fs.String("base-url", "", "custom provider base URL")
	interval := fs.String("interval", "", "collection interval")
	staleAfter := fs.String("stale-after", "", "freshness budget")
	enabled := fs.String("enabled", "", "true|false")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *id == "" {
		return errors.New("provider edit: -id is required")
	}
	c, err := loadOrEmpty(*path)
	if err != nil {
		return err
	}
	idx := -1
	for i, p := range c.Providers {
		if p.ID == *id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("provider edit: id %q not found", *id)
	}
	set := make(map[string]bool)
	fs.Visit(func(fl *flag.Flag) { set[fl.Name] = true })
	if set["region"] {
		c.Providers[idx].Region = *region
	}
	if set["base-url"] {
		c.Providers[idx].BaseURL = *baseURL
	}
	if set["interval"] {
		c.Providers[idx].Interval = *interval
	}
	if set["stale-after"] {
		c.Providers[idx].StaleAfter = *staleAfter
	}
	if set["enabled"] {
		b, err := parseBool(*enabled)
		if err != nil {
			return err
		}
		c.Providers[idx].Enabled = boolPtr(b)
	}
	c.ApplyDefaults()
	if err := c.ValidateWithRegistry(stringSet(connector.KnownTypes())); err != nil {
		return err
	}
	if err := c.Save(*path); err != nil {
		return err
	}
	fmt.Printf("updated provider %q\n", *id)
	return nil
}

func providerTest(args []string) error {
	fs := flag.NewFlagSet("provider test", flag.ContinueOnError)
	path := fs.String("config", config.DefaultPath(), "path to config.json")
	id := fs.String("id", "", "provider ID to test (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *id == "" {
		return errors.New("provider test: -id is required")
	}
	c, err := loadOrEmpty(*path)
	if err != nil {
		return err
	}
	var spec *config.ProviderConfig
	for i := range c.Providers {
		if c.Providers[i].ID == *id {
			spec = &c.Providers[i]
			break
		}
	}
	if spec == nil {
		return fmt.Errorf("provider test: id %q not found", *id)
	}
	store, err := secretstore.OpenFromEnv(context.Background())
	if err != nil {
		return err
	}
	conn, err := connector.Build(*spec, store)
	if err != nil {
		return err
	}
	if err := conn.ValidateConfig(); err != nil {
		return fmt.Errorf("config invalid: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	metrics, err := conn.Collect(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "provider test %q: %v\n", *id, err)
		return err
	}
	fmt.Printf("provider %q ok, %d metrics\n", *id, len(metrics))
	return nil
}

func providerRemove(args []string) error {
	fs := flag.NewFlagSet("provider remove", flag.ContinueOnError)
	path := fs.String("config", config.DefaultPath(), "path to config.json")
	id := fs.String("id", "", "provider ID to remove (required)")
	keepSecret := fs.Bool("keep-secret", false,
		"do not delete the keyring entry")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *id == "" {
		return errors.New("provider remove: -id is required")
	}
	c, err := loadOrEmpty(*path)
	if err != nil {
		return err
	}
	idx := -1
	var removed config.ProviderConfig
	for i, p := range c.Providers {
		if p.ID == *id {
			idx = i
			removed = p
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("provider remove: id %q not found", *id)
	}
	c.Providers = append(c.Providers[:idx], c.Providers[idx+1:]...)
	if err := c.Save(*path); err != nil {
		return err
	}
	if !*keepSecret && removed.SecretRef != "" {
		store, err := secretstore.OpenFromEnv(context.Background())
		if err != nil {
			return err
		}
		if err := store.Delete(context.Background(), removed.SecretRef); err != nil {
			fmt.Fprintf(os.Stderr,
				"WARN: provider removed but secret %q not deleted: %v\n",
				removed.SecretRef, err)
		}
	}
	fmt.Printf("removed provider %q\n", *id)
	return nil
}

// loadOrEmpty returns the configuration at path or a freshly-initialised
// empty Config if the file does not yet exist. This lets `provider add`
// work before `config init` has been called.
func loadOrEmpty(path string) (*config.Config, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		c := &config.Config{SchemaVersion: config.SchemaVersion}
		c.ApplyDefaults()
		return c, nil
	}
	return config.Load(path)
}

func maskRef(ref string) string {
	if ref == "" {
		return "-"
	}
	// Show the prefix and a short tail so the operator can tell
	// providers apart without leaking the secret name into logs.
	const tail = 6
	if len(ref) <= tail+len("keyring:") {
		return ref
	}
	return ref[:len("keyring:")] + "..." + ref[len(ref)-tail:]
}

func boolPtr(b bool) *bool { return &b }

func parseBool(s string) (bool, error) {
	switch strings.ToLower(s) {
	case "true", "yes", "on", "1":
		return true, nil
	case "false", "no", "off", "0":
		return false, nil
	}
	return false, fmt.Errorf("invalid boolean %q", s)
}

func readProviderSecret(fromStdin bool, in io.Reader) (string, error) {
	env := os.Getenv("HOMEPI_PROVIDER_SECRET")
	if fromStdin && env != "" {
		return "", errors.New("provider add: choose either HOMEPI_PROVIDER_SECRET or -secret-stdin")
	}
	if !fromStdin {
		return env, nil
	}
	const maxSecretBytes = 64 << 10
	raw, err := io.ReadAll(io.LimitReader(in, maxSecretBytes+1))
	if err != nil {
		return "", fmt.Errorf("provider add: read secret from stdin: %w", err)
	}
	if len(raw) > maxSecretBytes {
		return "", errors.New("provider add: stdin secret is too large")
	}
	secret := strings.TrimRight(string(raw), "\r\n")
	if secret == "" {
		return "", errors.New("provider add: -secret-stdin received an empty secret")
	}
	return secret, nil
}

func generateDeviceToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
