// Package state holds the daemon's in-memory current metric state.
//
// MOD-001 8 forbids a metric history: there is no sample series, no SQLite
// table and no snapshot log. The daemon keeps exactly one current value per
// metric ID, and each daemon start creates a fresh source_epoch whose
// snapshot_version counts up from zero.
package state

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/galendai/homepi-mon/internal/protocol"
)

// Current is the daemon's single source of truth for what to publish.
// It is safe for concurrent use by the scheduler and the LAN API.
type Current struct {
	mu sync.RWMutex

	nodeID    string
	nodeLabel string
	epoch     string
	version   uint64

	// metrics keyed by metric ID; only the latest reading per ID is kept.
	metrics map[string]protocol.ProviderMetric
	// observedSeq guards against an out-of-order collector result overwriting a
	// newer one for the same metric ID (Test-Module-001 U010).
	observedSeq map[string]uint64
	// metricOwners maps each current metric to the connector that last produced
	// it. The ownership is daemon-local and never enters the wire schema; it is
	// used to annotate only the affected last-known-good values after a failure.
	metricOwners map[string]string
	health       map[string]protocol.ConnectorHealth

	now func() time.Time
}

// Options configures a Current store.
type Options struct {
	// NodeID is the stable source_node identifier. Required.
	NodeID string
	// NodeLabel is the user-facing alias shown in the TUI header.
	NodeLabel string
	// Epoch is the instance generation ID. Required; callers pass a fresh UUID
	// on every daemon start.
	Epoch string
	// Now overrides the clock in tests.
	Now func() time.Time
}

// New builds an empty current-state store.
func New(opts Options) (*Current, error) {
	if opts.NodeID == "" {
		return nil, errors.New("state: NodeID is required")
	}
	if opts.Epoch == "" {
		return nil, errors.New("state: Epoch is required")
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	label := opts.NodeLabel
	if label == "" {
		label = opts.NodeID
	}
	return &Current{
		nodeID:       opts.NodeID,
		nodeLabel:    label,
		epoch:        opts.Epoch,
		metrics:      make(map[string]protocol.ProviderMetric),
		observedSeq:  make(map[string]uint64),
		metricOwners: make(map[string]string),
		health:       make(map[string]protocol.ConnectorHealth),
		now:          now,
	}, nil
}

// Epoch returns this instance's generation ID.
func (c *Current) Epoch() string { return c.epoch }

// NodeID returns the configured source node ID.
func (c *Current) NodeID() string { return c.nodeID }

// Version returns the current snapshot version.
func (c *Current) Version() uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.version
}

// ApplyMetrics replaces the readings for the given metric IDs and bumps the
// snapshot version once for the whole batch.
//
// seq is the collector's monotonically increasing attempt counter. A result
// carrying a lower seq than one already stored for the same metric ID is
// dropped, so a slow response cannot resurrect an older value.
func (c *Current) ApplyMetrics(seq uint64, metrics []protocol.ProviderMetric) (applied int, err error) {
	return c.applyMetrics("", seq, metrics)
}

// ApplyConnectorMetrics records a successful connector batch and associates
// its metric IDs with that connector for later failure propagation.
func (c *Current) ApplyConnectorMetrics(connectorID string, seq uint64,
	metrics []protocol.ProviderMetric) (applied int, err error) {
	if connectorID == "" {
		return 0, errors.New("state: connector ID is required")
	}
	return c.applyMetrics(connectorID, seq, metrics)
}

func (c *Current) applyMetrics(connectorID string, seq uint64,
	metrics []protocol.ProviderMetric) (applied int, err error) {
	for i := range metrics {
		if verr := metrics[i].Validate(); verr != nil {
			return 0, fmt.Errorf("state: metrics[%d]: %w", i, verr)
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, m := range metrics {
		if prev, ok := c.observedSeq[m.ID]; ok && seq <= prev {
			continue
		}
		c.observedSeq[m.ID] = seq
		c.metrics[m.ID] = m
		if connectorID != "" {
			c.metricOwners[m.ID] = connectorID
		}
		applied++
	}
	if applied > 0 {
		c.version++
	}
	return applied, nil
}

// ApplyConnectorError annotates the connector's current metrics with a
// classified, redacted failure while preserving their last successful values.
// A later successful batch replaces these metrics and clears the annotation.
func (c *Current) ApplyConnectorError(connectorID string, class protocol.ErrorClass,
	message string) (changed int, err error) {
	if connectorID == "" {
		return 0, errors.New("state: connector ID is required")
	}
	if class == protocol.ErrNone || !class.Valid() {
		return 0, fmt.Errorf("state: invalid connector error class %q", class)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	for id, owner := range c.metricOwners {
		if owner != connectorID {
			continue
		}
		m := c.metrics[id]
		if m.ErrorClass == class && m.Message == message {
			continue
		}
		m.ErrorClass = class
		m.Message = message
		if verr := m.Validate(); verr != nil {
			return 0, fmt.Errorf("state: annotate metric %q: %w", id, verr)
		}
		c.metrics[id] = m
		changed++
	}
	if changed > 0 {
		c.version++
	}
	return changed, nil
}

// ApplyHealth records connector health and bumps the snapshot version.
func (c *Current) ApplyHealth(h protocol.ConnectorHealth) error {
	if err := h.Validate(); err != nil {
		return fmt.Errorf("state: %w", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.health[h.ConnectorID] = h
	c.version++
	return nil
}

// Snapshot renders the current state as a publishable MetricSnapshot.
//
// Metrics are ordered deterministically by (Order, ID) so two consecutive
// snapshots with the same data serialise identically and the display's ETag
// comparison stays meaningful.
func (c *Current) Snapshot() *protocol.MetricSnapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()

	metrics := make([]protocol.ProviderMetric, 0, len(c.metrics))
	for _, m := range c.metrics {
		metrics = append(metrics, m)
	}
	sort.Slice(metrics, func(i, j int) bool {
		if metrics[i].Order != metrics[j].Order {
			return metrics[i].Order < metrics[j].Order
		}
		return metrics[i].ID < metrics[j].ID
	})

	health := make([]protocol.ConnectorHealth, 0, len(c.health))
	for _, h := range c.health {
		health = append(health, h)
	}
	sort.Slice(health, func(i, j int) bool {
		return health[i].ConnectorID < health[j].ConnectorID
	})

	return &protocol.MetricSnapshot{
		SchemaVersion:   protocol.SchemaVersion,
		SourceEpoch:     c.epoch,
		SnapshotVersion: c.version,
		GeneratedAt:     c.now().UTC(),
		SourceNode:      c.nodeID,
		SourceNodeLabel: c.nodeLabel,
		Metrics:         metrics,
		ConnectorHealth: health,
	}
}

// HasData reports whether any metric has been collected yet.
func (c *Current) HasData() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.metrics) > 0
}
