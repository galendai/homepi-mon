package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/galendai/homepi-mon/internal/config"
	"github.com/galendai/homepi-mon/internal/connector"
	"github.com/galendai/homepi-mon/internal/deviceacl"
	"github.com/galendai/homepi-mon/internal/secretstore"
)

func runDevice(args []string) error {
	if len(args) == 0 {
		return errors.New("device: expected one of list|add|revoke")
	}
	switch args[0] {
	case "list":
		return deviceList(args[1:])
	case "add":
		return deviceAdd(args[1:])
	case "revoke":
		return deviceRevoke(args[1:])
	}
	return fmt.Errorf("device: unknown subcommand %q", args[0])
}

func deviceList(args []string) error {
	fs := flag.NewFlagSet("device list", flag.ContinueOnError)
	path := fs.String("config", config.DefaultPath(), "path to config.json")
	if err := fs.Parse(args); err != nil {
		return err
	}
	c, err := config.Load(*path)
	if err != nil {
		return err
	}
	if len(c.Devices) == 0 {
		fmt.Println("(no devices configured)")
		return nil
	}
	fmt.Printf("%-20s %s\n", "ID", "TOKEN_REF")
	for _, d := range c.Devices {
		fmt.Printf("%-20s %s\n", d.ID, maskRef(d.TokenRef))
	}
	return nil
}

// deviceAdd writes a new device entry to config.json and stores the
// generated token in the keyring. The plaintext token is printed once
// to stdout; the operator must copy it into the Pi's environment.
func deviceAdd(args []string) error {
	fs := flag.NewFlagSet("device add", flag.ContinueOnError)
	path := fs.String("config", config.DefaultPath(), "path to config.json")
	id := fs.String("id", "", "device ID (required)")
	tokenRef := fs.String("token-ref", "", "keyring reference; default keyring:device-token:<id>")
	token := fs.String("token", "", "use an existing token instead of generating one")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *id == "" {
		return errors.New("device add: -id is required")
	}
	ref := *tokenRef
	if ref == "" {
		ref = "keyring:device-token:" + *id
	}
	tok := *token
	if tok == "" {
		v, err := generateDeviceToken()
		if err != nil {
			return err
		}
		tok = v
	}
	store, err := secretstore.OpenFromEnv(context.Background())
	if err != nil {
		return err
	}
	if err := addDevice(*path, *id, ref, tok, store); err != nil {
		return err
	}
	fmt.Printf("added device %q\n", *id)
	fmt.Printf("token (print once): %s\n", tok)
	return nil
}

func addDevice(path, id, ref, token string, store secretstore.Store) error {
	c, err := loadOrEmpty(path)
	if err != nil {
		return err
	}
	for _, d := range c.Devices {
		if d.ID == id {
			return fmt.Errorf("device add: id %q already exists", id)
		}
	}
	if err := store.Set(context.Background(), ref, token); err != nil {
		return err
	}
	c.Devices = append(c.Devices, config.DeviceConfig{ID: id, TokenRef: ref})
	c.ApplyDefaults()
	if err := c.ValidateWithRegistry(stringSet(connector.KnownTypes())); err != nil {
		// roll back the keyring write so the config and secrets stay in sync
		_ = store.Delete(context.Background(), ref)
		return err
	}
	if err := c.Save(path); err != nil {
		return err
	}
	return nil
}

// deviceRevoke looks up the device in the configuration, removes its
// token_ref from the keyring, and adds the token's SHA-256 to the
// revoked.json file so future requests are rejected even if the old
// token leaks later.
func deviceRevoke(args []string) error {
	fs := flag.NewFlagSet("device revoke", flag.ContinueOnError)
	path := fs.String("config", config.DefaultPath(), "path to config.json")
	id := fs.String("id", "", "device ID to revoke (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *id == "" {
		return errors.New("device revoke: -id is required")
	}
	dataDir, err := defaultDataDir()
	if err != nil {
		return err
	}
	store, err := secretstore.OpenFromEnv(context.Background())
	if err != nil {
		return err
	}
	return revokeDevice(*path, *id, dataDir, store)
}

func revokeDevice(path, id, dataDir string, store secretstore.Store) error {
	c, err := config.Load(path)
	if err != nil {
		return err
	}
	idx := -1
	var ref string
	for i, d := range c.Devices {
		if d.ID == id {
			idx = i
			ref = d.TokenRef
			break
		}
	}
	if ref == "" {
		return fmt.Errorf("device revoke: id %q not found", id)
	}

	revokedPath := filepath.Join(dataDir, "revoked.json")
	acl, err := deviceacl.Load(revokedPath)
	if err != nil {
		return err
	}
	tok, err := store.Get(context.Background(), ref)
	if err != nil {
		return fmt.Errorf("device revoke: resolve token_ref %q: %w", ref, err)
	}
	acl.Add(tok)
	if err := acl.Save(revokedPath); err != nil {
		return err
	}
	// Keep the durable token until the config no longer references it. If the
	// config save fails, the persisted revocation still fails authentication
	// closed and a retry can resolve the original token.
	c.Devices = append(c.Devices[:idx], c.Devices[idx+1:]...)
	if err := c.Save(path); err != nil {
		return fmt.Errorf("device revoke: save config: %w", err)
	}
	if err := store.Delete(context.Background(), ref); err != nil {
		fmt.Fprintf(os.Stderr, "WARN: could not delete keyring entry %q: %v\n",
			ref, err)
	}
	fmt.Printf("revoked device %q\n", id)
	return nil
}
