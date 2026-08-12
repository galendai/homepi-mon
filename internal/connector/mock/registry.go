// Package mock registers the mock connector with the connector registry
// under the "mock" type ID. Real Phase 1 connectors register themselves in
// their own packages so each contract can be tested independently.
package mock

import (
	"context"

	"github.com/galendai/homepi-mon/internal/config"
	"github.com/galendai/homepi-mon/internal/connector"
	"github.com/galendai/homepi-mon/internal/providermeta"
	"github.com/galendai/homepi-mon/internal/secretstore"
)

// init wires the mock connector into the registry.
func init() {
	connector.Register("mock", factory)
	providermeta.Register(providermeta.TypeMeta{
		TypeID:           "mock",
		Label:            "Mock Fixture",
		Provider:         "mock",
		MinInterval:      5 * 1e9, // 5s
		MinStaleAfter:    5 * 1e9, // 5s
		SupportedRegions: []string{"global"},
		DefaultRegion:    "global",
		MockFixtureOnly:  true,
		MockFixtureLabel: "Mock Fixture Path",
		Description:      "Reads metrics from a local JSON fixture. No credentials, no network.",
		MetricIDResolver: func(spec config.ProviderConfig) ([]string, error) {
			c, err := New(Options{ID: spec.ID, Path: spec.MockFixture})
			if err != nil {
				return nil, err
			}
			metrics, err := c.Collect(context.Background())
			if err != nil {
				// Missing/invalid fixtures are reported by the required
				// read-only provider test, not shadow projection.
				return nil, nil
			}
			ids := make([]string, 0, len(metrics))
			for _, metric := range metrics {
				ids = append(ids, metric.ID)
			}
			return ids, nil
		},
	})
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
