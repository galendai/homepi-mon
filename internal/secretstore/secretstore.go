package secretstore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ServicePrefix is the keyring service identifier shared by every HomePi
// component. It is constant so a previously granted keychain entry can
// still be looked up after an upgrade.
const ServicePrefix = "github.com/galendai/homepi-mon"

// Open picks the best available backend.
//
// The probe order is OS keyring first; the file backend is only used when
// the keyring is unavailable. When the daemon falls back to file, the
// caller is expected to surface the warning (the FileBackend already prints
// it on every Set/Delete).
func Open(ctx context.Context, opts Options) (Store, error) {
	if opts.FileDir == "" {
		return nil, errors.New("secretstore: Options.FileDir is required")
	}
	if kr, err := newKeyringBackend(ServicePrefix, probeRef(opts.DataDir)); err == nil {
		return kr, nil
	}
	// Keyring probe failed; degrade to the file backend.
	return NewFileBackend(opts.FileDir)
}

// Options configures Open.
type Options struct {
	// FileDir is the directory used when the OS keyring is unavailable.
	// It must be writable by the current user; typically
	// ~/.config/homepi-node/secrets.
	FileDir string
	// DataDir is used only to derive a stable probe reference name so the
	// daemon's own keyring entry does not collide between installations.
	DataDir string
}

// probeRef returns a reference name safe to write and remove on every
// Open, regardless of whether the underlying backend actually has a delete
// implementation. It contains the absolute path of DataDir so two HomePi
// installations on the same machine never collide.
func probeRef(dataDir string) string {
	if dataDir == "" {
		return "probe"
	}
	clean := filepath.Clean(dataDir)
	// Keyring implementations cap user-name length at ~255; keep us well
	// under that limit by hashing long paths.
	if len(clean) > 64 {
		return "probe-" + shortHash(clean)
	}
	return "probe-" + filepath.Base(clean)
}

// shortHash is a tiny FNV-1a hex digest that avoids importing crypto just
// for a 16-character suffix. It is not used for any security-sensitive
// decision; it only keeps the keyring reference name short and stable.
func shortHash(s string) string {
	const (
		offset uint64 = 1469598103934665603
		prime  uint64 = 1099511628211
	)
	h := offset
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= prime
	}
	const hexdigits = "0123456789abcdef"
	out := make([]byte, 16)
	for i := 15; i >= 0; i-- {
		out[i] = hexdigits[h&0x0f]
		h >>= 4
	}
	return string(out)
}

// OpenFromEnv reads HOMEPI_SECRET_DIR and HOMEPI_DATA_DIR (optional) and
// returns an Open store. It exists so cmd/homepi-node and tests can both
// construct a Store without duplicating path logic.
func OpenFromEnv(ctx context.Context) (Store, error) {
	dir := os.Getenv("HOMEPI_SECRET_DIR")
	if dir == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return nil, fmt.Errorf("secretstore: locate config dir: %w", err)
		}
		dir = filepath.Join(base, "homepi-node", "secrets")
	}
	return Open(ctx, Options{
		FileDir: dir,
		DataDir: os.Getenv("HOMEPI_DATA_DIR"),
	})
}
