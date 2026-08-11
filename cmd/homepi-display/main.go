// Command homepi-display is the Raspberry Pi kiosk.
//
// It is a strictly output-only program: it never reads stdin, never enables
// mouse reporting and registers no key bindings, so the target device works
// with no keyboard and no touch panel (ADR-008). Local maintenance happens over
// SSH, not on the 3.5 inch screen.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/galendai/homepi-mon/internal/buildinfo"
	"github.com/galendai/homepi-mon/internal/kioskunit"
	"github.com/galendai/homepi-mon/internal/pihealth"
	"github.com/galendai/homepi-mon/internal/protocol"
	"github.com/galendai/homepi-mon/internal/snapstore"
	"github.com/galendai/homepi-mon/internal/syncclient"
	"github.com/galendai/homepi-mon/internal/tlsconfig"
	"github.com/galendai/homepi-mon/internal/ui"
)

const binaryName = "homepi-display"

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
		case "run":
			return kiosk(args[1:])
		case "service-unit":
			fmt.Print(kioskunit.SystemdUnit)
			return nil
		case "environment-example":
			fmt.Print(kioskunit.EnvironmentExample)
			return nil
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
  %s run     [flags]   run the kiosk display
  %s doctor  [flags]   print a redacted diagnostic summary
  %s service-unit      print the DietPi systemd unit
  %s environment-example
                       print the 0600 environment-file template
  %s --version         print version information

The kiosk never reads stdin and offers no on-screen controls.
`, binaryName, buildinfo.Short(), binaryName, binaryName, binaryName, binaryName, binaryName)
}

type kioskFlags struct {
	baseURL       string
	deviceID      string
	token         string
	nodeID        string
	dataDir       string
	certPin       string
	style         ui.Style
	insecureNoTLS bool
	verbose       bool
}

func parseKioskFlags(name string, args []string) (*kioskFlags, error) {
	f := &kioskFlags{}
	styleName := envOr("HOMEPI_DISPLAY_STYLE", string(ui.StyleRich))
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.StringVar(&f.baseURL, "node-url", os.Getenv("HOMEPI_NODE_URL"), "homepi-node base URL, e.g. https://dev-mac.lan:8443 (required)")
	fs.StringVar(&f.deviceID, "device-id", os.Getenv("HOMEPI_DEVICE_ID"), "this display's device ID (required)")
	fs.StringVar(&f.token, "token", "", "this display's scoped token; prefer HOMEPI_DEVICE_TOKEN")
	fs.StringVar(&f.nodeID, "node-id", os.Getenv("HOMEPI_SOURCE_NODE_ID"), "the single source node this display accepts (required)")
	fs.StringVar(&f.dataDir, "data-dir", envOr("HOMEPI_DISPLAY_DATA_DIR", defaultDataDir()), "directory for the single last-known-good snapshot")
	fs.StringVar(&f.certPin, "node-cert-pin", os.Getenv("HOMEPI_NODE_CERT_PIN"),
		"SHA-256 fingerprint of the daemon's self-signed certificate (sha256:HEX:HEX:...)")
	fs.StringVar(&styleName, "style", styleName, "display style: rich or ascii")
	fs.BoolVar(&f.insecureNoTLS, "no-tls", false,
		"allow plain HTTP to loopback only; refuse any non-loopback host (development)")
	fs.BoolVar(&f.verbose, "verbose", false, "log at debug level")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	style, err := ui.ParseStyle(styleName)
	if err != nil {
		return nil, fmt.Errorf("-style: %w", err)
	}
	f.style = style

	// Reading the token from the environment keeps it out of the process list,
	// where -token would be visible to every user on the host.
	if env := os.Getenv("HOMEPI_DEVICE_TOKEN"); env != "" {
		f.token = env
	}
	if f.baseURL == "" {
		return nil, errors.New("-node-url is required")
	}
	if f.deviceID == "" {
		return nil, errors.New("-device-id is required")
	}
	if f.nodeID == "" {
		return nil, errors.New("-node-id is required")
	}
	if f.token == "" {
		return nil, errors.New("a device token is required; set HOMEPI_DEVICE_TOKEN")
	}
	return f, nil
}

// resolveHTTPClient validates the base URL against the security policy
// and returns an http.Client ready for syncclient.Options.HTTPClient.
//
// The rules:
//   - https://host   : requires a non-empty cert pin.
//   - http://127...  : only with -no-tls, only loopback.
//   - http://other   : rejected even with -no-tls.
func resolveHTTPClient(rawURL, pin string, noTLS bool) (*http.Client, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse -node-url: %w", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		if pin == "" {
			return nil, errors.New("https requires -node-cert-pin (use `homepi-node doctor` to read the fingerprint)")
		}
		parsed, err := tlsconfig.ParseFingerprint(pin)
		if err != nil {
			return nil, fmt.Errorf("-node-cert-pin: %w", err)
		}
		transport := tlsconfig.PinningTransport(parsed)
		transport.DialContext = (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext
		transport.TLSHandshakeTimeout = 10 * time.Second
		transport.ResponseHeaderTimeout = 10 * time.Second
		return &http.Client{Transport: transport, Timeout: 30 * time.Second}, nil
	case "http":
		if !noTLS {
			return nil, errors.New("http:// is only allowed with -no-tls; use https:// + -node-cert-pin on a real LAN")
		}
		host := u.Hostname()
		if host != "127.0.0.1" && host != "::1" && host != "localhost" {
			return nil, fmt.Errorf("http://%s is not loopback; -no-tls refuses non-loopback hosts", host)
		}
		return &http.Client{Timeout: 30 * time.Second}, nil
	default:
		return nil, fmt.Errorf("unsupported scheme %q in -node-url", u.Scheme)
	}
}

func defaultDataDir() string {
	if dir, err := os.UserConfigDir(); err == nil {
		return dir + "/homepi-display"
	}
	return "./homepi-display-data"
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func kiosk(args []string) error {
	f, err := parseKioskFlags("run", args)
	if err != nil {
		return err
	}
	log := newLogger(f.verbose)

	httpClient, err := resolveHTTPClient(f.baseURL, f.certPin, f.insecureNoTLS)
	if err != nil {
		return err
	}

	binding := protocol.SourceBinding{NodeID: f.nodeID}
	store, err := snapstore.New(f.dataDir, binding)
	if err != nil {
		return err
	}
	client, err := syncclient.New(syncclient.Options{
		BaseURL: f.baseURL, DeviceID: f.deviceID, Token: f.token,
		Binding: binding, Store: store,
		HTTPClient:    httpClient,
		ClientVersion: buildinfo.Version, Logger: log,
	})
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	screen := newScreen(os.Stdout, f.style)
	defer screen.restore()
	screen.enter()
	if cols, rows, sizeErr := terminalSize(os.Stdout); sizeErr == nil &&
		(cols < ui.Cols || rows < ui.Rows) {
		screen.drawRaw(sizeDiagnostic(cols, rows))
		<-ctx.Done()
		return nil
	}

	go client.Run(ctx)

	health := pihealth.NewReader()
	// Prime the CPU sampler so the first displayed figure is a real average.
	_ = health.Read()

	return loop(ctx, client, screen, health, f.nodeID)
}

// loop redraws only when something visible changes: a new snapshot, a
// connection state change, the local minute, or a health sample. MOD-002 6
// caps normal redraws at 2 FPS and forbids timer-driven repainting of
// unchanged content.
func loop(ctx context.Context, client *syncclient.Client, screen *screen,
	health *pihealth.Reader, nodeID string) error {

	const (
		// coalesce merges a burst of updates into one repaint (MOD-002 6).
		coalesce = 200 * time.Millisecond
		// tick drives the clock and freshness labels.
		tick = 15 * time.Second
	)

	reading := health.Read()
	healthAt := time.Now()

	draw := func() {
		now := time.Now()
		if now.Sub(healthAt) >= pihealth.SampleInterval {
			reading = health.Read()
			healthAt = now
		}
		screen.draw(ui.Build(client.Snapshot(), ui.BuildOptions{
			Page:       "CODING",
			Now:        now,
			Connected:  client.Connected(),
			Thresholds: protocol.DefaultThresholds(),
			Pi: ui.PiHealth{
				TempC: reading.TempC, CPUPercent: reading.CPUPercent,
				MemPercent: reading.MemPercent, Known: reading.Known,
				LANStatus: lanStatus(client.Connected()),
			},
			RetryIn:           retryIn(client),
			Version:           buildinfo.UIVersion(),
			FallbackNodeLabel: nodeID,
		}))
	}
	draw()

	ticker := time.NewTicker(tick)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			draw()
		case <-client.Changed():
			// Wait out the rest of the burst before repainting once.
			timer := time.NewTimer(coalesce)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				return nil
			}
			drainChanges(client)
			draw()
		}
	}
}

func drainChanges(client *syncclient.Client) {
	for {
		select {
		case <-client.Changed():
		default:
			return
		}
	}
}

func lanStatus(connected bool) protocol.DisplayStatus {
	if connected {
		return protocol.DisplayOK
	}
	return protocol.DisplayError
}

func retryIn(client *syncclient.Client) *time.Duration {
	if d := client.RetryIn(); d > 0 {
		return &d
	}
	return nil
}

func doctor(args []string) error {
	fmt.Println(buildinfo.String(binaryName))
	fmt.Println()

	cols, rows, err := terminalSize(os.Stdout)
	if err != nil {
		fmt.Printf("terminal: size unavailable (%v)\n", err)
	} else {
		fmt.Printf("terminal: %dx%d (baseline %dx%d)\n", cols, rows, ui.Cols, ui.Rows)
		if cols < ui.Cols || rows < ui.Rows {
			fmt.Printf("WARNING:  terminal is smaller than the %dx%d baseline\n", ui.Cols, ui.Rows)
		}
	}

	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	dataDir := fs.String("data-dir", defaultDataDir(), "snapshot directory to inspect")
	nodeID := fs.String("node-id", "", "expected source node ID")
	if err := fs.Parse(args); err != nil {
		return err
	}

	entries, err := os.ReadDir(*dataDir)
	if err != nil {
		fmt.Printf("data dir: %s (unreadable: %v)\n", *dataDir, err)
		return nil
	}
	fmt.Printf("data dir: %s (%d files)\n", *dataDir, len(entries))
	for _, e := range entries {
		fmt.Printf("  - %s\n", e.Name())
	}

	if *nodeID != "" {
		store, err := snapstore.New(*dataDir, protocol.SourceBinding{NodeID: *nodeID})
		if err != nil {
			return err
		}
		snap, err := store.Load()
		switch {
		case errors.Is(err, snapstore.ErrNoSnapshot):
			fmt.Println("snapshot: none")
		case err != nil:
			fmt.Printf("snapshot: unreadable (%v)\n", err)
		default:
			fmt.Printf("snapshot: epoch=%s version=%d metrics=%d generated=%s\n",
				snap.SourceEpoch, snap.SnapshotVersion, len(snap.Metrics),
				snap.GeneratedAt.Format(time.RFC3339))
		}
	}
	return nil
}

func newLogger(verbose bool) *slog.Logger {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	// Diagnostics go to stderr so they never corrupt the frame on stdout.
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}
