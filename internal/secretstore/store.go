// Package secretstore persists Provider keys, device tokens and other
// secrets without ever writing them to the project repository, logs or
// long-lived configuration files.
//
// The implementation picks the best backend available on the host:
//   - macOS  : Keychain (Security.framework)
//   - Windows: Credential Manager (wincred, DPAPI-encrypted)
//   - Linux  : Secret Service over D-Bus
//
// When none of those is available (e.g. headless Linux without a running
// Secret Service), a plain-file fallback under a 0600-permission directory
// is used. The fallback is loud about its downgrade so an operator can see
// in the daemon logs that they should configure a real keyring.
//
// ADR-013: the daemon never writes back any login state and never invokes
// the official CLI. This package is therefore limited to read and write of
// explicitly stored secrets; it does not import os/exec and never touches
// ~/.codex/auth.json, ~/.kimi/* or any other vendor login file.
package secretstore

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ErrNotFound is returned when a secret reference does not exist in the
// active backend. Callers should treat it as a recoverable condition (e.g.
// surface it as "run provider add first").
var ErrNotFound = errors.New("secretstore: secret not found")

// Store is the abstract backend the daemon talks to. The interface is
// narrow on purpose: it never accepts a Provider name, only an opaque
// reference chosen by the caller, so the backend cannot leak one secret
// when asked for another.
type Store interface {
	// Get returns the secret identified by ref. ErrNotFound is returned when
	// the backend has no such entry.
	Get(ctx context.Context, ref string) (string, error)
	// Set persists value under ref, overwriting any existing entry. The
	// fallback backend writes a warning to stderr; the OS backends do not.
	Set(ctx context.Context, ref, value string) error
	// Delete removes ref. ErrNotFound is returned when ref is unknown; the
	// delete is idempotent for callers that ignore it.
	Delete(ctx context.Context, ref string) error
	// Backend returns the human-readable name of the active backend,
	// e.g. "keychain", "credmgr", "secret-service" or "file-fallback".
	// It is safe to log.
	Backend() string
}

// ValidateRef rejects references the store cannot safely handle.
//
// The reference is part of the wire-format boundary: it appears in
// config.json and in `homepi-node provider list` output, so it must not
// embed a secret value or shell metacharacters.
func ValidateRef(ref string) error {
	if ref == "" {
		return errors.New("secretstore: ref is empty")
	}
	if len(ref) > 256 {
		return errors.New("secretstore: ref longer than 256 characters")
	}
	for _, r := range ref {
		if r == ':' || r == '/' || r == '\\' {
			continue
		}
		if r >= 'a' && r <= 'z' {
			continue
		}
		if r >= 'A' && r <= 'Z' {
			continue
		}
		if r >= '0' && r <= '9' {
			continue
		}
		if r == '-' || r == '_' || r == '.' || r == '@' {
			continue
		}
		return fmt.Errorf("secretstore: ref %q contains illegal character %q", ref, r)
	}
	return nil
}

// NormalizeRef collapses a list of aliases onto a single canonical form so
// the daemon can refer to a secret by either a logical or an alias name.
// It does not touch the backend.
func NormalizeRef(ref string) string {
	return strings.TrimSpace(ref)
}
