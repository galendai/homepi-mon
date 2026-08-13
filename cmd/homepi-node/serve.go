package main

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/galendai/homepi-mon/internal/buildinfo"
	"github.com/galendai/homepi-mon/internal/commandbus"
	"github.com/galendai/homepi-mon/internal/config"
	"github.com/galendai/homepi-mon/internal/connector"
	"github.com/galendai/homepi-mon/internal/deviceacl"
	"github.com/galendai/homepi-mon/internal/nodeapi"
	"github.com/galendai/homepi-mon/internal/scheduler"
	"github.com/galendai/homepi-mon/internal/secretstore"
	"github.com/galendai/homepi-mon/internal/state"
	"github.com/galendai/homepi-mon/internal/tlsconfig"
)

// serveFlags contains both file-config overrides and the legacy Phase 1
// options used when no config file exists.
type serveFlags struct {
	configPath string
	addr       string
	nodeID     string
	nodeLabel  string
	deviceID   string
	deviceTok  string
	interval   time.Duration
	verbose    bool
	mockPath   string
	noTLS      bool
	explicit   map[string]bool
}

func parseServeFlags(args []string) (*serveFlags, error) {
	f := &serveFlags{}
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.StringVar(&f.configPath, "config", "",
		"path to config.json (default "+config.DefaultPath()+")")
	fs.StringVar(&f.addr, "addr", "",
		"override listen.addr from config")
	fs.StringVar(&f.nodeID, "node-id", "",
		"override source_node.id from config")
	fs.StringVar(&f.nodeLabel, "node-label", "",
		"override source_node.label from config")
	fs.StringVar(&f.deviceID, "device-id", "",
		"legacy single-device ID; required when no config file is present")
	fs.StringVar(&f.deviceTok, "device-token", "",
		"legacy single-device token; generate+print if empty")
	fs.DurationVar(&f.interval, "interval", 0,
		"override default collection interval")
	fs.StringVar(&f.mockPath, "mock-fixture", "",
		"legacy P1-03 mock fixture path; equivalent to a single mock provider")
	fs.BoolVar(&f.noTLS, "no-tls", false,
		"disable TLS even when the config requests it (development only)")
	fs.BoolVar(&f.verbose, "verbose", false, "log at debug level")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	f.explicit = make(map[string]bool)
	fs.Visit(func(fl *flag.Flag) { f.explicit[fl.Name] = true })
	return f, nil
}

// runServe loads the configuration (file or flag-only), wires every
// subsystem and blocks until the daemon is signalled to stop.
func runServe(args []string) error {
	f, err := parseServeFlags(args)
	if err != nil {
		return err
	}
	log := newLogger(f.verbose)

	cfg, fromFile, err := loadServeConfig(f)
	if err != nil {
		return err
	}
	legacyToken := ""
	if !fromFile {
		legacyToken = f.deviceTok
		if legacyToken == "" {
			legacyToken = os.Getenv("HOMEPI_DEVICE_TOKEN")
		}
		if legacyToken == "" {
			legacyToken, err = generateDeviceToken()
			if err != nil {
				return err
			}
			fmt.Printf("generated device token for %q: %s\n",
				cfg.Devices[0].ID, legacyToken)
		}
	}
	if err := validateServeConfig(cfg, !fromFile); err != nil {
		return fmt.Errorf("configuration is invalid: %w", err)
	}

	dataDir, err := defaultDataDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return fmt.Errorf("create data dir %s: %w", dataDir, err)
	}
	if err := os.Chmod(dataDir, 0o700); err != nil {
		return fmt.Errorf("chmod data dir: %w", err)
	}

	secrets, err := secretstore.OpenFromEnv(context.Background())
	if err != nil {
		return err
	}
	log.Info("secret store ready", "backend", secrets.Backend())

	devices, err := resolveDevices(cfg, secrets, legacyToken, log)
	if err != nil {
		return err
	}

	revokedPath := filepath.Join(dataDir, "revoked.json")
	acl, err := deviceacl.Load(revokedPath)
	if err != nil {
		return fmt.Errorf("revocation list: %w", err)
	}
	revokedMap := make(map[string]struct{}, acl.Len())
	for _, h := range acl.Snapshot() {
		revokedMap[h] = struct{}{}
	}

	tlsConf, err := resolveTLSConfig(cfg, dataDir, f.noTLS, log)
	if err != nil {
		return err
	}

	tasks, err := buildSchedulerTasks(cfg, secrets, log)
	if err != nil {
		return err
	}

	epoch, err := generateEpoch()
	if err != nil {
		return err
	}
	store, err := state.New(state.Options{
		NodeID:    cfg.SourceNode.ID,
		NodeLabel: cfg.SourceNode.Label,
		Epoch:     epoch,
	})
	if err != nil {
		return err
	}

	sched := scheduler.New(tasks, scheduler.Options{
		Store:          store,
		NodeLabel:      cfg.SourceNode.Label,
		MaxConcurrency: 4,
		Logger:         log,
	})

	if err := sched.ValidateAll(); err != nil {
		return fmt.Errorf("configuration is invalid: %w", err)
	}
	var commands *commandbus.Bus
	controlToken := ""
	if fromFile {
		commands, err = commandbus.Open(filepath.Join(dataDir, "commands.json"), time.Now)
		if err != nil {
			return err
		}
		controlToken, err = ensureControlToken(context.Background(), secrets)
		if err != nil {
			return err
		}
	}

	api, err := nodeapi.New(nodeapi.Options{
		Store:         store,
		Devices:       devices,
		Logger:        log,
		RevokedTokens: revokedMap,
		RevocationChecker: func(token string) (bool, error) {
			current, err := deviceacl.Load(revokedPath)
			if err != nil {
				return false, err
			}
			return current.IsRevoked(token), nil
		},
		Commands:     commands,
		ControlToken: controlToken,
		Refresh:      sched.Refresh,
		ConnectorIDs: sched.ConnectorIDs(),
	})
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go sched.Run(ctx)

	srv := &http.Server{
		Addr:              cfg.Listen.Addr,
		Handler:           api.Handler(),
		TLSConfig:         tlsConf,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	log.Info("homepi-node listening",
		"addr", cfg.Listen.Addr,
		"node", cfg.SourceNode.ID,
		"epoch", epoch,
		"config_source", map[bool]string{true: "file", false: "flags"}[fromFile],
		"providers", len(tasks),
		"version", buildinfo.Version,
	)

	serveErr := listenAndServe(srv)
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		return serveErr
	}
	log.Info("homepi-node stopped")
	return nil
}

const controlTokenRef = "keyring:homepi-remote-control"

func ensureControlToken(ctx context.Context, store secretstore.Store) (string, error) {
	value, err := store.Get(ctx, controlTokenRef)
	if err == nil {
		if len(value) < 32 {
			return "", errors.New("stored remote-control credential is invalid")
		}
		return value, nil
	}
	if !errors.Is(err, secretstore.ErrNotFound) {
		return "", fmt.Errorf("read remote-control credential: %w", err)
	}
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate remote-control credential: %w", err)
	}
	value = hex.EncodeToString(raw[:])
	if err := store.Set(ctx, controlTokenRef, value); err != nil {
		return "", fmt.Errorf("store remote-control credential: %w", err)
	}
	return value, nil
}

func listenAndServe(srv *http.Server) error {
	if srv.TLSConfig != nil {
		return srv.ListenAndServeTLS("", "")
	}
	return srv.ListenAndServe()
}

// loadServeConfig builds a Config from -config when present, otherwise
// from the legacy P1-03 flags.
func loadServeConfig(f *serveFlags) (*config.Config, bool, error) {
	if f.configPath != "" {
		c, err := config.Load(f.configPath)
		if err != nil {
			return nil, true, err
		}
		applyServeOverrides(c, f)
		return c, true, nil
	}
	if _, err := os.Stat(config.DefaultPath()); err == nil {
		c, err := config.Load(config.DefaultPath())
		if err != nil {
			return nil, true, err
		}
		applyServeOverrides(c, f)
		return c, true, nil
	}
	if f.deviceID == "" {
		return nil, false, errors.New(
			"no -config file and no -device-id; nothing to serve")
	}
	if f.nodeID == "" {
		return nil, false, errors.New("-node-id is required in legacy flag mode")
	}
	if f.mockPath == "" {
		return nil, false, errors.New("legacy flag mode requires -mock-fixture")
	}
	mockID := "mock-1"
	interval := "60s"
	if f.interval > 0 {
		interval = f.interval.String()
	}
	c := &config.Config{
		SchemaVersion: config.SchemaVersion,
		SourceNode:    config.SourceNodeConfig{ID: f.nodeID, Label: f.nodeLabel},
		Listen: config.ListenConfig{
			Addr: envOr("HOMEPI_NODE_ADDR", "127.0.0.1:8443"),
		},
		Devices: []config.DeviceConfig{{
			ID:       f.deviceID,
			TokenRef: "",
		}},
		Providers: []config.ProviderConfig{{
			ID:           mockID,
			Type:         "mock",
			AccountLabel: "demo",
			Region:       "global",
			Interval:     interval,
			StaleAfter:   "5m",
			MockFixture:  f.mockPath,
		}},
	}
	if c.SourceNode.Label == "" {
		c.SourceNode.Label = c.SourceNode.ID
	}
	c.ApplyDefaults()
	applyServeOverrides(c, f)
	return c, false, nil
}

// applyServeOverrides merges only flags explicitly supplied by the operator.
// Device and fixture flags remain exclusive to the legacy no-config mode.
func applyServeOverrides(c *config.Config, f *serveFlags) {
	if f.explicit["addr"] {
		c.Listen.Addr = f.addr
	}
	if f.explicit["node-id"] {
		c.SourceNode.ID = f.nodeID
	}
	if f.explicit["node-label"] {
		c.SourceNode.Label = f.nodeLabel
	}
	if f.explicit["interval"] {
		for i := range c.Providers {
			c.Providers[i].Interval = f.interval.String()
		}
	}
}

// validateServeConfig preserves strict file validation while allowing the
// legacy environment/flag token to be resolved outside the on-disk schema.
func validateServeConfig(c *config.Config, allowLegacyToken bool) error {
	if !allowLegacyToken {
		return c.ValidateWithRegistry(stringSet(connector.KnownTypes()))
	}
	clone := *c
	clone.Devices = append([]config.DeviceConfig(nil), c.Devices...)
	for i := range clone.Devices {
		if clone.Devices[i].TokenRef == "" {
			clone.Devices[i].TokenRef = "keyring:legacy-device-token"
		}
	}
	return clone.ValidateWithRegistry(stringSet(connector.KnownTypes()))
}

// resolveDevices resolves each device's token_ref via the secret store
// and falls back to the legacy HOMEPI_DEVICE_TOKEN for the only device
// when the entry has no ref (the P1-03 compatibility path).
func resolveDevices(cfg *config.Config, secrets secretstore.Store,
	legacyToken string, log *slog.Logger) ([]nodeapi.Device, error) {

	out := make([]nodeapi.Device, 0, len(cfg.Devices))
	for i, d := range cfg.Devices {
		if d.TokenRef == "" {
			if legacyToken == "" {
				return nil, fmt.Errorf("device[%d] %q has no token_ref; "+
					"set HOMEPI_DEVICE_TOKEN or run `device add`", i, d.ID)
			}
			out = append(out, nodeapi.Device{ID: d.ID, Token: legacyToken})
			continue
		}
		v, err := secrets.Get(context.Background(), d.TokenRef)
		if err != nil {
			return nil, fmt.Errorf("device %q token_ref %q: %w",
				d.ID, d.TokenRef, err)
		}
		log.Info("device token resolved", "device", d.ID)
		out = append(out, nodeapi.Device{ID: d.ID, Token: v})
	}
	return out, nil
}

// resolveTLSConfig loads or generates the certificate and returns a
// *tls.Config suitable for the HTTP server. When the operator passes
// -no-tls, the function returns nil and the server falls back to plain
// HTTP for local development.
func resolveTLSConfig(cfg *config.Config, dataDir string, noTLS bool,
	log *slog.Logger) (*tls.Config, error) {

	if noTLS {
		log.Info("TLS disabled by -no-tls (development only)")
		return nil, nil
	}
	certPath := cfg.Listen.TLS.Cert
	keyPath := cfg.Listen.TLS.Key
	var certPEM, keyPEM []byte
	var err error
	if certPath == "auto" && keyPath == "auto" {
		certPEM, keyPEM, err = tlsconfig.EnsureCert(dataDir, cfg.SourceNode.ID, 0)
		certPath = filepath.Join(dataDir, "cert.pem")
	} else {
		if certPath == "" || keyPath == "" || certPath == "auto" || keyPath == "auto" {
			return nil, errors.New("listen.tls.cert and listen.tls.key must both be auto or explicit paths")
		}
		certPEM, err = os.ReadFile(certPath)
		if err == nil {
			keyPEM, err = os.ReadFile(keyPath)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("load TLS certificate: %w", err)
	}
	tlsConf, err := tlsconfig.ServerTLSConfig(certPEM, keyPEM)
	if err != nil {
		return nil, err
	}
	fp, err := tlsconfig.Fingerprint(certPath)
	if err != nil {
		return nil, err
	}
	log.Info("TLS ready", "fingerprint", fp)
	return tlsConf, nil
}

// buildSchedulerTasks turns each provider in the config into a scheduler
// task via the connector registry.
func buildSchedulerTasks(cfg *config.Config, secrets secretstore.Store,
	log *slog.Logger) ([]scheduler.Task, error) {

	tasks := make([]scheduler.Task, 0, len(cfg.Providers))
	for _, p := range cfg.Providers {
		c, err := connector.Build(p, secrets)
		if err != nil {
			return nil, fmt.Errorf("provider %q: %w", p.ID, err)
		}
		interval, err := p.IntervalDuration()
		if err != nil {
			return nil, err
		}
		if !p.IsEnabled() {
			log.Info("provider disabled", "id", p.ID)
		}
		staleAfter, err := p.StaleAfterDuration()
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, scheduler.Task{
			Connector:  c,
			Policy:     scheduler.DefaultPolicy(interval),
			Timeout:    30 * time.Second,
			Enabled:    p.IsEnabled(),
			StaleAfter: staleAfter,
		})
	}
	return tasks, nil
}

// defaultDataDir is the per-user directory the daemon writes generated
// state (revoked list, certificate) into.
func defaultDataDir() (string, error) {
	if v := os.Getenv("HOMEPI_NODE_DATA_DIR"); v != "" {
		return v, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate user config dir: %w", err)
	}
	return filepath.Join(base, "homepi-node"), nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func stringSet(xs []string) map[string]bool {
	out := make(map[string]bool, len(xs))
	for _, x := range xs {
		out[x] = true
	}
	return out
}

// generateEpoch produces this instance's generation ID. A fresh epoch on
// every start is what lets the display accept a restarted daemon's
// version counter starting again at zero (HL-Spec 6.3).
func generateEpoch() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate epoch: %w", err)
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x",
		buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:16]), nil
}
