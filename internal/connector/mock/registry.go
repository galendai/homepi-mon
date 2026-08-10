// Package mock registers the mock connector with the connector registry
// under the "mock" type ID. The mock is the only fully working Phase 1
// provider; everything else (minimax_coding, codex_usage, etc.) registers
// an unimplemented placeholder so the CLI commands already exist but the
// daemon will surface "N/A" until P1-05 and P1-06 land.
package mock

import (
	"github.com/galendai/homepi-mon/internal/config"
	"github.com/galendai/homepi-mon/internal/connector"
	"github.com/galendai/homepi-mon/internal/secretstore"
)

// init wires the mock connector into the registry and reserves the slot
// for the real connectors that P1-05 / P1-06 will implement.
func init() {
	connector.Register("mock", factory)
	for _, id := range []string{
		"minimax_coding",
		"codex_usage",
		"kimi_coding",
		"deepseek_api",
		"kimi_api",
	} {
		connector.Register(id, connector.UnimplementedFactory(id))
	}
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
