package protocol

import (
	"encoding/json"
	"time"

	"github.com/galendai/homepi-mon/internal/decimal"
)

// SchemaVersion is the wire schema this build produces (HL-Spec 6.3).
// Major version changes are breaking; a client that does not support the major
// version must refuse the payload and show an upgrade prompt.
const SchemaVersion = "1.1"

// SchemaMajor is the major component of SchemaVersion.
const SchemaMajor = 1

// MetricSnapshot is the full current state published by one homepi-node
// instance (HL-Spec 5.1).
//
// It carries no credentials by construction: there is no field for a token,
// cookie, authorization header, auth.json content or raw provider response.
type MetricSnapshot struct {
	SchemaVersion   string            `json:"schema_version"`
	SourceEpoch     string            `json:"source_epoch"`
	SnapshotVersion uint64            `json:"snapshot_version"`
	GeneratedAt     time.Time         `json:"generated_at"`
	SourceNode      string            `json:"source_node"`
	SourceNodeLabel string            `json:"source_node_label,omitempty"`
	Metrics         []ProviderMetric  `json:"metrics"`
	ConnectorHealth []ConnectorHealth `json:"connector_health"`
	HomeLabNodes    []HomeLabNode     `json:"homelab_nodes,omitempty"`
	HomeLabServices []HomeLabService  `json:"homelab_services,omitempty"`
}

// ProviderMetric is one normalised reading (HL-Spec 5.2).
type ProviderMetric struct {
	ID           string           `json:"id"`
	Provider     string           `json:"provider"`
	AccountLabel string           `json:"account_label"`
	DisplayName  string           `json:"display_name"`
	MetricKind   MetricKind       `json:"metric_kind"`
	Value        *decimal.Decimal `json:"value,omitempty"`
	Limit        *decimal.Decimal `json:"limit,omitempty"`
	Unit         string           `json:"unit"`
	Window       Window           `json:"window"`
	ResetsAt     *time.Time       `json:"resets_at,omitempty"`
	ObservedAt   time.Time        `json:"observed_at"`
	Precision    Precision        `json:"precision"`
	SourceKind   SourceKind       `json:"source_kind"`
	Status       MetricStatus     `json:"status"`
	// StaleAfter is the configured freshness budget for this metric. Zero means
	// the consumer falls back to its own default.
	StaleAfter Duration `json:"stale_after,omitempty"`
	// ErrorClass is set when the last collection attempt failed and the value
	// shown is the previous one.
	ErrorClass ErrorClass `json:"error_class,omitempty"`
	// Derived marks fields the daemon computed rather than received
	// (MOD-001 7), e.g. "remaining", "percent".
	Derived []string `json:"derived,omitempty"`
	// Message is a short, already-redacted, user-presentable note.
	Message string `json:"message,omitempty"`
	// Order is the user-configured display position; lower sorts first.
	Order int `json:"order,omitempty"`
	// Group buckets the metric into a UI section, e.g. "coding" or "api".
	Group string `json:"group,omitempty"`
}

type monetaryDecimal struct {
	value decimal.Decimal
}

func (d monetaryDecimal) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.value.Rescale(2))
}

// MarshalJSON keeps exact decimals for non-monetary metrics while enforcing
// the public two-decimal contract for balance and cost values.
func (m ProviderMetric) MarshalJSON() ([]byte, error) {
	type metricAlias ProviderMetric
	if m.MetricKind != KindBalance && m.MetricKind != KindCost {
		return json.Marshal(metricAlias(m))
	}
	out := struct {
		metricAlias
		Value *monetaryDecimal `json:"value,omitempty"`
		Limit *monetaryDecimal `json:"limit,omitempty"`
	}{metricAlias: metricAlias(m)}
	if m.Value != nil {
		out.Value = &monetaryDecimal{value: *m.Value}
	}
	if m.Limit != nil {
		out.Limit = &monetaryDecimal{value: *m.Limit}
	}
	return json.Marshal(out)
}

// ConnectorHealth reports scheduler-visible connector state (MOD-001 4).
type ConnectorHealth struct {
	ConnectorID         string         `json:"connector_id"`
	Provider            string         `json:"provider"`
	Enabled             bool           `json:"enabled"`
	State               ConnectorState `json:"state"`
	LastAttemptAt       *time.Time     `json:"last_attempt_at,omitempty"`
	LastSuccessAt       *time.Time     `json:"last_success_at,omitempty"`
	NextAttemptAt       *time.Time     `json:"next_attempt_at,omitempty"`
	ConsecutiveFailures int            `json:"consecutive_failures"`
	ErrorClass          ErrorClass     `json:"error_class,omitempty"`
	// Message is a redacted classification such as
	// "re-authenticate with the official CLI on this host".
	Message string `json:"message,omitempty"`
}

// Clone returns a deep copy so callers cannot mutate shared daemon state.
func (s *MetricSnapshot) Clone() *MetricSnapshot {
	if s == nil {
		return nil
	}
	out := *s
	out.Metrics = make([]ProviderMetric, len(s.Metrics))
	for i, m := range s.Metrics {
		out.Metrics[i] = m.clone()
	}
	out.ConnectorHealth = make([]ConnectorHealth, len(s.ConnectorHealth))
	copy(out.ConnectorHealth, s.ConnectorHealth)
	out.HomeLabNodes = make([]HomeLabNode, len(s.HomeLabNodes))
	for i, node := range s.HomeLabNodes {
		out.HomeLabNodes[i] = node.clone()
	}
	out.HomeLabServices = make([]HomeLabService, len(s.HomeLabServices))
	for i, service := range s.HomeLabServices {
		out.HomeLabServices[i] = service.clone()
	}
	return &out
}

func (m ProviderMetric) clone() ProviderMetric {
	out := m
	if m.Value != nil {
		v := *m.Value
		out.Value = &v
	}
	if m.Limit != nil {
		l := *m.Limit
		out.Limit = &l
	}
	if m.ResetsAt != nil {
		r := *m.ResetsAt
		out.ResetsAt = &r
	}
	if m.Derived != nil {
		out.Derived = append([]string(nil), m.Derived...)
	}
	return out
}

// Percent returns the integer percentage of Value against Limit.
//
// It reports ok=false when the upstream limit is unknown, so callers render
// "--" instead of inventing a plan-remaining percentage (MOD-001 7).
func (m ProviderMetric) Percent() (int, bool) {
	if m.Value == nil || m.Limit == nil {
		return 0, false
	}
	return decimal.PercentOf(*m.Value, *m.Limit)
}

// Duration is a time.Duration that marshals as a human-readable string such as
// "90s", keeping the JSON readable in fixtures and diagnostics.
type Duration time.Duration

// D returns the underlying time.Duration.
func (d Duration) D() time.Duration { return time.Duration(d) }

// MarshalJSON encodes the duration using time.Duration's string form.
func (d Duration) MarshalJSON() ([]byte, error) {
	return []byte(`"` + time.Duration(d).String() + `"`), nil
}

// UnmarshalJSON accepts a duration string ("90s") or an integer of nanoseconds.
func (d *Duration) UnmarshalJSON(b []byte) error {
	s := string(b)
	if s == "null" {
		*d = 0
		return nil
	}
	if len(s) >= 2 && s[0] == '"' {
		parsed, err := time.ParseDuration(s[1 : len(s)-1])
		if err != nil {
			return err
		}
		*d = Duration(parsed)
		return nil
	}
	var ns int64
	for _, c := range s {
		if c < '0' || c > '9' {
			return errInvalidDuration
		}
		ns = ns*10 + int64(c-'0')
	}
	*d = Duration(ns)
	return nil
}
