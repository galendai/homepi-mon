// Command homepi-node is the remote collection daemon.
//
// Phase 1 ships the full connector set, cross-platform secret storage, TLS,
// device token revocation and user-level service installation.
package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/galendai/homepi-mon/internal/buildinfo"

	_ "github.com/galendai/homepi-mon/internal/connector/codexusage"
	_ "github.com/galendai/homepi-mon/internal/connector/deepseek"
	_ "github.com/galendai/homepi-mon/internal/connector/kimiapi"
	_ "github.com/galendai/homepi-mon/internal/connector/kimicoding"
	_ "github.com/galendai/homepi-mon/internal/connector/minimax"
	_ "github.com/galendai/homepi-mon/internal/connector/mock"
)

const binaryName = "homepi-node"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, binaryName+": "+err.Error())
		os.Exit(1)
	}
}

// run dispatches the subcommand. Each leaf subcommand lives in its own
// file (serve.go, doctor.go, ...).
func run(args []string) error {
	if len(args) == 0 || isHelp(args[0]) {
		usage()
		return nil
	}
	switch args[0] {
	case "--version", "-version", "version":
		fmt.Println(buildinfo.String(binaryName))
		return nil
	case "serve":
		return runServe(args[1:])
	case "doctor":
		return runDoctor(args[1:])
	case "config":
		return runConfig(args[1:])
	case "provider":
		return runProvider(args[1:])
	case "device":
		return runDevice(args[1:])
	case "install", "start", "stop", "status", "uninstall":
		return runService(args)
	}
	usage()
	return errors.New("unknown command: " + args[0])
}

func isHelp(s string) bool {
	switch s {
	case "-h", "--help", "help":
		return true
	}
	return false
}

func usage() {
	fmt.Fprintf(os.Stderr, `%s %s

Usage:
  %s serve                       run the collection daemon and LAN API
  %s doctor                      print a redacted diagnostic summary
  %s config init|show|validate   manage the configuration file
  %s provider list|add|edit|test|remove
                                 manage provider credentials
  %s device list|add|revoke      manage paired display devices
  %s install|start|stop|status|uninstall
                                 manage the user-level service
  %s --version                   print version information

Phase 1 mock data path is documented in .vibe/IMPL-001-Phase1-P1-01-to-P1-03.md.
Provider configuration, OS credential storage and the official connectors
are delivered by tasks P1-04 through P1-06.
`, binaryName, buildinfo.Short(),
		binaryName, binaryName, binaryName,
		binaryName, binaryName, binaryName, binaryName)
}

// newLogger is shared by every subcommand. Diagnostics always go to
// stderr so stdout remains reserved for structured command output
// (e.g. `homepi-node provider list`).
func newLogger(verbose bool) *slog.Logger {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}
