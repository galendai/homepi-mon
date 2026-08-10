package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/galendai/homepi-mon/internal/buildinfo"
	"github.com/galendai/homepi-mon/internal/config"
	"github.com/galendai/homepi-mon/internal/connector"
	"github.com/galendai/homepi-mon/internal/secretstore"
	"github.com/galendai/homepi-mon/internal/tlsconfig"
)

func runDoctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	configPath := fs.String("config", config.DefaultPath(),
		"path to config.json to inspect")
	if err := fs.Parse(args); err != nil {
		return err
	}

	fmt.Println(buildinfo.String(binaryName))
	fmt.Println()
	fmt.Printf("user:     uid=%d euid=%d\n", os.Getuid(), os.Geteuid())
	if os.Geteuid() == 0 {
		fmt.Println("WARNING:  running as root; homepi-node must run as the logged-in user")
	}
	host, _ := os.Hostname()
	fmt.Printf("hostname: %s\n", host)

	dataDir, err := defaultDataDir()
	if err != nil {
		return err
	}
	fmt.Printf("data dir: %s\n", dataDir)

	// Secret store: probe which backend would be picked.
	store, err := secretstore.OpenFromEnv(context.Background())
	if err != nil {
		return fmt.Errorf("secret store: %w", err)
	}
	fmt.Printf("secretstore: backend=%s\n", store.Backend())

	// TLS: read or generate the certificate so the operator can copy
	// the fingerprint straight into `homepi-display run -node-cert-pin`.
	certPath := filepath.Join(dataDir, "cert.pem")
	if _, err := os.Stat(certPath); err == nil {
		fp, err := tlsconfig.Fingerprint(certPath)
		if err != nil {
			fmt.Printf("tls:      fingerprint unavailable (%v)\n", err)
		} else {
			fmt.Printf("tls:      fingerprint=%s\n", fp)
		}
	} else {
		fmt.Println("tls:      not generated yet (start the daemon to create it)")
	}

	// Revocation list: print count without leaking contents.
	revokedPath := filepath.Join(dataDir, "revoked.json")
	if _, err := os.Stat(revokedPath); err == nil {
		fmt.Println("revoked:  file present")
	} else {
		fmt.Println("revoked:  no revocations recorded")
	}

	// Configuration: print resolved path and provider summary.
	if _, err := os.Stat(*configPath); err == nil {
		c, err := config.Load(*configPath)
		if err != nil {
			fmt.Printf("config:   %s (invalid: %v)\n", *configPath, err)
			return nil
		}
		fmt.Printf("config:   %s (ok)\n", *configPath)
		fmt.Printf("          node=%s addr=%s devices=%d providers=%d\n",
			c.SourceNode.ID, c.Listen.Addr,
			len(c.Devices), len(c.Providers))
		for _, p := range c.Providers {
			fmt.Printf("            - %s type=%s region=%s enabled=%t\n",
				p.ID, p.Type, p.Region, p.IsEnabled())
		}
	} else {
		fmt.Printf("config:   %s (not present)\n", *configPath)
	}

	fmt.Println()
	fmt.Println("registered connector types:")
	for _, t := range connector.KnownTypes() {
		fmt.Printf("  - %s\n", t)
	}
	return nil
}
