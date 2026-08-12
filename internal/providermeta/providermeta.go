// Package providermeta centralises the typed metadata for every Provider
// the daemon supports. It exists so the Config Transaction Service, the
// Web Admin and the CLI all validate Provider fields against a single
// source of truth instead of repeating switch-statements scattered across
// the codebase.
//
// Each connector package registers its TypeMeta in init(); the package
// never imports a concrete connector, which keeps the dependency graph
// one-way (config -> providermeta -> never imports connectors).
package providermeta

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/galendai/homepi-mon/internal/config"
)

// TypeMeta is the typed contract for one Provider type. The Config
// Transaction Service uses it to drive validation, draft field
// generation, key-overlay handling and the Web Admin form schema.
type TypeMeta struct {
	// TypeID is the stable identifier written to ProviderConfig.Type.
	TypeID string
	// Label is the human-readable name shown in the Web Admin.
	Label string
	// Provider is the vendor key, e.g. "openai" or "deepseek".
	Provider string
	// RequiresSecret reports whether the connector needs a secret_ref
	// pointing at a stored Provider API key. When false, the secret
	// field is hidden in the Web Admin and CLI.
	RequiresSecret bool
	// SecretFieldLabel is the input label shown next to the password
	// field, e.g. "DeepSeek API Key". Empty when RequiresSecret is false.
	SecretFieldLabel string
	// SecretMaxBytes bounds a single secret value to keep
	// memory/overlay usage predictable. 0 falls back to defaultSecretMax.
	SecretMaxBytes int
	// RequiresAuthFile reports whether the connector reads a local
	// login state file (currently only codex_usage).
	RequiresAuthFile bool
	// AuthFileFieldLabel is the label shown next to the auth_file
	// input. Empty when RequiresAuthFile is false.
	AuthFileFieldLabel string
	// MinInterval is the lower bound for ProviderConfig.Interval.
	MinInterval time.Duration
	// MinStaleAfter is the lower bound for ProviderConfig.StaleAfter.
	MinStaleAfter time.Duration
	// SupportedRegions is the set of region values the operator can pick.
	// When empty, the connector only accepts the default "global".
	SupportedRegions []string
	// DefaultRegion is the value used when the draft is empty.
	DefaultRegion string
	// DefaultBaseURL is used when region=custom; the Web Admin uses it
	// as a placeholder. Empty means custom is not supported.
	DefaultBaseURL string
	// MockFixtureOnly marks types that use a mock fixture path instead
	// of a remote endpoint (only the "mock" type).
	MockFixtureOnly bool
	// MockFixtureLabel is the label for the mock_fixture field.
	MockFixtureLabel string
	// Description is shown under the type label in the Web Admin.
	Description string
	// MetricIDSuffixes lists every stable metric suffix the connector
	// may emit. The transaction service prefixes each with Provider ID.
	MetricIDSuffixes []string
	// MetricIDResolver handles data-driven IDs such as mock fixtures.
	// It takes precedence over MetricIDSuffixes when non-nil.
	MetricIDResolver func(config.ProviderConfig) ([]string, error)
}

// DefaultSecretMax is the fallback cap on a single secret value when
// TypeMeta.SecretMaxBytes is 0.
const DefaultSecretMax = 4096

// SecretMax returns the effective per-secret byte cap.
func (m TypeMeta) SecretMax() int {
	if m.SecretMaxBytes > 0 {
		return m.SecretMaxBytes
	}
	return DefaultSecretMax
}

// HasRegion reports whether region is one of the connector's supported
// values. Unknown region always returns false.
func (m TypeMeta) HasRegion(region string) bool {
	for _, r := range m.SupportedRegions {
		if r == region {
			return true
		}
	}
	return false
}

// StableMetricIDs returns the complete IDs this provider can emit without
// performing network I/O. Data-driven connectors may return an empty list
// when their local source is not available; their required provider test
// will still prevent an unverified Apply.
func (m TypeMeta) StableMetricIDs(spec config.ProviderConfig) ([]string, error) {
	if m.MetricIDResolver != nil {
		return m.MetricIDResolver(spec)
	}
	out := make([]string, 0, len(m.MetricIDSuffixes))
	for _, suffix := range m.MetricIDSuffixes {
		out = append(out, spec.ID+"."+suffix)
	}
	return out, nil
}

// registry is the process-wide list of registered Provider types. The
// Config Transaction Service, Web Admin and CLI all read from it. The
// underlying map is never mutated after init, so lookups only take a
// read lock.
var (
	registryMu sync.RWMutex
	registry   = map[string]TypeMeta{}
)

// Register installs metadata for typeID. It panics on duplicate or empty
// registrations because they indicate a programming error caught at
// startup, not a runtime condition.
func Register(m TypeMeta) {
	if m.TypeID == "" {
		panic("providermeta: Register called with empty TypeID")
	}
	if m.Label == "" {
		panic("providermeta: Register called with empty Label for " + m.TypeID)
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := registry[m.TypeID]; exists {
		panic("providermeta: Register called twice for " + m.TypeID)
	}
	registry[m.TypeID] = m
}

// Lookup returns the TypeMeta registered for typeID. The bool mirrors
// map lookups so callers can tell "unknown" from "registered with
// zero value".
func Lookup(typeID string) (TypeMeta, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	m, ok := registry[typeID]
	return m, ok
}

// Known returns every registered TypeMeta sorted by TypeID. The Web
// Admin and `provider list` both rely on a stable order.
func Known() []TypeMeta {
	registryMu.RLock()
	out := make([]TypeMeta, 0, len(registry))
	for _, m := range registry {
		out = append(out, m)
	}
	registryMu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].TypeID < out[j].TypeID })
	return out
}

// KnownIDs returns the sorted set of registered type IDs.
func KnownIDs() []string {
	registryMu.RLock()
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	registryMu.RUnlock()
	sort.Strings(out)
	return out
}

// ValidateRegion returns nil when region is empty and "global" is the
// only supported entry, when region matches the default, or when region
// is in SupportedRegions. The error message names the type so the
// Web Admin can surface a useful field-level message.
func (m TypeMeta) ValidateRegion(region string) error {
	if region == "" {
		return nil
	}
	if m.HasRegion(region) {
		return nil
	}
	if len(m.SupportedRegions) == 0 && region == m.DefaultRegion {
		return nil
	}
	return fmt.Errorf("type=%s region=%q is not supported", m.TypeID, region)
}

// EffectiveMinInterval / EffectiveMinStaleAfter default to 5s/interval
// when TypeMeta leaves them at zero, matching the daemon-wide rule.
func (m TypeMeta) EffectiveMinInterval() time.Duration {
	if m.MinInterval > 0 {
		return m.MinInterval
	}
	return 5 * time.Second
}

func (m TypeMeta) EffectiveMinStaleAfter() time.Duration {
	if m.MinStaleAfter > 0 {
		return m.MinStaleAfter
	}
	return 5 * time.Second
}
