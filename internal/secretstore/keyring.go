package secretstore

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/zalando/go-keyring"
)

// keyringBackend wraps the OS keyring via zalando/go-keyring. It picks a
// service prefix so multiple HomePi installations on the same machine do
// not collide, and never returns the raw keyring error to callers so we
// can downgrade to ErrNotFound uniformly.
type keyringBackend struct {
	service string
}

// newKeyringBackend probes the keyring once; if Set succeeds it is also
// implicitly available for Get/Delete, so the probe exercises the same code
// path the rest of the daemon will use.
func newKeyringBackend(service, probeRef string) (*keyringBackend, error) {
	if err := keyring.Set(service, probeRef, "homepi-mon-probe"); err != nil {
		return nil, fmt.Errorf("secretstore: keyring probe failed: %w", err)
	}
	// Best-effort cleanup of the probe entry. If the platform requires
	// elevated privileges for delete (rare), the entry is harmless: it
	// lives under our service prefix and cannot collide with anything else.
	_ = keyring.Delete(service, probeRef)
	return &keyringBackend{service: service}, nil
}

// Backend implements Store. It returns the OS-appropriate label
// (keychain / credmgr / secret-service) regardless of which zalando
// go-keyring sub-package was selected at build time.
func (b *keyringBackend) Backend() string { return platformKeyringName() }

// Get implements Store.
func (b *keyringBackend) Get(_ context.Context, ref string) (string, error) {
	if err := ValidateRef(ref); err != nil {
		return "", err
	}
	v, err := keyring.Get(b.service, ref)
	if err != nil {
		if isKeyringNotFound(err) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("secretstore: keyring get %q: %w", ref, err)
	}
	return v, nil
}

// Set implements Store.
func (b *keyringBackend) Set(_ context.Context, ref, value string) error {
	if err := ValidateRef(ref); err != nil {
		return err
	}
	if err := keyring.Set(b.service, ref, value); err != nil {
		return fmt.Errorf("secretstore: keyring set %q: %w", ref, err)
	}
	return nil
}

// Delete implements Store.
func (b *keyringBackend) Delete(_ context.Context, ref string) error {
	if err := ValidateRef(ref); err != nil {
		return err
	}
	if err := keyring.Delete(b.service, ref); err != nil {
		if isKeyringNotFound(err) {
			return ErrNotFound
		}
		return fmt.Errorf("secretstore: keyring delete %q: %w", ref, err)
	}
	return nil
}

// isKeyringNotFound normalises the various "no such entry" errors the
// different OS backends return. The string match is intentional: the
// upstream library does not export typed errors and we want to keep the
// surface narrow.
func isKeyringNotFound(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not found") ||
		strings.Contains(msg, "no such") ||
		strings.Contains(msg, "cannot find") ||
		errors.Is(err, ErrNotFound)
}

// platformKeyringName returns the OS-appropriate keyring label without
// touching the real keyring. It mirrors the selection zalando/go-keyring
// makes at build time so the operator output stays consistent.
func platformKeyringName() string {
	switch platform() {
	case "darwin":
		return "keychain"
	case "windows":
		return "credmgr"
	default:
		return "secret-service"
	}
}
