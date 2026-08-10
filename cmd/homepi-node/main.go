// Command homepi-node is the remote collection daemon.
//
// Phase 1 ships the mock connector only: it proves the whole path end to end
// without any provider credential on the machine. Real connectors and the
// provider configuration CLI arrive in P1-04 through P1-06.
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/galendai/homepi-mon/internal/buildinfo"
	"github.com/galendai/homepi-mon/internal/connector/mock"
	"github.com/galendai/homepi-mon/internal/nodeapi"
	"github.com/galendai/homepi-mon/internal/scheduler"
	"github.com/galendai/homepi-mon/internal/state"
)

const binaryName = "homepi-node"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, binaryName+": "+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "--version", "-version", "version":
			fmt.Println(buildinfo.String(binaryName))
			return nil
		case "doctor":
			return doctor(args[1:])
		case "serve":
			return serve(args[1:])
		case "--help", "-h", "help":
			usage()
			return nil
		}
	}
	usage()
	return errors.New("no command given")
}

func usage() {
	fmt.Fprintf(os.Stderr, `%s %s

Usage:
  %s serve   [flags]   run the collection daemon and LAN API
  %s doctor  [flags]   print a redacted diagnostic summary
  %s --version         print version information

Phase 1 ships the mock connector only. Provider configuration, OS credential
storage and the official connectors are delivered by tasks P1-04 to P1-06.
`, binaryName, buildinfo.Short(), binaryName, binaryName, binaryName)
}

// serveFlags are the Phase 1 daemon options. A full configuration file lands
// with P1-04; until then every value is an explicit flag so nothing is implied.
type serveFlags struct {
	addr        string
	nodeID      string
	nodeLabel   string
	deviceID    string
	deviceToken string
	fixture     string
	interval    time.Duration
	verbose     bool
}

func parseServeFlags(args []string) (*serveFlags, error) {
	f := &serveFlags{}
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.StringVar(&f.addr, "addr", "127.0.0.1:8443",
		"LAN listen address; bind to a specific interface, never 0.0.0.0 on an untrusted network")
	fs.StringVar(&f.nodeID, "node-id", "", "stable source node ID (required)")
	fs.StringVar(&f.nodeLabel, "node-label", "", "user-facing node alias shown in the TUI header")
	fs.StringVar(&f.deviceID, "device-id", "", "paired display device ID (required)")
	fs.StringVar(&f.deviceToken, "device-token", "",
		"paired display token; if empty one is generated and printed once")
	fs.StringVar(&f.fixture, "mock-fixture", "", "path to the mock data file (required in Phase 1)")
	fs.DurationVar(&f.interval, "interval", 15*time.Second, "collection interval")
	fs.BoolVar(&f.verbose, "verbose", false, "log at debug level")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	// ADR-014: Phase 1 binds exactly one node and one display, so both IDs are
	// mandatory rather than defaulted to something guessable.
	if f.nodeID == "" {
		return nil, errors.New("-node-id is required")
	}
	if f.deviceID == "" {
		return nil, errors.New("-device-id is required")
	}
	if f.fixture == "" {
		return nil, errors.New("-mock-fixture is required in Phase 1")
	}
	if f.nodeLabel == "" {
		f.nodeLabel = f.nodeID
	}
	return f, nil
}

func serve(args []string) error {
	f, err := parseServeFlags(args)
	if err != nil {
		return err
	}
	log := newLogger(f.verbose)

	if f.deviceToken == "" {
		f.deviceToken, err = generateToken()
		if err != nil {
			return err
		}
		// Printed once, to stdout only, so the operator can copy it to the
		// display. It is never written to a file or into a log record.
		fmt.Printf("generated device token for %q: %s\n", f.deviceID, f.deviceToken)
	}

	epoch, err := generateEpoch()
	if err != nil {
		return err
	}
	store, err := state.New(state.Options{
		NodeID: f.nodeID, NodeLabel: f.nodeLabel, Epoch: epoch,
	})
	if err != nil {
		return err
	}

	conn, err := mock.New(mock.Options{ID: "mock", Path: f.fixture})
	if err != nil {
		return err
	}
	sched := scheduler.New([]scheduler.Task{{
		Connector: conn,
		Policy:    scheduler.DefaultPolicy(f.interval),
		Timeout:   30 * time.Second,
		Enabled:   true,
	}}, scheduler.Options{Store: store, NodeLabel: f.nodeLabel, Logger: log})

	// Validate before listening, so the daemon never starts with bad config.
	if err := sched.ValidateAll(); err != nil {
		return fmt.Errorf("configuration is invalid: %w", err)
	}

	api, err := nodeapi.New(nodeapi.Options{
		Store:   store,
		Devices: []nodeapi.Device{{ID: f.deviceID, Token: f.deviceToken}},
		Logger:  log,
	})
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go sched.Run(ctx)

	srv := &http.Server{
		Addr:              f.addr,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	log.Info("homepi-node listening",
		"addr", f.addr, "node", f.nodeID, "epoch", epoch, "version", buildinfo.Version)

	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	log.Info("homepi-node stopped")
	return nil
}

func doctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fixture := fs.String("mock-fixture", "", "mock data file to validate")
	if err := fs.Parse(args); err != nil {
		return err
	}

	fmt.Println(buildinfo.String(binaryName))
	fmt.Println()
	fmt.Printf("user:     uid=%d euid=%d\n", os.Getuid(), os.Geteuid())
	if os.Geteuid() == 0 {
		// MOD-001 11: the daemon must run as the user that owns the CLI login
		// state, never as root.
		fmt.Println("WARNING:  running as root; homepi-node must run as the logged-in user")
	}
	host, _ := os.Hostname()
	fmt.Printf("hostname: %s\n", host)

	if *fixture != "" {
		conn, err := mock.New(mock.Options{ID: "mock", Path: *fixture})
		if err != nil {
			return err
		}
		if err := conn.ValidateConfig(); err != nil {
			fmt.Printf("fixture:  INVALID (%v)\n", err)
			return err
		}
		metrics, err := conn.Collect(context.Background())
		if err != nil {
			fmt.Printf("fixture:  INVALID (%v)\n", err)
			return err
		}
		fmt.Printf("fixture:  ok, %d metrics\n", len(metrics))
	}
	return nil
}

func newLogger(verbose bool) *slog.Logger {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}

// generateToken produces a 256-bit device token.
func generateToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate device token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// generateEpoch produces this instance's generation ID. A fresh epoch on every
// start is what lets the display accept a restarted daemon's version counter
// starting again at zero (HL-Spec 6.3).
func generateEpoch() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate epoch: %w", err)
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:16]), nil
}
