package state_test

import (
	"testing"
	"time"

	"github.com/galendai/homepi-mon/internal/decimal"
	"github.com/galendai/homepi-mon/internal/protocol"
	"github.com/galendai/homepi-mon/internal/state"
)

func newStore(t *testing.T, epoch string) *state.Current {
	t.Helper()
	fixed := time.Date(2026, 8, 10, 6, 32, 5, 0, time.UTC)
	c, err := state.New(state.Options{
		NodeID:    "dev-mac",
		NodeLabel: "DEV-MAC",
		Epoch:     epoch,
		Now:       func() time.Time { return fixed },
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	return c
}

func metric(id string, value string, order int) protocol.ProviderMetric {
	v := decimal.MustParse(value)
	l := decimal.MustParse("100")
	return protocol.ProviderMetric{
		ID:          id,
		Provider:    "mock",
		DisplayName: id,
		MetricKind:  protocol.KindQuota,
		Value:       &v,
		Limit:       &l,
		Unit:        "percent",
		Window:      protocol.WindowRolling5h,
		ObservedAt:  time.Date(2026, 8, 10, 6, 32, 0, 0, time.UTC),
		Precision:   protocol.PrecisionExact,
		SourceKind:  protocol.SourceMock,
		Status:      protocol.StatusOK,
		Order:       order,
	}
}

// U-C001: applying a batch bumps the version exactly once and the snapshot
// carries the daemon's epoch.
func TestApplyMetricsBumpsVersionOncePerBatch(t *testing.T) {
	c := newStore(t, "epoch-a")
	if c.Version() != 0 {
		t.Fatalf("initial version = %d, want 0", c.Version())
	}
	if _, err := c.ApplyMetrics(1, []protocol.ProviderMetric{metric("a", "10", 1), metric("b", "20", 2)}); err != nil {
		t.Fatal(err)
	}
	if c.Version() != 1 {
		t.Fatalf("version after batch = %d, want 1", c.Version())
	}
	snap := c.Snapshot()
	if snap.SourceEpoch != "epoch-a" || snap.SnapshotVersion != 1 {
		t.Fatalf("snapshot = epoch %s version %d", snap.SourceEpoch, snap.SnapshotVersion)
	}
	if err := snap.Validate(); err != nil {
		t.Fatalf("snapshot invalid: %v", err)
	}
}

// U-C002 (Test-Module-001 U010): a late result with a lower sequence must not
// overwrite a newer value for the same metric ID.
func TestLateResultDoesNotOverwriteNewer(t *testing.T) {
	c := newStore(t, "epoch-a")

	if _, err := c.ApplyMetrics(11, []protocol.ProviderMetric{metric("a", "11", 1)}); err != nil {
		t.Fatal(err)
	}
	applied, err := c.ApplyMetrics(10, []protocol.ProviderMetric{metric("a", "10", 1)})
	if err != nil {
		t.Fatal(err)
	}
	if applied != 0 {
		t.Fatalf("late seq applied %d metrics, want 0", applied)
	}
	if got := c.Snapshot().Metrics[0].Value.String(); got != "11" {
		t.Fatalf("value = %s, want 11 (newer result preserved)", got)
	}
	if c.Version() != 1 {
		t.Fatalf("version = %d, want 1 (dropped batch must not bump)", c.Version())
	}
}

// U-C003: snapshot ordering is deterministic so identical data serialises
// identically for ETag comparison.
func TestSnapshotOrderIsDeterministic(t *testing.T) {
	c := newStore(t, "epoch-a")
	_, err := c.ApplyMetrics(1, []protocol.ProviderMetric{
		metric("zeta", "1", 2),
		metric("alpha", "2", 2),
		metric("first", "3", 1),
	})
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, m := range c.Snapshot().Metrics {
		got = append(got, m.ID)
	}
	want := []string{"first", "alpha", "zeta"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

// U-C004: invalid readings are rejected before they can enter the store.
func TestApplyMetricsRejectsInvalid(t *testing.T) {
	c := newStore(t, "epoch-a")
	bad := metric("a", "10", 1)
	bad.MetricKind = "vibes"
	if _, err := c.ApplyMetrics(1, []protocol.ProviderMetric{bad}); err == nil {
		t.Fatal("expected validation error")
	}
	if c.Version() != 0 || c.HasData() {
		t.Fatal("invalid batch must not mutate state")
	}
}

// U-C005 (Test-Module-001 U018 / E007): a restarted daemon uses a new epoch and
// restarts its version counter, and the display accepts that as newer.
func TestRestartProducesNewEpochFromZero(t *testing.T) {
	first := newStore(t, "epoch-a")
	if _, err := first.ApplyMetrics(1, []protocol.ProviderMetric{metric("a", "10", 1)}); err != nil {
		t.Fatal(err)
	}
	before := first.Snapshot()

	restarted := newStore(t, "epoch-b")
	if _, err := restarted.ApplyMetrics(1, []protocol.ProviderMetric{metric("a", "42", 1)}); err != nil {
		t.Fatal(err)
	}
	after := restarted.Snapshot()

	if after.SnapshotVersion >= before.SnapshotVersion+1 && after.SourceEpoch == before.SourceEpoch {
		t.Fatal("restart must change the epoch")
	}
	ok, err := protocol.Supersedes(before, after)
	if !ok || err != nil {
		t.Fatalf("new epoch snapshot rejected: ok=%v err=%v", ok, err)
	}
}

// U-C006: connector health is stored and published without any raw error text.
func TestApplyHealth(t *testing.T) {
	c := newStore(t, "epoch-a")
	err := c.ApplyHealth(protocol.ConnectorHealth{
		ConnectorID: "mock-codex",
		Provider:    "codex",
		Enabled:     true,
		State:       protocol.ConnBlockedAuth,
		ErrorClass:  protocol.ErrAuth,
		Message:     "re-authenticate with the official CLI on dev-mac",
	})
	if err != nil {
		t.Fatal(err)
	}
	snap := c.Snapshot()
	if len(snap.ConnectorHealth) != 1 || snap.ConnectorHealth[0].State != protocol.ConnBlockedAuth {
		t.Fatalf("health not published: %+v", snap.ConnectorHealth)
	}
	if err := snap.Validate(); err != nil {
		t.Fatalf("snapshot invalid: %v", err)
	}
}

// U-C007 (Test-Module-001 U019/U021): a connector failure annotates only the
// metrics owned by that connector while preserving their last successful
// values. A later successful collection clears the failure state.
func TestConnectorFailurePropagatesToOwnedMetrics(t *testing.T) {
	c := newStore(t, "epoch-a")
	if _, err := c.ApplyConnectorMetrics("codex", 1, []protocol.ProviderMetric{
		metric("codex.5h", "42", 1),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ApplyConnectorMetrics("minimax", 2, []protocol.ProviderMetric{
		metric("minimax.5h", "68", 2),
	}); err != nil {
		t.Fatal(err)
	}

	changed, err := c.ApplyConnectorError("codex", protocol.ErrAuth,
		"re-authenticate with the official CLI on DEV-MAC")
	if err != nil {
		t.Fatal(err)
	}
	if changed != 1 {
		t.Fatalf("changed = %d, want 1", changed)
	}

	byID := map[string]protocol.ProviderMetric{}
	for _, m := range c.Snapshot().Metrics {
		byID[m.ID] = m
	}
	if got := byID["codex.5h"]; got.ErrorClass != protocol.ErrAuth ||
		got.Value == nil || got.Value.String() != "42" {
		t.Fatalf("codex metric = %+v, want preserved value with auth", got)
	}
	if got := byID["minimax.5h"].ErrorClass; got != protocol.ErrNone {
		t.Fatalf("unrelated metric error class = %q", got)
	}

	if _, err := c.ApplyConnectorMetrics("codex", 3, []protocol.ProviderMetric{
		metric("codex.5h", "55", 1),
	}); err != nil {
		t.Fatal(err)
	}
	got := c.Snapshot().Metrics[0]
	if got.ID != "codex.5h" || got.ErrorClass != protocol.ErrNone ||
		got.Value == nil || got.Value.String() != "55" {
		t.Fatalf("recovered metric = %+v", got)
	}
}

// U-C008 (Test-Module-001 U051): a successful connector batch is the complete
// current metric set for that connector. Optional windows that disappear must
// not remain behind and age into STALE, while unrelated and newer data stays.
func TestSuccessfulConnectorBatchReplacesOwnedMetricSet(t *testing.T) {
	c := newStore(t, "epoch-a")
	if _, err := c.ApplyConnectorMetrics("codex", 10, []protocol.ProviderMetric{
		metric("codex.5h", "42", 1),
		metric("codex.weekly", "15", 2),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ApplyConnectorMetrics("minimax", 11, []protocol.ProviderMetric{
		metric("minimax.5h", "68", 3),
	}); err != nil {
		t.Fatal(err)
	}

	// A late Codex batch cannot overwrite the 5h metric or delete weekly.
	if applied, err := c.ApplyConnectorMetrics("codex", 9, []protocol.ProviderMetric{
		metric("codex.5h", "1", 1),
	}); err != nil || applied != 0 {
		t.Fatalf("late batch: applied=%d err=%v", applied, err)
	}
	if got := len(c.Snapshot().Metrics); got != 3 {
		t.Fatalf("late batch left %d metrics, want 3", got)
	}

	// A newer successful batch that contains only 5h removes the old weekly
	// window and leaves another connector's metric untouched.
	if _, err := c.ApplyConnectorMetrics("codex", 12, []protocol.ProviderMetric{
		metric("codex.5h", "55", 1),
	}); err != nil {
		t.Fatal(err)
	}
	byID := make(map[string]protocol.ProviderMetric)
	for _, m := range c.Snapshot().Metrics {
		byID[m.ID] = m
	}
	if len(byID) != 2 {
		t.Fatalf("metrics = %+v, want codex.5h and minimax.5h", byID)
	}
	if _, ok := byID["codex.weekly"]; ok {
		t.Fatal("successful reduced batch retained stale codex.weekly")
	}
	if got := byID["codex.5h"].Value.String(); got != "55" {
		t.Fatalf("codex.5h = %s, want 55", got)
	}
	if got := byID["minimax.5h"].Value.String(); got != "68" {
		t.Fatalf("minimax.5h = %s, want 68", got)
	}

	// The deletion sequence is retained as a tombstone, so an older result
	// cannot resurrect the removed weekly metric after the newer batch won.
	if applied, err := c.ApplyConnectorMetrics("codex", 11, []protocol.ProviderMetric{
		metric("codex.weekly", "99", 2),
	}); err != nil || applied != 0 {
		t.Fatalf("resurrection batch: applied=%d err=%v", applied, err)
	}
	for _, m := range c.Snapshot().Metrics {
		if m.ID == "codex.weekly" {
			t.Fatal("older batch resurrected deleted codex.weekly")
		}
	}
}
