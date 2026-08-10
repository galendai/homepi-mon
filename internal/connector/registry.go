package connector

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/galendai/homepi-mon/internal/config"
	"github.com/galendai/homepi-mon/internal/protocol"
	"github.com/galendai/homepi-mon/internal/secretstore"
)

// Factory turns a ProviderConfig into a live Connector. It receives the
// secret store so the connector can resolve its SecretRef lazily during
// Collect; secrets are never stored on the Connector itself.
type Factory func(spec config.ProviderConfig, secrets secretstore.Store) (Connector, error)

// registry is the process-wide list of registered connector types.
//
// It is intentionally a global with a RWMutex rather than a context value:
// the registry is built at process start and never mutated afterwards, so
// the lock only contends during startup.
var (
	registryMu sync.RWMutex
	registry   = map[string]Factory{}
)

// Register installs a Factory for the given type ID. Registering the same
// ID twice is a programmer error and panics at startup.
func Register(typeID string, f Factory) {
	if typeID == "" {
		panic("connector: Register called with empty typeID")
	}
	if f == nil {
		panic("connector: Register called with nil factory for " + typeID)
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := registry[typeID]; exists {
		panic("connector: Register called twice for " + typeID)
	}
	registry[typeID] = f
}

// KnownTypes returns the sorted set of registered type IDs. Useful for
// config validation and for `homepi-node provider list`.
func KnownTypes() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	return out
}

// HasType reports whether typeID is registered.
func HasType(typeID string) bool {
	registryMu.RLock()
	defer registryMu.RUnlock()
	_, ok := registry[typeID]
	return ok
}

// Build looks up the factory for spec.Type and invokes it. The returned
// error is always safe to surface to the operator: it names the provider
// type and ID but never includes resolved secret values.
func Build(spec config.ProviderConfig, secrets secretstore.Store) (Connector, error) {
	if spec.Type == "" {
		return nil, errors.New("connector: provider type is empty")
	}
	registryMu.RLock()
	f, ok := registry[spec.Type]
	registryMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("connector: provider type %q is not registered", spec.Type)
	}
	return f(spec, secrets)
}

// UnimplementedFactory is the placeholder used for connector types that
// are declared in the registry but not yet implemented. P1-05 and P1-06
// overwrite these with real factories; until then `homepi-node provider
// test <id>` reports the pending status instead of trying to call the
// Provider.
func UnimplementedFactory(typeID string) Factory {
	return func(spec config.ProviderConfig, _ secretstore.Store) (Connector, error) {
		return &unimplemented{typeID: typeID, spec: spec}, nil
	}
}

type unimplemented struct {
	typeID string
	spec   config.ProviderConfig
}

func (u *unimplemented) ID() string       { return u.spec.ID }
func (u *unimplemented) Provider() string { return u.typeID }
func (u *unimplemented) ValidateConfig() error {
	// Unimplemented connectors still respect their config shape; deeper
	// checks happen in the config package.
	return nil
}
func (u *unimplemented) Collect(_ context.Context) ([]protocol.ProviderMetric, error) {
	return nil, Errorf(protocol.ErrUnsupported,
		"provider %q (type %s) will be delivered by a later Phase", u.spec.ID, u.typeID)
}

var _ Connector = (*unimplemented)(nil)
