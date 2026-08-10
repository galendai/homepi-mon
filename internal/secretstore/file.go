package secretstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// FileBackend stores every secret as a separate 0600-permission file under
// Dir. The on-disk layout is intentionally simple so it can be inspected
// and rotated by hand:
//
//	Dir/
//	  <sha256(ref)>.secret
//
// FileBackend is the explicit fallback used when the host has no keyring
// available. Every Set/Delete call prints a loud warning to stderr so the
// operator notices the downgrade in daemon logs.
type FileBackend struct {
	Dir string
}

// NewFileBackend ensures Dir exists with mode 0700 and returns the backend.
// It refuses to operate on a path outside the user's writable directories:
// the caller is expected to pass ~/.config/homepi-node/secrets or
// equivalent, never /tmp or a system path.
func NewFileBackend(dir string) (*FileBackend, error) {
	if dir == "" {
		return nil, errors.New("secretstore: file backend needs a non-empty directory")
	}
	if strings.ContainsRune(dir, 0) {
		return nil, errors.New("secretstore: file backend directory contains a NUL byte")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("secretstore: create %s: %w", dir, err)
	}
	// os.MkdirAll honours the umask, so tighten explicitly to 0700.
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, fmt.Errorf("secretstore: chmod %s: %w", dir, err)
	}
	return &FileBackend{Dir: dir}, nil
}

// Backend implements Store.
func (b *FileBackend) Backend() string { return "file-fallback" }

func (b *FileBackend) path(ref string) (string, error) {
	if err := ValidateRef(ref); err != nil {
		return "", err
	}
	// A fixed-size digest is one-to-one for practical storage purposes and
	// avoids both character-replacement collisions and filesystem name limits.
	digest := sha256.Sum256([]byte(ref))
	name := hex.EncodeToString(digest[:])
	return filepath.Join(b.Dir, name+".secret"), nil
}

// Get implements Store.
func (b *FileBackend) Get(_ context.Context, ref string) (string, error) {
	path, err := b.path(ref)
	if err != nil {
		return "", err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("secretstore: read %s: %w", path, err)
	}
	return strings.TrimRight(string(raw), "\n"), nil
}

// Set implements Store. It atomically writes a temp file, fsyncs it, then
// renames it into place and fsyncs the parent directory so the secret
// survives a power loss (snapstore uses the same pattern).
func (b *FileBackend) Set(_ context.Context, ref, value string) error {
	path, err := b.path(ref)
	if err != nil {
		return err
	}
	warnFileFallback("Set", ref)
	tmp, err := os.CreateTemp(b.Dir, ".secret-*.tmp")
	if err != nil {
		return fmt.Errorf("secretstore: create temp: %w", err)
	}
	cleanup := func() { _ = os.Remove(tmp.Name()) }
	if _, err := tmp.WriteString(value + "\n"); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("secretstore: write temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("secretstore: fsync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("secretstore: close temp: %w", err)
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		cleanup()
		return fmt.Errorf("secretstore: chmod temp: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		cleanup()
		return fmt.Errorf("secretstore: rename: %w", err)
	}
	if dir, err := os.Open(b.Dir); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}

// Delete implements Store.
func (b *FileBackend) Delete(_ context.Context, ref string) error {
	path, err := b.path(ref)
	if err != nil {
		return err
	}
	warnFileFallback("Delete", ref)
	err = os.Remove(path)
	if err == nil || errors.Is(err, fs.ErrNotExist) {
		if errors.Is(err, fs.ErrNotExist) {
			return ErrNotFound
		}
		return nil
	}
	return fmt.Errorf("secretstore: remove %s: %w", path, err)
}

// warnFileFallback prints a single warning to stderr the first time a
// file-fallback write happens in this process. It is intentionally chatty:
// the operator needs to see "the daemon downgraded to a plain file" in the
// logs to know they should configure a real keyring.
func warnFileFallback(op, ref string) {
	fmt.Fprintf(os.Stderr,
		"WARNING: secretstore %s(%q) is using the 0600-file fallback; "+
			"configure the OS keyring to avoid persisting secrets on disk\n",
		op, ref)
}
