package protocol

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var errInvalidDuration = errors.New("protocol: invalid duration")

// Validation errors callers can match on.
var (
	// ErrSchemaMajor means the payload's major schema version is not supported
	// and the client must show an upgrade prompt instead of rendering data.
	ErrSchemaMajor = errors.New("protocol: unsupported schema major version")
	// ErrWrongSource means the snapshot came from a node this device is not
	// bound to. Phase 1 binds exactly one node (ADR-014).
	ErrWrongSource = errors.New("protocol: snapshot from unbound source node")
	// ErrVersionRegressed means an older snapshot arrived for the same epoch.
	ErrVersionRegressed = errors.New("protocol: snapshot version regressed")
)

// idPattern allows the conservative identifier charset we use for node,
// device, connector and metric IDs. It keeps IDs safe for paths, logs and the
// ASCII grid without needing per-call escaping.
func validID(field, v string) error {
	if v == "" {
		return fmt.Errorf("%s: must not be empty", field)
	}
	if len(v) > 64 {
		return fmt.Errorf("%s: longer than 64 characters", field)
	}
	for _, r := range v {
		ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.'
		if !ok {
			return fmt.Errorf("%s: contains disallowed character %q", field, r)
		}
	}
	return nil
}

// safeText rejects control characters in any user- or upstream-supplied string
// that can reach the terminal (UI-001 9, MOD-004 message rules).
func safeText(field, v string, maxLen int) error {
	if len(v) > maxLen {
		return fmt.Errorf("%s: longer than %d characters", field, maxLen)
	}
	for _, r := range v {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("%s: contains control character %s", field, strconv.QuoteRune(r))
		}
	}
	return nil
}

// CheckSchema verifies the payload's schema version is one this build can read.
func CheckSchema(version string) error {
	if version == "" {
		return fmt.Errorf("schema_version: must not be empty")
	}
	majorText, _, _ := strings.Cut(version, ".")
	major, err := strconv.Atoi(majorText)
	if err != nil {
		return fmt.Errorf("schema_version: %q is not major.minor", version)
	}
	if major != SchemaMajor {
		return fmt.Errorf("%w: got %d, this build supports %d", ErrSchemaMajor, major, SchemaMajor)
	}
	return nil
}

// Validate checks structural invariants of a snapshot.
//
// It is deliberately strict about identifiers, enums and control characters,
// and deliberately tolerant about unknown JSON fields, which the decoder drops
// so that a newer minor schema still renders (HL-Spec 6.3).
func (s *MetricSnapshot) Validate() error {
	if s == nil {
		return errors.New("snapshot: nil")
	}
	if err := CheckSchema(s.SchemaVersion); err != nil {
		return err
	}
	if err := validID("source_epoch", s.SourceEpoch); err != nil {
		return err
	}
	if err := validID("source_node", s.SourceNode); err != nil {
		return err
	}
	if err := safeText("source_node_label", s.SourceNodeLabel, 64); err != nil {
		return err
	}
	if s.GeneratedAt.IsZero() {
		return errors.New("generated_at: must be set")
	}
	seen := make(map[string]bool, len(s.Metrics))
	for i := range s.Metrics {
		if err := s.Metrics[i].Validate(); err != nil {
			return fmt.Errorf("metrics[%d]: %w", i, err)
		}
		if seen[s.Metrics[i].ID] {
			return fmt.Errorf("metrics[%d]: duplicate id %q", i, s.Metrics[i].ID)
		}
		seen[s.Metrics[i].ID] = true
	}
	for i := range s.ConnectorHealth {
		if err := s.ConnectorHealth[i].Validate(); err != nil {
			return fmt.Errorf("connector_health[%d]: %w", i, err)
		}
	}
	return nil
}

// Validate checks a single metric.
func (m *ProviderMetric) Validate() error {
	if err := validID("id", m.ID); err != nil {
		return err
	}
	if err := validID("provider", m.Provider); err != nil {
		return err
	}
	if err := safeText("account_label", m.AccountLabel, 64); err != nil {
		return err
	}
	if err := safeText("display_name", m.DisplayName, 64); err != nil {
		return err
	}
	if err := safeText("unit", m.Unit, 16); err != nil {
		return err
	}
	if err := safeText("message", m.Message, 160); err != nil {
		return err
	}
	if !m.MetricKind.Valid() {
		return enumError("metric_kind", m.MetricKind)
	}
	if !m.Window.Valid() {
		return enumError("window", m.Window)
	}
	if !m.Precision.Valid() {
		return enumError("precision", m.Precision)
	}
	if !m.SourceKind.Valid() {
		return enumError("source_kind", m.SourceKind)
	}
	if !m.Status.Valid() {
		return enumError("status", m.Status)
	}
	if m.ErrorClass != ErrNone && !m.ErrorClass.Valid() {
		return enumError("error_class", m.ErrorClass)
	}
	if m.ObservedAt.IsZero() {
		return errors.New("observed_at: must be set")
	}
	if m.Unit == "" {
		return errors.New("unit: must not be empty")
	}
	return nil
}

// Validate checks a connector health entry.
func (c *ConnectorHealth) Validate() error {
	if err := validID("connector_id", c.ConnectorID); err != nil {
		return err
	}
	if err := validID("provider", c.Provider); err != nil {
		return err
	}
	if !c.State.Valid() {
		return enumError("state", c.State)
	}
	if c.ErrorClass != ErrNone && !c.ErrorClass.Valid() {
		return enumError("error_class", c.ErrorClass)
	}
	if err := safeText("message", c.Message, 160); err != nil {
		return err
	}
	if c.ConsecutiveFailures < 0 {
		return errors.New("consecutive_failures: must not be negative")
	}
	return nil
}

// SourceBinding pins a display device to exactly one homepi-node (ADR-014).
type SourceBinding struct {
	NodeID string
}

// Accept validates that a snapshot originates from the bound node.
// A snapshot from any other node is rejected rather than merged (MOD-002 8).
func (b SourceBinding) Accept(s *MetricSnapshot) error {
	if b.NodeID == "" {
		return errors.New("source binding: no node configured")
	}
	if s == nil {
		return errors.New("source binding: nil snapshot")
	}
	if s.SourceNode != b.NodeID {
		return fmt.Errorf("%w: got %q, bound to %q", ErrWrongSource, s.SourceNode, b.NodeID)
	}
	return nil
}

// Supersedes reports whether next may replace prev.
//
// Within one source_epoch only strictly increasing snapshot versions are
// accepted. A new epoch means the daemon restarted, so its version counter
// legitimately restarts at zero and the full snapshot is accepted
// (HL-Spec 6.3, MOD-002 7).
func Supersedes(prev, next *MetricSnapshot) (bool, error) {
	if next == nil {
		return false, errors.New("supersedes: nil candidate")
	}
	if prev == nil {
		return true, nil
	}
	if prev.SourceEpoch != next.SourceEpoch {
		return true, nil
	}
	if next.SnapshotVersion > prev.SnapshotVersion {
		return true, nil
	}
	return false, fmt.Errorf("%w: got version %d, holding %d in epoch %s",
		ErrVersionRegressed, next.SnapshotVersion, prev.SnapshotVersion, prev.SourceEpoch)
}

// Age returns how long ago the snapshot was generated relative to now.
func (s *MetricSnapshot) Age(now time.Time) time.Duration {
	return now.Sub(s.GeneratedAt)
}
