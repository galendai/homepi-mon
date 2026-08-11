// Package mock registers the mock connector with the connector registry
// under the "mock" type ID. Real Phase 1 connectors register themselves in
// their own packages so each contract can be tested independently.
package mock

import (
	"github.com/galendai/homepi-mon/internal/config"
	"github.com/galendai/homepi-mon/internal/connector"
	"github.com/galendai/homepi-mon/internal/secretstore"
)

// init wires the mock connector into the registry.
func init() {
	connector.Register("mock", factory)
}

// factory turns the ProviderConfig for a mock entry into a Connector.
// Only the fields the mock connector understands are forwarded; anything
// else (region, base_url, secret_ref) is silently ignored because the
// mock never touches the network.
func factory(spec config.ProviderConfig, _ secretstore.Store) (connector.Connector, error) {
	return New(Options{
		ID:   spec.ID,
		Path: spec.MockFixture,
	})
}
