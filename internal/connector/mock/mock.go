// Package mock provides a file-driven connector used by the Phase 1 vertical
// slice.
//
// It exists so the whole path (scheduler to LAN API to Pi to screen) can be
// exercised and manually verified with no real provider key on the machine.
// It performs no network access.
package mock

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/galendai/homepi-mon/internal/connector"
	"github.com/galendai/homepi-mon/internal/decimal"
	"github.com/galendai/homepi-mon/internal/protocol"
)

// Fixture is the on-disk mock data file. Editing it is the manual acceptance
// step for P1-03: the change must appear on the Pi screen within the spec's
// time budget.
type Fixture struct {
	Metrics    []FixtureMetric    `json:"metrics"`
	Connectors []FixtureConnector `json:"connectors"`
}

// FixtureMetric mirrors ProviderMetric but expresses time relatively, so a
// checked-in fixture never goes stale on its own.
type FixtureMetric struct {
	ID          string `json:"id"`
	Provider    string `json:"provider"`
	DisplayName string `json:"display_name"`
	Group       string `json:"group"`
	Order       int    `json:"order"`

	MetricKind protocol.MetricKind `json:"metric_kind"`
	Value      string              `json:"value"`
	Limit      string              `json:"limit,omitempty"`
	Unit       string              `json:"unit"`
	Window     protocol.Window     `json:"window"`

	// ResetsIn is a duration from collection time, e.g. "2h". Empty means the
	// upstream gives no reset time and the UI shows ROLLING.
	ResetsIn string `json:"resets_in,omitempty"`
	// ObservedAgo backdates the reading, used to exercise STALE rendering.
	ObservedAgo string `json:"observed_ago,omitempty"`
	// StaleAfter overrides the freshness budget for this metric.
	StaleAfter string `json:"stale_after,omitempty"`

	Precision  protocol.Precision    `json:"precision"`
	SourceKind protocol.SourceKind   `json:"source_kind,omitempty"`
	Status     protocol.MetricStatus `json:"status,omitempty"`
	ErrorClass protocol.ErrorClass   `json:"error_class,omitempty"`
	Message    string                `json:"message,omitempty"`
	Derived    []string              `json:"derived,omitempty"`
}

// FixtureConnector lets the fixture drive connector health independently of
// the metrics, so AUTH and error states can be demonstrated.
type FixtureConnector struct {
	ID         string                  `json:"id"`
	Provider   string                  `json:"provider"`
	Enabled    *bool                   `json:"enabled,omitempty"`
	State      protocol.ConnectorState `json:"state"`
	ErrorClass protocol.ErrorClass     `json:"error_class,omitempty"`
	Message    string                  `json:"message,omitempty"`
}

// Connector reads Fixture from a path on every Collect, so the operator can
// edit the file while the daemon runs.
type Connector struct {
	id       string
	provider string
	path     string
	now      func() time.Time
}

// Options configures the mock connector.
type Options struct {
	// ID is the connector ID; defaults to "mock".
	ID string
	// Path is the fixture file to read on each collection. Required.
	Path string
	// Now overrides the clock in tests.
	Now func() time.Time
}

// New builds a mock connector.
func New(opts Options) (*Connector, error) {
	if opts.Path == "" {
		return nil, fmt.Errorf("mock: Path is required")
	}
	id := opts.ID
	if id == "" {
		id = "mock"
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &Connector{id: id, provider: "mock", path: opts.Path, now: now}, nil
}

// ID implements connector.Connector.
func (c *Connector) ID() string { return c.id }

// Provider implements connector.Connector.
func (c *Connector) Provider() string { return c.provider }

// ValidateConfig checks the fixture parses before the daemon starts serving.
func (c *Connector) ValidateConfig() error {
	_, err := c.load()
	return err
}

// Collect reads the fixture and converts it to normalised metrics.
func (c *Connector) Collect(ctx context.Context) ([]protocol.ProviderMetric, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	fx, err := c.load()
	if err != nil {
		return nil, err
	}
	now := c.now().UTC()
	out := make([]protocol.ProviderMetric, 0, len(fx.Metrics))
	for i := range fx.Metrics {
		m, cerr := fx.Metrics[i].toMetric(now)
		if cerr != nil {
			return nil, connector.Errorf(protocol.ErrInvalidConfig, "metrics[%d]: %v", i, cerr)
		}
		out = append(out, m)
	}
	return out, nil
}

// Health converts the fixture's connector entries into health records.
func (c *Connector) Health() ([]protocol.ConnectorHealth, error) {
	fx, err := c.load()
	if err != nil {
		return nil, err
	}
	now := c.now().UTC()
	out := make([]protocol.ConnectorHealth, 0, len(fx.Connectors))
	for i := range fx.Connectors {
		fc := fx.Connectors[i]
		enabled := true
		if fc.Enabled != nil {
			enabled = *fc.Enabled
		}
		h := protocol.ConnectorHealth{
			ConnectorID: fc.ID,
			Provider:    fc.Provider,
			Enabled:     enabled,
			State:       fc.State,
			ErrorClass:  fc.ErrorClass,
			Message:     fc.Message,
		}
		if h.State == "" {
			h.State = protocol.ConnOK
		}
		if h.State == protocol.ConnOK {
			t := now
			h.LastSuccessAt = &t
		}
		if err := h.Validate(); err != nil {
			return nil, connector.Errorf(protocol.ErrInvalidConfig, "connectors[%d]: %v", i, err)
		}
		out = append(out, h)
	}
	return out, nil
}

func (c *Connector) load() (*Fixture, error) {
	raw, err := os.ReadFile(c.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, connector.Errorf(protocol.ErrInvalidConfig, "mock fixture not found: %s", c.path)
		}
		return nil, connector.Errorf(protocol.ErrInvalidConfig, "mock fixture unreadable: %s", c.path)
	}
	var fx Fixture
	if err := json.Unmarshal(raw, &fx); err != nil {
		return nil, connector.Errorf(protocol.ErrSchemaChanged, "mock fixture is not valid JSON")
	}
	return &fx, nil
}

func (f FixtureMetric) toMetric(now time.Time) (protocol.ProviderMetric, error) {
	m := protocol.ProviderMetric{
		ID:           f.ID,
		Provider:     f.Provider,
		AccountLabel: "mock",
		DisplayName:  f.DisplayName,
		Group:        f.Group,
		Order:        f.Order,
		MetricKind:   f.MetricKind,
		Unit:         f.Unit,
		Window:       f.Window,
		Precision:    f.Precision,
		SourceKind:   f.SourceKind,
		Status:       f.Status,
		ErrorClass:   f.ErrorClass,
		Message:      f.Message,
		Derived:      f.Derived,
		ObservedAt:   now,
	}
	if m.SourceKind == "" {
		m.SourceKind = protocol.SourceMock
	}
	if m.Status == "" {
		m.Status = protocol.StatusOK
	}
	if m.DisplayName == "" {
		m.DisplayName = f.Provider
	}

	if f.Value != "" {
		v, err := decimal.Parse(f.Value)
		if err != nil {
			return m, fmt.Errorf("value: %w", err)
		}
		m.Value = &v
	}
	if f.Limit != "" {
		l, err := decimal.Parse(f.Limit)
		if err != nil {
			return m, fmt.Errorf("limit: %w", err)
		}
		m.Limit = &l
	}
	if f.ResetsIn != "" {
		d, err := time.ParseDuration(f.ResetsIn)
		if err != nil {
			return m, fmt.Errorf("resets_in: %w", err)
		}
		t := now.Add(d)
		m.ResetsAt = &t
	}
	if f.ObservedAgo != "" {
		d, err := time.ParseDuration(f.ObservedAgo)
		if err != nil {
			return m, fmt.Errorf("observed_ago: %w", err)
		}
		m.ObservedAt = now.Add(-d)
	}
	if f.StaleAfter != "" {
		d, err := time.ParseDuration(f.StaleAfter)
		if err != nil {
			return m, fmt.Errorf("stale_after: %w", err)
		}
		m.StaleAfter = protocol.Duration(d)
	}
	if err := m.Validate(); err != nil {
		return m, err
	}
	return m, nil
}
