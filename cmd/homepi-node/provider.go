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
	"github.com/galendai/homepi-mon/internal/configtx"
)

// runProvider dispatches provider subcommands. The implementation in
// this file is a thin adapter over configtx.Service: the CLI keeps
// its flag-based interface, but every read and write goes through the
// transaction service so the Web Admin and the CLI see the same
// behaviour.
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

// openService builds a configtx.Service from a per-command config
// path. The function centralises the option list so a future change
// to the secret dir / data dir logic only happens in one place.
func openService(path string) (*configtx.Service, error) {
	secretDir := os.Getenv("HOMEPI_SECRET_DIR")
	if secretDir == "" {
		base, err := defaultDataDir()
		if err != nil {
			return nil, err
		}
		secretDir = base + "/secrets"
	}
	dataDir := os.Getenv("HOMEPI_DATA_DIR")
	if dataDir == "" {
		base, err := defaultDataDir()
		if err != nil {
			return nil, err
		}
		dataDir = base
	}
	return configtx.New(context.Background(), configtx.Options{
		ConfigPath:   path,
		SecretDir:    secretDir,
		DataDir:      dataDir,
		ServiceLabel: binaryName,
	})
}

func providerList(args []string) error {
	fs := flag.NewFlagSet("provider list", flag.ContinueOnError)
	path := fs.String("config", config.DefaultPath(), "path to config.json")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := loadOrEmpty(*path)
	if err != nil {
		return err
	}
	if len(cfg.Providers) == 0 {
		fmt.Println("(no providers configured)")
		return nil
	}
	fmt.Printf("%-20s %-15s %-8s %-7s %-12s %-20s %s\n",
		"ID", "TYPE", "REGION", "ENABLED", "INTERVAL", "SECRET_REF", "AUTH_FILE")
	for _, p := range cfg.Providers {
		fmt.Printf("%-20s %-15s %-8s %-7t %-12s %-20s %s\n",
			p.ID, p.Type, p.Region, p.IsEnabled(), p.Interval,
			maskRef(p.SecretRef), displayAuthFile(p.Type, p.AuthFile))
	}
	return nil
}

func providerAdd(args []string) error {
	fs := flag.NewFlagSet("provider add", flag.ContinueOnError)
	path := fs.String("config", config.DefaultPath(), "path to config.json")
	id := fs.String("id", "", "provider ID (required)")
	typ := fs.String("type", "", "provider type")
	account := fs.String("account-label", "", "operator-readable alias")
	region := fs.String("region", "global", "global|cn|custom")
	baseURL := fs.String("base-url", "", "required when region=custom")
	interval := fs.String("interval", "60s", "collection interval")
	staleAfter := fs.String("stale-after", "5m", "freshness budget")
	secretRef := fs.String("secret-ref", "", "keyring reference; defaults to keyring:provider-key:<id>")
	authFile := fs.String("auth-file", "", "Codex auth.json path (type=codex_usage only)")
	secretStdin := fs.Bool("secret-stdin", false,
		"read the secret from standard input instead of argv")
	mockFixture := fs.String("mock-fixture", "",
		"path to the mock fixture file (type=mock only)")
	// Reject the legacy -secret flag explicitly so the operator
	// cannot accidentally pass a key on the command line.
	if err := fs.Parse(args); err != nil {
		return err
	}
	for _, a := range args {
		if a == "-secret" || strings.HasPrefix(a, "-secret=") {
			return errors.New("provider add: -secret is not accepted; " +
				"use HOMEPI_PROVIDER_SECRET or -secret-stdin")
		}
	}
	if *id == "" || *typ == "" || *account == "" {
		return errors.New("provider add: -id, -type and -account-label are required")
	}
	svc, err := openService(*path)
	if err != nil {
		return err
	}
	draft, err := svc.OpenDraft(context.Background())
	if err != nil {
		return err
	}
	for _, existing := range draft.Pending().Providers {
		if existing.ID == *id {
			return fmt.Errorf("provider add: id %q already exists", *id)
		}
	}
	regionVal := *region
	intervalVal := *interval
	staleVal := *staleAfter
	mockVal := *mockFixture
	edit := configtx.ProviderEdit{
		ID:           *id,
		NewType:      *typ,
		AccountLabel: strPtrLocal(*account),
		Region:       &regionVal,
		BaseURL:      strPtrLocal(*baseURL),
		Interval:     &intervalVal,
		StaleAfter:   &staleVal,
		Enabled:      boolPtrLocal(true),
		AuthFile:     strPtrLocal(*authFile),
		MockFixture:  strPtrLocal(mockVal),
	}
	// Resolve secret input before opening the draft edit so the
	// secret never has to flow through the CLI argv. The
	// configtx overlay accepts the raw value at Edit time.
	needsSecret := *typ != "mock" && *typ != "codex_usage"
	if needsSecret {
		secretValue, err := readProviderSecret(*secretStdin, os.Stdin)
		if err != nil {
			return err
		}
		resolvedRef := *secretRef
		if resolvedRef == "" {
			resolvedRef = "keyring:provider-key:" + *id
		}
		edit.SecretRef = strPtrLocal(resolvedRef)
		edit.CandidateSecret = secretValue
	} else if *secretRef != "" || *secretStdin {
		return fmt.Errorf("provider add: type=%s does not accept a provider secret", *typ)
	}
	if err := draft.EditProvider(edit); err != nil {
		return err
	}
	if _, err := svc.Apply(context.Background(), draft, configtx.ApplyOptions{}); err != nil {
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
	authFile := fs.String("auth-file", "", "Codex auth.json path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *id == "" {
		return errors.New("provider edit: -id is required")
	}
	svc, err := openService(*path)
	if err != nil {
		return err
	}
	draft, err := svc.OpenDraft(context.Background())
	if err != nil {
		return err
	}
	set := make(map[string]bool)
	fs.Visit(func(fl *flag.Flag) { set[fl.Name] = true })
	edit := configtx.ProviderEdit{ID: *id}
	if set["region"] {
		edit.Region = strPtrLocal(*region)
	}
	if set["base-url"] {
		edit.BaseURL = strPtrLocal(*baseURL)
	}
	if set["interval"] {
		edit.Interval = strPtrLocal(*interval)
	}
	if set["stale-after"] {
		edit.StaleAfter = strPtrLocal(*staleAfter)
	}
	if set["enabled"] {
		b, err := parseBool(*enabled)
		if err != nil {
			return err
		}
		edit.Enabled = boolPtrLocal(b)
	}
	if set["auth-file"] {
		edit.AuthFile = strPtrLocal(*authFile)
	}
	if err := draft.EditProvider(edit); err != nil {
		return err
	}
	if _, err := svc.Apply(context.Background(), draft, configtx.ApplyOptions{}); err != nil {
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
	svc, err := openService(*path)
	if err != nil {
		return err
	}
	draft, err := svc.OpenDraft(context.Background())
	if err != nil {
		return err
	}
	res, err := draft.TestProvider(context.Background(), *id)
	if err != nil {
		fmt.Fprintf(os.Stderr, "provider test %q: %v\n", *id, err)
		return err
	}
	fmt.Printf("provider %q ok, %d metrics (in %s)\n",
		*id, res.MetricCount, res.Elapsed.Round(time.Millisecond))
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
	svc, err := openService(*path)
	if err != nil {
		return err
	}
	draft, err := svc.OpenDraft(context.Background())
	if err != nil {
		return err
	}
	ref, err := draft.DeleteProvider(*id)
	if err != nil {
		return err
	}
	applyOpts := configtx.ApplyOptions{}
	if *keepSecret && ref != "" {
		applyOpts.KeepSecretRefs = []string{ref}
	}
	if _, err := svc.Apply(context.Background(), draft, applyOpts); err != nil {
		return err
	}
	fmt.Printf("removed provider %q\n", *id)
	return nil
}

// loadOrEmpty returns the configuration at path or a freshly-initialised
// empty Config if the file does not yet exist. This is shared by the
// read-only provider subcommands and the Web Admin bootstrap.
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
	const tail = 6
	if len(ref) <= tail+len("keyring:") {
		return ref
	}
	return ref[:len("keyring:")] + "..." + ref[len(ref)-tail:]
}

func displayAuthFile(providerType, path string) string {
	if providerType != "codex_usage" {
		return "-"
	}
	if path == "" {
		return "default"
	}
	return path
}

func boolPtr(b bool) *bool { return &b }

func boolPtrLocal(b bool) *bool { return &b }

func strPtrLocal(s string) *string { return &s }

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
