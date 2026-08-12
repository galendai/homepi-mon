// Package configtx owns the Config Transaction Service. The Service is
// the single entry point used by both the CLI subcommands and the
// Web Admin to load, edit, test and apply Provider configuration in a
// transactional way.
//
// Phase 2 makes the Service the only place that mutates
// config.json and the keyring; both call sites (CLI and the Web Admin
// in P2-02) must go through the Service. The package never imports the
// Web Admin or the connector packages, so the dependency graph stays
// one-way.
package configtx

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/galendai/homepi-mon/internal/config"
	"github.com/galendai/homepi-mon/internal/connector"
	"github.com/galendai/homepi-mon/internal/providermeta"
	"github.com/galendai/homepi-mon/internal/secretstore"
)

// Options configures a Service instance. A zero Options is rejected so
// the caller has to be explicit about the on-disk paths and the
// secret store.
type Options struct {
	// ConfigPath is the absolute path to the daemon config.json. The
	// Service reads and writes it transactionally.
	ConfigPath string
	// SecretDir is the file-fallback secret directory. The Service
	// resolves a keyring Store on construction.
	SecretDir string
	// DataDir is used to derive the per-installation keyring probe
	// reference, keeping multiple installs on the same machine apart.
	DataDir string
	// ServiceLabel is the human-readable service name written to the
	// audit summary (e.g. "homepi-node"). Not used for any binding
	// decision.
	ServiceLabel string
	// ApplyTimeout bounds the whole Apply pipeline. The default
	// (45s) is enough for save + service restart + health check on
	// macOS; Linux and Windows can be slower on first invocation.
	ApplyTimeout time.Duration
	// Now is injectable for tests; production code uses time.Now.
	Now func() time.Time
	// SecretStore overrides the platform store in tests. Production
	// callers leave it nil so the configured OS/file backend is used.
	SecretStore secretstore.Store
}

// Service is the single owner of the configuration transaction. It
// serialises Apply calls through an internal mutex and exposes a
// Draft handle to callers that need staged edits.
type Service struct {
	opts Options

	// mu serialises mutating calls. It is intentionally non-reentrant:
	// every public method takes the write lock while it runs.
	mu sync.Mutex

	// baseRevision is the content-derived revision observed after the
	// latest successful load or Apply.
	baseRevision string
	// lastApply is the timestamp + result of the most recent Apply
	// call. It is used to short-circuit the Web Admin's "last apply"
	// banner without re-reading the config.
	lastApply applyRecord
}

type applyRecord struct {
	at      time.Time
	status  string
	summary string
}

// New constructs a Service. It opens the secret store eagerly so a
// misconfigured environment fails at startup, not at the first Apply.
func New(ctx context.Context, opts Options) (*Service, error) {
	if opts.ConfigPath == "" {
		return nil, fmt.Errorf("configtx: Options.ConfigPath is required")
	}
	if opts.SecretDir == "" {
		return nil, fmt.Errorf("configtx: Options.SecretDir is required")
	}
	if opts.ServiceLabel == "" {
		opts.ServiceLabel = "homepi-node"
	}
	if opts.ApplyTimeout <= 0 {
		opts.ApplyTimeout = 45 * time.Second
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	store, err := openConfiguredStore(ctx, opts)
	if err != nil {
		return nil, err
	}
	// Touch the store so the operator sees the backend in logs even
	// before the first draft.
	_ = store.Backend()
	revision, err := revisionForDisk(opts.ConfigPath)
	if err != nil {
		return nil, err
	}
	s := &Service{opts: opts, baseRevision: revision}
	return s, nil
}

// ConfigPath returns the absolute config path the Service operates on.
func (s *Service) ConfigPath() string { return s.opts.ConfigPath }

// SecretBackend returns the resolved secret-store backend name, e.g.
// "keychain" or "file-fallback".
func (s *Service) SecretBackend() string {
	// Open is idempotent and cheap; the Service keeps no Store handle
	// to keep constructor and Apply independent. Callers that need a
	// long-lived Store can call secretstore.Open directly.
	store, err := s.openStore(context.Background())
	if err != nil {
		return "unavailable"
	}
	return store.Backend()
}

// OpenDraft loads the on-disk config (or an empty Config when the file
// does not yet exist) and returns a Draft bound to this Service. The
// returned revision matches the next EditProvider invocation's
// expectation; Apply verifies the disk revision still matches the draft
// before mutating anything.
func (s *Service) OpenDraft(ctx context.Context) (*Draft, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	base, err := loadOrEmpty(s.opts.ConfigPath)
	if err != nil {
		return nil, err
	}
	revision, err := revisionForDisk(s.opts.ConfigPath)
	if err != nil {
		return nil, err
	}
	s.baseRevision = revision
	return newDraft(s, base, revision), nil
}

// Now exposes the Service's clock for tests.
func (s *Service) Now() time.Time { return s.opts.Now() }

// revisionForDisk binds a draft to the exact bytes currently stored at
// ConfigPath. External editors and other processes therefore invalidate
// an already-open draft even when they do not share this Service instance.
func revisionForDisk(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	body, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("configtx: read revision %s: %w", path, err)
		}
		body = []byte("<missing>")
	}
	payload := append(append([]byte(abs), 0), body...)
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:]), nil
}

func openConfiguredStore(ctx context.Context, opts Options) (secretstore.Store, error) {
	if opts.SecretStore != nil {
		return opts.SecretStore, nil
	}
	return secretstore.Open(ctx, secretstore.Options{
		FileDir: opts.SecretDir,
		DataDir: opts.DataDir,
	})
}

func (s *Service) openStore(ctx context.Context) (secretstore.Store, error) {
	return openConfiguredStore(ctx, s.opts)
}

// loadOrEmpty mirrors cmd/homepi-node/provider.go:loadOrEmpty but is
// duplicated here so this package has no upward dependency on cmd.
func loadOrEmpty(path string) (*config.Config, error) {
	if _, err := os.Stat(path); err == nil {
		return config.Load(path)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("configtx: stat %s: %w", path, err)
	}
	c := &config.Config{SchemaVersion: config.SchemaVersion}
	c.ApplyDefaults()
	return c, nil
}

// knownTypeSet returns the registered Provider types as a map for the
// ValidateWithRegistry call.
func knownTypeSet() map[string]bool {
	out := make(map[string]bool, len(connector.KnownTypes()))
	for _, t := range connector.KnownTypes() {
		out[t] = true
	}
	return out
}

// knownMetaSet returns the TypeMeta registry as a map for field checks.
func knownMetaSet() map[string]providermeta.TypeMeta {
	out := make(map[string]providermeta.TypeMeta, len(providermeta.Known()))
	for _, m := range providermeta.Known() {
		out[m.TypeID] = m
	}
	return out
}
