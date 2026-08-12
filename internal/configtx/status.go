package configtx

import (
	"context"
	"sort"
	"time"

	"github.com/galendai/homepi-mon/internal/config"
	"github.com/galendai/homepi-mon/internal/secretstore"
)

// ProviderStatus is one row in the Web Admin's Provider table. The
// fields are deliberately redacted: SecretRef is only returned when it
// is non-empty (and never includes the secret value); SecretPresent
// is the boolean the UI uses to render "configured" vs "not set".
type ProviderStatus struct {
	ID            string
	Type          string
	AccountLabel  string
	Region        string
	BaseURL       string
	Interval      string
	StaleAfter    string
	Enabled       bool
	SecretRef     string // masked
	SecretPresent bool
	AuthFile      string
	MockFixture   string
	// Pending indicates the draft differs from the on-disk config.
	Pending bool
	// Configured means the entry exists in the on-disk config.
	Configured bool
	// Loaded means the entry is part of the current config snapshot
	// (which is the same as Configured here; kept for clarity).
	Loaded bool
}

// NodeStatus is the overview shown on the Web Admin landing page. It
// never includes secret values.
type NodeStatus struct {
	NodeID         string
	NodeLabel      string
	ListenAddr     string
	SecretBackend  string
	ProviderCount  int
	DeviceCount    int
	Revision       string
	LastApplyAt    time.Time
	LastApplyState string
	LastApplyNote  string
	// HealthyProviderCount is the number of providers that passed a
	// read-only collect during the last Apply. 0 when no Apply has
	// happened yet.
	HealthyProviderCount int
}

// Status returns a redacted snapshot of the on-disk config plus the
// last-apply record. The Web Admin renders it on Overview.
func (s *Service) Status(ctx context.Context) (NodeStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg, err := loadOrEmpty(s.opts.ConfigPath)
	if err != nil {
		return NodeStatus{}, err
	}
	revision, err := revisionForDisk(s.opts.ConfigPath)
	if err != nil {
		return NodeStatus{}, err
	}
	s.baseRevision = revision
	return NodeStatus{
		NodeID:         cfg.SourceNode.ID,
		NodeLabel:      cfg.SourceNode.Label,
		ListenAddr:     cfg.Listen.Addr,
		SecretBackend:  s.SecretBackend(),
		ProviderCount:  len(cfg.Providers),
		DeviceCount:    len(cfg.Devices),
		Revision:       revision,
		LastApplyAt:    s.lastApply.at,
		LastApplyState: s.lastApply.status,
		LastApplyNote:  s.lastApply.summary,
	}, nil
}

// ProviderStatuses returns the redacted per-Provider view used by the
// Web Admin. It performs no network IO; "pending" is computed by
// diffing the on-disk config against the supplied draft (which may be
// nil when the Web Admin opens Overview before any draft edits).
func (s *Service) ProviderStatuses(ctx context.Context, draft *Draft) ([]ProviderStatus, error) {
	cfg, err := loadOrEmpty(s.opts.ConfigPath)
	if err != nil {
		return nil, err
	}
	pendingMap := make(map[string]config.ProviderConfig)
	diffMap := make(map[string]bool)
	if draft != nil && draft.svc == s {
		draft.mu.RLock()
		defer draft.mu.RUnlock()
		for _, p := range draft.pending.Providers {
			pendingMap[p.ID] = p
		}
		for _, change := range draft.diffLocked() {
			diffMap[change.ID] = true
		}
	}
	store, err := s.openStore(ctx)
	if err != nil {
		// We tolerate a missing backend so the Overview still
		// renders. The presence check is best-effort.
		store = nil
	}
	diskMap := indexProviders(cfg.Providers)
	ids := make([]string, 0, len(diskMap)+len(pendingMap))
	seen := make(map[string]bool)
	for id := range diskMap {
		ids = append(ids, id)
		seen[id] = true
	}
	for id := range pendingMap {
		if !seen[id] {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	out := make([]ProviderStatus, 0, len(ids))
	for _, id := range ids {
		p, inPending := pendingMap[id]
		diskProvider, configured := diskMap[id]
		if !inPending {
			p = diskProvider
		}
		st := statusFor(p, diffMap[id], store)
		st.Configured = configured
		st.Loaded = configured
		out = append(out, st)
	}
	return out, nil
}

func statusFor(p config.ProviderConfig, pending bool,
	store secretstore.Store) ProviderStatus {

	st := ProviderStatus{
		ID:           p.ID,
		Type:         p.Type,
		AccountLabel: p.AccountLabel,
		Region:       p.Region,
		BaseURL:      p.BaseURL,
		Interval:     p.Interval,
		StaleAfter:   p.StaleAfter,
		Enabled:      p.IsEnabled(),
		SecretRef:    maskRef(p.SecretRef),
		AuthFile:     p.AuthFile,
		MockFixture:  p.MockFixture,
		Pending:      pending,
		Configured:   true,
		Loaded:       true,
	}
	if p.SecretRef != "" {
		if store == nil {
			st.SecretPresent = true // optimistic when backend is unreachable
		} else if _, err := store.Get(context.Background(), p.SecretRef); err == nil {
			st.SecretPresent = true
		}
	}
	return st
}

// maskRef is the public-facing reference form. It mirrors
// cmd/homepi-node/provider.go:maskRef so the operator sees the same
// shape across CLI and Web Admin.
func maskRef(ref string) string {
	if ref == "" {
		return ""
	}
	const tail = 6
	if len(ref) <= tail+len("keyring:") {
		return ref
	}
	return ref[:len("keyring:")] + "..." + ref[len(ref)-tail:]
}

// DisplayStatus is the redacted state of the connected Pi shown in
// the Web Admin. It is sourced from the current daemon runtime; the
// Phase 2 implementation only populates the offline fields until P2-03
// introduces the SSH deployer.
type DisplayStatus struct {
	Connected       bool
	Theme           string
	LatestSnapshot  time.Time
	SourceEpoch     string
	SnapshotVersion uint64
	Note            string
}

// DisplayStatus returns the current Display state. In Phase 2 P2-01
// the Service has no direct view into the Pi; the values are filled
// from a callback the caller injects. When the callback is nil, the
// returned DisplayStatus is marked as not connected.
func (s *Service) DisplayStatus(_ context.Context, fetch func() (DisplayStatus, error)) (DisplayStatus, error) {
	if fetch == nil {
		return DisplayStatus{Connected: false, Note: "Display status source not registered"}, nil
	}
	st, err := fetch()
	if err != nil {
		return DisplayStatus{Connected: false, Note: err.Error()}, nil
	}
	if st.Note == "" {
		st.Note = "Display status source not registered"
	}
	return st, nil
}
