package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/galendai/homepi-mon/internal/config"
	"github.com/galendai/homepi-mon/internal/connector"
)

func runConfig(args []string) error {
	if len(args) == 0 {
		return errors.New("config: expected one of init|show|validate")
	}
	switch args[0] {
	case "init":
		return configInit(args[1:])
	case "show":
		return configShow(args[1:])
	case "validate":
		return configValidate(args[1:])
	}
	return fmt.Errorf("config: unknown subcommand %q", args[0])
}

// configInit writes a documented example config to -out when no file
// already exists. It refuses to overwrite so a real configuration is
// never silently replaced by a placeholder.
func configInit(args []string) error {
	fs := flag.NewFlagSet("config init", flag.ContinueOnError)
	path := fs.String("out", config.DefaultPath(),
		"destination path for the new configuration file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if _, err := os.Stat(*path); err == nil {
		return fmt.Errorf("config: %s already exists; remove it before running init", *path)
	}
	body := exampleConfig()
	if err := os.MkdirAll(parentDir(*path), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(*path, []byte(body), 0o600); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n", *path)
	return nil
}

func configShow(args []string) error {
	fs := flag.NewFlagSet("config show", flag.ContinueOnError)
	path := fs.String("config", config.DefaultPath(), "path to config.json")
	if err := fs.Parse(args); err != nil {
		return err
	}
	c, err := config.Load(*path)
	if err != nil {
		return err
	}
	c.ApplyDefaults()
	// Secret refs are intentionally preserved: the operator needs to see
	// the reference names to know what to populate via `provider add`.
	body, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(body))
	return nil
}

func configValidate(args []string) error {
	fs := flag.NewFlagSet("config validate", flag.ContinueOnError)
	path := fs.String("config", config.DefaultPath(), "path to config.json")
	if err := fs.Parse(args); err != nil {
		return err
	}
	c, err := config.Load(*path)
	if err != nil {
		return err
	}
	if err := c.ValidateWithRegistry(stringSet(connector.KnownTypes())); err != nil {
		return err
	}
	fmt.Printf("%s: ok (%d providers, %d devices)\n",
		*path, len(c.Providers), len(c.Devices))
	return nil
}

// exampleConfig is the starter JSON. Comments are stripped before write
// because JSON has no standard comment syntax; the operator learns the
// field meanings from `homepi-node doctor` and from .vibe/Module-Spec-001.
func exampleConfig() string {
	return `{
  "schema_version": 1,
  "source_node": {
    "id": "dev-mac",
    "label": "DEV-MAC"
  },
  "listen": {
    "addr": "127.0.0.1:8443",
    "tls": {
      "cert": "auto",
      "key": "auto"
    }
  },
  "devices": [],
  "providers": []
}
`
}

func parentDir(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' || p[i] == os.PathSeparator {
			return p[:i]
		}
	}
	return "."
}
