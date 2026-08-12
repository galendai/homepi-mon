package configtx

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/galendai/homepi-mon/internal/config"
	"github.com/galendai/homepi-mon/internal/connector"
	"github.com/galendai/homepi-mon/internal/protocol"
	"github.com/galendai/homepi-mon/internal/providermeta"
	"github.com/galendai/homepi-mon/internal/secretstore"
)

// Draft is the staged view of a Config that the operator (CLI or
// browser) edits. It is bound to the Service that created it; calling
// Apply on the Service commits the staged edits transactionally.
type Draft struct {
	svc *Service
	mu  sync.RWMutex

	// base is the snapshot loaded from disk. The Service uses base's
	// revision to detect stale drafts.
	base *config.Config
	// pending is the operator's working copy. Apply writes pending to
	// disk only after every step succeeds.
	pending *config.Config
	// overlay carries candidate secrets entered by the operator. The
	// overlay is the only path to test or apply a new key without
	// writing it to the keyring.
	overlay *overlayStore
	// revision is the content digest of the on-disk config this draft
	// was opened from. Apply compares it with a fresh disk digest.
	revision string
	// tested binds a successful read-only test to the exact Provider
	// config and candidate secret currently staged for that Provider.
	tested map[string]string
}

// newDraft returns a Draft whose pending state equals the on-disk
// config (or an empty Config when the file does not yet exist).
func newDraft(svc *Service, base *config.Config, revision string) *Draft {
	pending := cloneConfig(base)
	pending.ApplyDefaults()
	return &Draft{
		svc:      svc,
		base:     base,
		pending:  pending,
		overlay:  newOverlayStore(nil),
		revision: revision,
		tested:   make(map[string]string),
	}
}

// Revision exposes the draft's revision id.
func (d *Draft) Revision() string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.revision
}

// Base returns the on-disk snapshot the draft was derived from.
// Callers must treat it as read-only; editing it does not affect the
// draft.
func (d *Draft) Base() *config.Config {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return cloneConfig(d.base)
}

// Pending returns a deep copy of the operator's working copy. Like
// Base, the caller must not edit it directly; the result is intended
// for the Web Admin to render.
func (d *Draft) Pending() *config.Config {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return cloneConfig(d.pending)
}

// HasPending reports whether the draft differs from the on-disk base.
func (d *Draft) HasPending() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return !configEqual(d.base, d.pending)
}

// ProviderEdit is one staged change to a Provider entry. Empty fields
// mean "leave the existing value untouched"; setting Enabled/clearing
// fields uses a *bool/*string so the operator can force a value.
//
// The struct is the wire format for the Web Admin's draft API: the
// JSON tags are part of the contract and renames need to keep the
// browser compatible.
type ProviderEdit struct {
	// ID is the stable Provider identifier. When it does not yet
	// exist in the pending config, the edit creates a new entry; in
	// that case NewType must be set.
	ID string `json:"id"`
	// NewType is the registered Provider type, e.g. "mock" or
	// "deepseek_api". Required when the ID is new.
	NewType string `json:"new_type"`
	// AccountLabel is the operator-readable alias.
	AccountLabel *string `json:"account_label,omitempty"`
	// Region is "global", "cn" or "custom".
	Region *string `json:"region,omitempty"`
	// BaseURL is required when Region == "custom".
	BaseURL *string `json:"base_url,omitempty"`
	// Interval / StaleAfter are Go duration strings ("60s", "5m").
	Interval   *string `json:"interval,omitempty"`
	StaleAfter *string `json:"stale_after,omitempty"`
	// Enabled toggles the Provider; nil means "leave unchanged".
	Enabled *bool `json:"enabled,omitempty"`
	// SecretRef overrides the auto-generated keyring reference.
	SecretRef *string `json:"secret_ref,omitempty"`
	// AuthFile is the Codex-only local auth_file path.
	AuthFile *string `json:"auth_file,omitempty"`
	// MockFixture is the mock-only fixture path.
	MockFixture *string `json:"mock_fixture,omitempty"`
	// Options contains connector-specific, non-secret settings. A nil map leaves
	// the current value unchanged; an empty map clears it.
	Options map[string]string `json:"options,omitempty"`
	// CandidateSecret is a candidate value to attach to SecretRef
	// during this edit. The overlay only holds it until Apply
	// commits the secret to the keyring.
	CandidateSecret string `json:"candidate_secret,omitempty"`
	// RotateSecret, when true, requires CandidateSecret to be set
	// and signals that an existing secret_ref should be replaced
	// with a new versioned reference.
	RotateSecret bool `json:"rotate_secret,omitempty"`
	// DropSecret, when true, removes the entry from the pending
	// secret_ref list. DropSecret is exclusive with CandidateSecret
	// and RotateSecret.
	DropSecret bool `json:"drop_secret,omitempty"`
}

// EditProvider stages one Provider change. It returns a non-nil error
// when the staged edit would leave the pending config in a state that
// fails ValidateWithRegistry.
func (d *Draft) EditProvider(edit ProviderEdit) (err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	previous := cloneConfig(d.pending)
	previousOverlay := d.overlay.snapshotValues()
	previousTests := cloneTests(d.tested)
	defer func() {
		if err == nil {
			return
		}
		d.pending = previous
		d.overlay = overlayFromValues(previousOverlay)
		d.tested = previousTests
	}()
	meta, ok := providermeta.Lookup(edit.NewType)
	if !ok && edit.NewType != "" {
		return fmt.Errorf("%w: type %q is not registered", ErrInvalidDraft, edit.NewType)
	}
	idx := d.findIndex(edit.ID)
	isNew := idx < 0
	baseIdx := findProviderIndex(d.base, edit.ID)
	if isNew && edit.NewType == "" {
		return fmt.Errorf("%w: new provider requires type", ErrInvalidDraft)
	}
	if isNew {
		entry := config.ProviderConfig{ID: edit.ID, Type: edit.NewType}
		d.pending.Providers = append(d.pending.Providers, entry)
		idx = len(d.pending.Providers) - 1
	}
	if meta.TypeID == "" && idx >= 0 {
		meta, _ = providermeta.Lookup(d.pending.Providers[idx].Type)
	}
	if edit.NewType != "" && d.pending.Providers[idx].Type != "" &&
		d.pending.Providers[idx].Type != edit.NewType {
		return fmt.Errorf("%w: provider type cannot change from %q to %q",
			ErrInvalidDraft, d.pending.Providers[idx].Type, edit.NewType)
	}
	if edit.NewType != "" {
		d.pending.Providers[idx].Type = edit.NewType
		meta, _ = providermeta.Lookup(edit.NewType)
	}
	if edit.AccountLabel != nil {
		d.pending.Providers[idx].AccountLabel = *edit.AccountLabel
	}
	if edit.Region != nil {
		if meta.TypeID != "" {
			if err := meta.ValidateRegion(*edit.Region); err != nil {
				return fmt.Errorf("%w: %v", ErrInvalidDraft, err)
			}
		}
		d.pending.Providers[idx].Region = *edit.Region
	}
	if edit.BaseURL != nil {
		d.pending.Providers[idx].BaseURL = *edit.BaseURL
	}
	if edit.Interval != nil {
		d.pending.Providers[idx].Interval = *edit.Interval
	}
	if edit.StaleAfter != nil {
		d.pending.Providers[idx].StaleAfter = *edit.StaleAfter
	}
	if edit.Enabled != nil {
		d.pending.Providers[idx].Enabled = edit.Enabled
	}
	if edit.AuthFile != nil {
		d.pending.Providers[idx].AuthFile = *edit.AuthFile
	}
	if edit.MockFixture != nil {
		d.pending.Providers[idx].MockFixture = *edit.MockFixture
	}
	if edit.Options != nil {
		d.pending.Providers[idx].Options = cloneStringMap(edit.Options)
	}
	if edit.SecretRef != nil {
		d.pending.Providers[idx].SecretRef = *edit.SecretRef
	}
	if edit.DropSecret {
		if edit.RotateSecret || edit.CandidateSecret != "" {
			return fmt.Errorf("%w: drop_secret is exclusive with rotate and candidate",
				ErrInvalidDraft)
		}
		d.pending.Providers[idx].SecretRef = ""
		d.overlay.deleteFor(d.pending.Providers[idx].ID)
	}
	if edit.CandidateSecret != "" {
		ref := d.pending.Providers[idx].SecretRef
		if ref == "" {
			ref = "keyring:provider-key:" + d.pending.Providers[idx].ID
		}
		if meta.TypeID != "" && len(edit.CandidateSecret) > meta.SecretMax() {
			return fmt.Errorf("%w: %s", ErrSecretOversize, meta.SecretFieldLabel)
		}
		if baseIdx >= 0 {
			baseRef := d.base.Providers[baseIdx].SecretRef
			if ref == baseRef || !d.overlay.has(ref) {
				ref = nextVersionedRef(ref)
			}
		} else if isNew {
			ref = firstVersionedRef(ref)
		} else if edit.RotateSecret {
			ref = nextVersionedRef(ref)
		}
		d.pending.Providers[idx].SecretRef = ref
		if err := secretstore.ValidateRef(ref); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidDraft, err)
		}
		if err := d.overlay.Set(context.Background(), ref, edit.CandidateSecret); err != nil {
			return err
		}
	}
	d.pending.ApplyDefaults()
	if err := d.pending.ValidateWithRegistry(knownTypeSet()); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidDraft, err)
	}
	if err := d.checkShadows(); err != nil {
		return err
	}
	delete(d.tested, edit.ID)
	return nil
}

// DeleteProvider removes the entry whose ID matches id. The returned
// secret_ref is not deleted from the keyring here; Apply drops the
// secret after the new config is in place.
func (d *Draft) DeleteProvider(id string) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	idx := d.findIndex(id)
	if idx < 0 {
		return "", fmt.Errorf("%w: provider %q not found", ErrInvalidDraft, id)
	}
	ref := d.pending.Providers[idx].SecretRef
	d.pending.Providers = append(d.pending.Providers[:idx], d.pending.Providers[idx+1:]...)
	d.overlay.deleteFor(id)
	delete(d.tested, id)
	if err := d.pending.ValidateWithRegistry(knownTypeSet()); err != nil {
		return ref, fmt.Errorf("%w: %v", ErrInvalidDraft, err)
	}
	return ref, nil
}

// TestProvider runs a read-only Collect against the staged config,
// merging in any candidate secrets from the overlay. It is the same
// path the CLI's `provider test` uses, so the Web Admin never has a
// different answer.
func (d *Draft) TestProvider(ctx context.Context, id string) (TestResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	idx := d.findIndex(id)
	if idx < 0 {
		return TestResult{}, fmt.Errorf("%w: provider %q not found", ErrInvalidDraft, id)
	}
	spec := d.pending.Providers[idx]
	// Materialise a Store that reads the candidate overlay first and
	// falls back to the real keyring for any other ref. The fallback
	// is only used when the operator has not staged a candidate for
	// this Provider.
	real, err := d.svc.openStore(ctx)
	if err != nil {
		return TestResult{}, err
	}
	overlay := newOverlayStore(real)
	// Copy current overlay values (refs, not values) into the new
	// overlay so unrelated ref lookups still see the user's inputs.
	for k, v := range d.overlay.snapshotValues() {
		_ = overlay.Set(ctx, k, v)
	}
	conn, err := connector.Build(spec, overlay)
	if err != nil {
		return TestResult{}, err
	}
	if err := conn.ValidateConfig(); err != nil {
		return TestResult{}, fmt.Errorf("config invalid: %w", err)
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	started := d.svc.opts.Now()
	metrics, err := conn.Collect(cctx)
	elapsed := d.svc.opts.Now().Sub(started)
	res := TestResult{
		ProviderID:   spec.ID,
		ProviderType: spec.Type,
		MetricCount:  len(metrics),
		Elapsed:      elapsed,
		Class:        protocol.ErrNone,
	}
	if err != nil {
		delete(d.tested, id)
		res.Class = connector.Classify(err)
		res.Message = connector.Message(err)
		res.RetryAfter = connector.RetryAfter(err)
		return res, err
	}
	if reporter, ok := conn.(connector.HomeLabReporter); ok {
		report, reportErr := reporter.HomeLab()
		if reportErr != nil {
			delete(d.tested, id)
			res.Class = protocol.ErrSchemaChanged
			res.Message = "connector produced an invalid HomeLab summary"
			return res, reportErr
		}
		for i := range report.Nodes {
			if validateErr := report.Nodes[i].Validate(); validateErr != nil {
				delete(d.tested, id)
				res.Class = protocol.ErrSchemaChanged
				res.Message = "connector produced an invalid HomeLab summary"
				return res, validateErr
			}
		}
		for i := range report.Services {
			if validateErr := report.Services[i].Validate(); validateErr != nil {
				delete(d.tested, id)
				res.Class = protocol.ErrSchemaChanged
				res.Message = "connector produced an invalid HomeLab summary"
				return res, validateErr
			}
		}
		res.MetricCount += len(report.Nodes) + len(report.Services)
	}
	fingerprint, err := d.providerFingerprint(id)
	if err != nil {
		return TestResult{}, err
	}
	d.tested[id] = fingerprint
	return res, nil
}

// TestResult is the public summary of a single provider test. The
// Class is what the Web Admin colour-codes; the Message is safe to
// show to the user.
type TestResult struct {
	ProviderID   string
	ProviderType string
	MetricCount  int
	Elapsed      time.Duration
	Class        protocol.ErrorClass
	Message      string
	RetryAfter   int
}

// Diff returns the staged changes as a stable, sorted list. Each
// entry is one of: added, removed, modified.
func (d *Draft) Diff() []DiffEntry {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.diffLocked()
}

func (d *Draft) diffLocked() []DiffEntry {
	before := indexProviders(d.base.Providers)
	after := indexProviders(d.pending.Providers)
	seen := make(map[string]struct{}, len(after))
	out := make([]DiffEntry, 0, len(before)+len(after))
	for id, post := range after {
		seen[id] = struct{}{}
		pre, ok := before[id]
		if !ok {
			out = append(out, DiffEntry{Op: DiffAdded, ID: id, Type: post.Type})
			continue
		}
		if changes := diffProvider(pre, post); len(changes) > 0 {
			out = append(out, DiffEntry{Op: DiffModified, ID: id, Type: post.Type, Fields: changes})
		}
	}
	for id, pre := range before {
		if _, ok := seen[id]; ok {
			continue
		}
		out = append(out, DiffEntry{Op: DiffRemoved, ID: id, Type: pre.Type})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (d *Draft) validateRequiredTests() error {
	base := indexProviders(d.base.Providers)
	for _, p := range d.pending.Providers {
		if !p.IsEnabled() {
			continue
		}
		old, existed := base[p.ID]
		needsTest := !existed || old.SecretRef != p.SecretRef
		if !needsTest {
			continue
		}
		fingerprint, err := d.providerFingerprint(p.ID)
		if err != nil {
			return err
		}
		if d.tested[p.ID] != fingerprint {
			return fmt.Errorf("%w: provider %q must pass a read-only test before apply",
				ErrInvalidDraft, p.ID)
		}
	}
	return nil
}

func (d *Draft) providerFingerprint(id string) (string, error) {
	idx := d.findIndex(id)
	if idx < 0 {
		return "", fmt.Errorf("%w: provider %q not found", ErrInvalidDraft, id)
	}
	body, err := json.Marshal(d.pending.Providers[idx])
	if err != nil {
		return "", err
	}
	ref := d.pending.Providers[idx].SecretRef
	if value, ok := d.overlay.snapshotValues()[ref]; ok {
		body = append(body, 0)
		body = append(body, value...)
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

func cloneTests(src map[string]string) map[string]string {
	out := make(map[string]string, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

func overlayFromValues(values map[string]string) *overlayStore {
	o := newOverlayStore(nil)
	for ref, value := range values {
		_ = o.Set(context.Background(), ref, value)
	}
	return o
}

// DiffOp names a single change in a Draft diff.
type DiffOp string

const (
	DiffAdded    DiffOp = "added"
	DiffRemoved  DiffOp = "removed"
	DiffModified DiffOp = "modified"
)

// DiffEntry is one element of a Draft.Diff() result. Fields lists the
// provider field names that changed; values are never included.
type DiffEntry struct {
	Op     DiffOp
	ID     string
	Type   string
	Fields []string
}

func (d *Draft) findIndex(id string) int {
	for i := range d.pending.Providers {
		if d.pending.Providers[i].ID == id {
			return i
		}
	}
	return -1
}

func findProviderIndex(cfg *config.Config, id string) int {
	if cfg == nil {
		return -1
	}
	for i := range cfg.Providers {
		if cfg.Providers[i].ID == id {
			return i
		}
	}
	return -1
}

func (d *Draft) checkShadows() error {
	seen := make(map[string]string, len(d.pending.Providers))
	for _, p := range d.pending.Providers {
		if p.Enabled != nil && !*p.Enabled {
			continue
		}
		meta, ok := providermeta.Lookup(p.Type)
		if !ok {
			continue
		}
		ids, err := meta.StableMetricIDs(p)
		if err != nil {
			return fmt.Errorf("%w: provider %q metric ids: %v", ErrInvalidDraft, p.ID, err)
		}
		for _, stable := range ids {
			if other, dup := seen[stable]; dup {
				return fmt.Errorf("%w: provider %q and %q emit metric id %q",
					ErrShadowedProvider, other, p.ID, stable)
			}
			seen[stable] = p.ID
		}
	}
	return nil
}

func cloneConfig(c *config.Config) *config.Config {
	if c == nil {
		return &config.Config{SchemaVersion: config.SchemaVersion}
	}
	out := *c
	if c.Providers != nil {
		out.Providers = make([]config.ProviderConfig, len(c.Providers))
		for i, provider := range c.Providers {
			out.Providers[i] = provider
			out.Providers[i].Options = cloneStringMap(provider.Options)
		}
	}
	if c.Devices != nil {
		out.Devices = make([]config.DeviceConfig, len(c.Devices))
		copy(out.Devices, c.Devices)
	}
	return &out
}

func configEqual(a, b *config.Config) bool {
	if len(a.Providers) != len(b.Providers) {
		return false
	}
	if len(a.Devices) != len(b.Devices) {
		return false
	}
	for i := range a.Providers {
		if !providerEqual(a.Providers[i], b.Providers[i]) {
			return false
		}
	}
	for i := range a.Devices {
		if !deviceEqual(a.Devices[i], b.Devices[i]) {
			return false
		}
	}
	return true
}

func providerEqual(a, b config.ProviderConfig) bool {
	return a.ID == b.ID && a.Type == b.Type && a.AccountLabel == b.AccountLabel &&
		a.Region == b.Region && a.BaseURL == b.BaseURL && a.Interval == b.Interval &&
		a.StaleAfter == b.StaleAfter && sameBoolPtr(a.Enabled, b.Enabled) &&
		a.SecretRef == b.SecretRef && a.AuthFile == b.AuthFile &&
		a.MockFixture == b.MockFixture && stringMapEqual(a.Options, b.Options)
}

func cloneStringMap(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	out := make(map[string]string, len(source))
	for key, value := range source {
		out[key] = value
	}
	return out
}

func deviceEqual(a, b config.DeviceConfig) bool {
	if a.ID != b.ID || a.TokenRef != b.TokenRef {
		return false
	}
	if len(a.SubscribedMetrics) != len(b.SubscribedMetrics) {
		return false
	}
	for i := range a.SubscribedMetrics {
		if a.SubscribedMetrics[i] != b.SubscribedMetrics[i] {
			return false
		}
	}
	return true
}

func indexProviders(xs []config.ProviderConfig) map[string]config.ProviderConfig {
	out := make(map[string]config.ProviderConfig, len(xs))
	for _, p := range xs {
		out[p.ID] = p
	}
	return out
}

func diffProvider(a, b config.ProviderConfig) []string {
	var fields []string
	if a.Type != b.Type {
		fields = append(fields, "type")
	}
	if a.AccountLabel != b.AccountLabel {
		fields = append(fields, "account_label")
	}
	if a.Region != b.Region {
		fields = append(fields, "region")
	}
	if a.BaseURL != b.BaseURL {
		fields = append(fields, "base_url")
	}
	if a.Interval != b.Interval {
		fields = append(fields, "interval")
	}
	if a.StaleAfter != b.StaleAfter {
		fields = append(fields, "stale_after")
	}
	if a.SecretRef != b.SecretRef {
		fields = append(fields, "secret_ref")
	}
	if a.AuthFile != b.AuthFile {
		fields = append(fields, "auth_file")
	}
	if a.MockFixture != b.MockFixture {
		fields = append(fields, "mock_fixture")
	}
	if !stringMapEqual(a.Options, b.Options) {
		fields = append(fields, "options")
	}
	if !sameBoolPtr(a.Enabled, b.Enabled) {
		fields = append(fields, "enabled")
	}
	return fields
}

func stringMapEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if b[key] != value {
			return false
		}
	}
	return true
}

func sameBoolPtr(a, b *bool) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

// nextVersionedRef bumps a keyring:provider-key:<id> reference to
// keyring:provider-key:<id>@<n> so the old keyring entry is
// preserved until Apply proves the new one works.
func nextVersionedRef(ref string) string {
	if !strings.HasPrefix(ref, "keyring:") {
		return ref
	}
	if i := strings.LastIndex(ref, "@"); i > 0 {
		return ref[:i+1] + nextVersion(strings.TrimPrefix(ref, ref[:i+1]))
	}
	return ref + "@2"
}

func firstVersionedRef(ref string) string {
	if !strings.HasPrefix(ref, "keyring:") || strings.Contains(ref, "@") {
		return ref
	}
	return ref + "@1"
}

func nextVersion(s string) string {
	if s == "" {
		return "2"
	}
	// We accept a simple integer suffix; anything else is replaced
	// with "2" so the operator gets a sensible default.
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		return "2"
	}
	return fmt.Sprintf("%d", n+1)
}
