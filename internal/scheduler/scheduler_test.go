package scheduler_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/galendai/homepi-mon/internal/connector"
	"github.com/galendai/homepi-mon/internal/decimal"
	"github.com/galendai/homepi-mon/internal/protocol"
	"github.com/galendai/homepi-mon/internal/scheduler"
	"github.com/galendai/homepi-mon/internal/state"
	"github.com/galendai/homepi-mon/internal/ui"
)

// U-K001 (Test-Module-001 U008): a 429 with Retry-After schedules no earlier
// than the header asks.
func TestRetryAfterWins(t *testing.T) {
	p := scheduler.DefaultPolicy(time.Minute)
	for _, random := range []float64{0, 0.25, 0.5, 0.75, 0.999} {
		got := p.NextDelay(protocol.ErrRateLimited, 1, 120, random)
		if got < 120*time.Second {
			t.Fatalf("NextDelay(random=%v) = %v, want >= 120s", random, got)
		}
	}
}

// U-K002 (Test-Module-001 U019): an auth failure backs off at least 15 minutes
// so an expired login never becomes a retry storm.
func TestAuthBackoffIsSlow(t *testing.T) {
	p := scheduler.DefaultPolicy(time.Minute)
	if got := p.Delay(protocol.ErrAuth, 1, 0); got < 15*time.Minute {
		t.Fatalf("auth backoff = %v, want >= 15m", got)
	}
	if !scheduler.Blocked(protocol.ErrAuth) {
		t.Error("auth must be treated as blocked-on-user")
	}
	if got := scheduler.StateFor(protocol.ErrAuth, true); got != protocol.ConnBlockedAuth {
		t.Errorf("state = %v, want blocked_auth", got)
	}
}

// U-K003: transient failures get one fast retry, then exponential backoff
// capped at MaxBackoff.
func TestTransientBackoffGrowsAndCaps(t *testing.T) {
	p := scheduler.DefaultPolicy(time.Minute)

	if got := p.Delay(protocol.ErrTimeout, 1, 0); got != p.MinBackoff {
		t.Fatalf("first failure = %v, want fast retry %v", got, p.MinBackoff)
	}
	prev := time.Duration(0)
	for failures := 2; failures <= 12; failures++ {
		got := p.Delay(protocol.ErrNetwork, failures, 0)
		if got < prev {
			t.Fatalf("backoff shrank at %d failures: %v < %v", failures, got, prev)
		}
		if got > p.MaxBackoff {
			t.Fatalf("backoff %v exceeded cap %v", got, p.MaxBackoff)
		}
		prev = got
	}
	if prev != p.MaxBackoff {
		t.Fatalf("backoff never reached cap: %v", prev)
	}
}

// U-K004: success returns to the steady interval and jitter stays inside the
// configured fraction.
func TestSuccessUsesIntervalWithBoundedJitter(t *testing.T) {
	p := scheduler.DefaultPolicy(90 * time.Second)
	if got := p.Delay(protocol.ErrNone, 0, 0); got != 90*time.Second {
		t.Fatalf("success delay = %v, want 90s", got)
	}
	for _, r := range []float64{0, 0.25, 0.5, 0.75, 0.999} {
		got := p.Jitter(90*time.Second, r)
		if got < 81*time.Second || got > 99*time.Second {
			t.Errorf("jitter(%v) = %v, outside +/-10%%", r, got)
		}
	}
	// Startup jitter only ever delays, never advances.
	for _, r := range []float64{0, 0.5, 0.999} {
		got := p.StartupJitter(r)
		if got < 0 || got > 9*time.Second {
			t.Errorf("startup jitter(%v) = %v, want 0..9s", r, got)
		}
	}
}

// --- integration-style tests over the running scheduler ---

type stubConnector struct {
	id      string
	calls   atomic.Int64
	mu      sync.Mutex
	metrics []protocol.ProviderMetric
	err     error
	// onCall is invoked while the connector holds a concurrency slot.
	onCall func()
}

func (s *stubConnector) ID() string            { return s.id }
func (s *stubConnector) Provider() string      { return "stub" }
func (s *stubConnector) ValidateConfig() error { return nil }
func (s *stubConnector) Collect(ctx context.Context) ([]protocol.ProviderMetric, error) {
	s.calls.Add(1)
	if s.onCall != nil {
		s.onCall()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return nil, s.err
	}
	return s.metrics, nil
}

func (s *stubConnector) set(metrics []protocol.ProviderMetric, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.metrics, s.err = metrics, err
}

func stubMetric(id, value string) protocol.ProviderMetric {
	v := decimal.MustParse(value)
	l := decimal.MustParse("100")
	return protocol.ProviderMetric{
		ID: id, Provider: "stub", DisplayName: id,
		MetricKind: protocol.KindQuota, Value: &v, Limit: &l,
		Unit: "percent", Window: protocol.WindowRolling5h,
		ObservedAt: time.Now().UTC(), Precision: protocol.PrecisionExact,
		SourceKind: protocol.SourceMock, Status: protocol.StatusOK,
	}
}

func newStore(t *testing.T) *state.Current {
	t.Helper()
	c, err := state.New(state.Options{NodeID: "dev-mac", NodeLabel: "DEV-MAC", Epoch: "epoch-test"})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// E-K001 (Test-Module-001 E003): one failing connector does not stop the
// others from publishing, and the process stays alive.
func TestOneFailingConnectorDoesNotBlockOthers(t *testing.T) {
	store := newStore(t)
	good := &stubConnector{id: "good"}
	good.set([]protocol.ProviderMetric{stubMetric("good.metric", "68")}, nil)
	bad := &stubConnector{id: "bad"}
	bad.set(nil, connector.Errorf(protocol.ErrTimeout, "upstream timed out"))

	fast := scheduler.DefaultPolicy(20 * time.Millisecond)
	fast.MinBackoff = 10 * time.Millisecond
	fast.MaxBackoff = 20 * time.Millisecond
	fast.JitterFraction = 0

	s := scheduler.New([]scheduler.Task{
		{Connector: good, Policy: fast, Timeout: time.Second, Enabled: true},
		{Connector: bad, Policy: fast, Timeout: time.Second, Enabled: true},
	}, scheduler.Options{Store: store, NodeLabel: "DEV-MAC", Logger: quietLogger()})

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	s.Run(ctx)

	snap := store.Snapshot()
	if len(snap.Metrics) != 1 || snap.Metrics[0].ID != "good.metric" {
		t.Fatalf("healthy connector output missing: %+v", snap.Metrics)
	}
	states := map[string]protocol.ConnectorState{}
	for _, h := range snap.ConnectorHealth {
		states[h.ConnectorID] = h.State
	}
	if states["good"] != protocol.ConnOK {
		t.Errorf("good connector state = %v", states["good"])
	}
	if states["bad"] != protocol.ConnDegraded {
		t.Errorf("bad connector state = %v, want degraded", states["bad"])
	}
	if bad.calls.Load() < 2 {
		t.Errorf("failing connector retried %d times, expected repeated attempts", bad.calls.Load())
	}
}

// E-K002 (Test-Module-001 U019 / S004): an auth failure publishes blocked_auth
// with the official-CLI hint and nothing else.
func TestAuthFailurePublishesBlockedAuthOnly(t *testing.T) {
	store := newStore(t)
	c := &stubConnector{id: "codex"}
	c.set(nil, connector.Errorf(protocol.ErrAuth, "401 from upstream"))

	fast := scheduler.DefaultPolicy(20 * time.Millisecond)
	fast.AuthBackoff = 30 * time.Millisecond
	fast.JitterFraction = 0

	s := scheduler.New([]scheduler.Task{
		{Connector: c, Policy: fast, Timeout: time.Second, Enabled: true},
	}, scheduler.Options{Store: store, NodeLabel: "DEV-MAC", Logger: quietLogger()})

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()
	s.Run(ctx)

	snap := store.Snapshot()
	if len(snap.ConnectorHealth) != 1 {
		t.Fatalf("want 1 health record, got %d", len(snap.ConnectorHealth))
	}
	h := snap.ConnectorHealth[0]
	if h.State != protocol.ConnBlockedAuth || h.ErrorClass != protocol.ErrAuth {
		t.Fatalf("health = %+v", h)
	}
	if h.Message != "re-authenticate with the official CLI on DEV-MAC" {
		t.Errorf("message = %q, want the official-CLI hint", h.Message)
	}
	// The raw upstream text must not survive into the published record.
	if h.Message == "401 from upstream" {
		t.Error("raw upstream error leaked into published health")
	}
}

// E-K003 (Test-Module-001 U019/U021): a real Collect error after a successful
// reading propagates to the owned metric, so the TUI can render AUTH instead
// of leaving the old value marked healthy.
func TestCollectErrorAnnotatesLastSuccessfulMetric(t *testing.T) {
	store := newStore(t)
	c := &stubConnector{id: "codex"}
	m := stubMetric("codex.5h", "42")
	m.Group = "coding"
	c.set([]protocol.ProviderMetric{m}, nil)

	fast := scheduler.DefaultPolicy(10 * time.Millisecond)
	fast.AuthBackoff = 20 * time.Millisecond
	fast.JitterFraction = 0
	s := scheduler.New([]scheduler.Task{
		{Connector: c, Policy: fast, Timeout: time.Second, Enabled: true},
	}, scheduler.Options{Store: store, NodeLabel: "DEV-MAC", Logger: quietLogger()})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	waitForScheduler(t, time.Second, func() bool { return store.HasData() })
	c.set(nil, connector.Errorf(protocol.ErrAuth, "401 from upstream"))
	waitForScheduler(t, time.Second, func() bool {
		snap := store.Snapshot()
		return len(snap.Metrics) == 1 && snap.Metrics[0].ErrorClass == protocol.ErrAuth
	})

	snap := store.Snapshot()
	if snap.Metrics[0].Value == nil || snap.Metrics[0].Value.String() != "42" {
		t.Fatalf("last successful value was not preserved: %+v", snap.Metrics[0])
	}
	if snap.Metrics[0].Message != "re-authenticate with the official CLI on DEV-MAC" {
		t.Fatalf("metric message = %q", snap.Metrics[0].Message)
	}
	vm := ui.Build(snap, ui.BuildOptions{
		Page: "CODING", Now: time.Now(), Connected: true,
		Thresholds: protocol.DefaultThresholds(), FallbackNodeLabel: "DEV-MAC",
	})
	if len(vm.Coding) != 1 || vm.Coding[0].Status != protocol.DisplayAuth {
		t.Fatalf("TUI card did not receive auth state: %+v", vm.Coding)
	}

	recovered := stubMetric("codex.5h", "55")
	recovered.Group = "coding"
	c.set([]protocol.ProviderMetric{recovered}, nil)
	waitForScheduler(t, time.Second, func() bool {
		snap := store.Snapshot()
		return len(snap.Metrics) == 1 && snap.Metrics[0].ErrorClass == protocol.ErrNone &&
			snap.Metrics[0].Value != nil && snap.Metrics[0].Value.String() == "55"
	})
	c.set(nil, connector.Errorf(protocol.ErrTimeout, "upstream timed out"))
	waitForScheduler(t, time.Second, func() bool {
		snap := store.Snapshot()
		return len(snap.Metrics) == 1 && snap.Metrics[0].ErrorClass == protocol.ErrTimeout
	})
	vm = ui.Build(store.Snapshot(), ui.BuildOptions{
		Page: "CODING", Now: time.Now(), Connected: true,
		Thresholds: protocol.DefaultThresholds(), FallbackNodeLabel: "DEV-MAC",
	})
	if len(vm.Coding) != 1 || vm.Coding[0].Status != protocol.DisplayStale {
		t.Fatalf("TUI card did not receive stale state: %+v", vm.Coding)
	}
}

// U-K006 (Test-Module-001 U022): scheduler health retains the last success,
// counts consecutive failures and publishes the actual next-attempt time.
func TestHealthTracksConsecutiveFailuresAndNextAttempt(t *testing.T) {
	store := newStore(t)
	c := &stubConnector{id: "codex"}
	c.set([]protocol.ProviderMetric{stubMetric("codex.5h", "42")}, nil)

	fast := scheduler.DefaultPolicy(10 * time.Millisecond)
	fast.MinBackoff = 40 * time.Millisecond
	fast.MaxBackoff = 40 * time.Millisecond
	fast.JitterFraction = 0
	s := scheduler.New([]scheduler.Task{
		{Connector: c, Policy: fast, Timeout: time.Second, Enabled: true},
	}, scheduler.Options{Store: store, NodeLabel: "DEV-MAC", Logger: quietLogger()})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(done)
	}()

	waitForScheduler(t, time.Second, func() bool {
		h, ok := healthFor(store.Snapshot(), "codex")
		return ok && h.State == protocol.ConnOK && h.LastSuccessAt != nil
	})
	c.set(nil, connector.Errorf(protocol.ErrTimeout, "upstream timed out"))
	waitForScheduler(t, 2*time.Second, func() bool {
		h, ok := healthFor(store.Snapshot(), "codex")
		return ok && h.ConsecutiveFailures == 3
	})

	h, ok := healthFor(store.Snapshot(), "codex")
	if !ok {
		t.Fatal("missing connector health")
	}
	if h.LastSuccessAt == nil {
		t.Fatal("last_success_at was lost after failures")
	}
	if h.NextAttemptAt == nil || !h.NextAttemptAt.After(*h.LastAttemptAt) {
		t.Fatalf("next attempt not scheduled after attempt: %+v", h)
	}
	if h.ConsecutiveFailures != 3 {
		t.Fatalf("consecutive failures = %d, want 3", h.ConsecutiveFailures)
	}
	lastSuccess := *h.LastSuccessAt

	c.set([]protocol.ProviderMetric{stubMetric("codex.5h", "55")}, nil)
	waitForScheduler(t, time.Second, func() bool {
		recovered, ok := healthFor(store.Snapshot(), "codex")
		return ok && recovered.State == protocol.ConnOK && recovered.ConsecutiveFailures == 0 &&
			recovered.LastSuccessAt != nil && !recovered.LastSuccessAt.Before(lastSuccess)
	})
	cancel()
	<-done
}

func waitForScheduler(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for scheduler state")
}

func healthFor(snap *protocol.MetricSnapshot, id string) (protocol.ConnectorHealth, bool) {
	for _, h := range snap.ConnectorHealth {
		if h.ConnectorID == id {
			return h, true
		}
	}
	return protocol.ConnectorHealth{}, false
}

// P-K001 (Test-Module-001 P001): global concurrency never exceeds the limit.
func TestConcurrencyIsBounded(t *testing.T) {
	const limit = 4
	store := newStore(t)

	var live atomic.Int64
	var peak atomic.Int64
	hold := func() {
		n := live.Add(1)
		for {
			old := peak.Load()
			if n <= old || peak.CompareAndSwap(old, n) {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
		live.Add(-1)
	}

	fast := scheduler.DefaultPolicy(5 * time.Millisecond)
	fast.JitterFraction = 0

	tasks := make([]scheduler.Task, 0, 20)
	conns := make([]*stubConnector, 0, 20)
	for i := 0; i < 20; i++ {
		c := &stubConnector{id: "c" + string(rune('a'+i)), onCall: hold}
		c.set([]protocol.ProviderMetric{stubMetric("m"+string(rune('a'+i)), "50")}, nil)
		conns = append(conns, c)
		tasks = append(tasks, scheduler.Task{Connector: c, Policy: fast, Timeout: time.Second, Enabled: true})
	}

	s := scheduler.New(tasks, scheduler.Options{
		Store: store, MaxConcurrency: limit, Logger: quietLogger(),
	})
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	s.Run(ctx)

	if got := peak.Load(); got > limit {
		t.Fatalf("peak concurrency = %d, want <= %d", got, limit)
	}
	total := int64(0)
	for _, c := range conns {
		total += c.calls.Load()
	}
	if total == 0 {
		t.Fatal("no collections ran")
	}
}

// U-K005: a disabled task publishes a disabled connector and never collects.
func TestDisabledTaskNeverCollects(t *testing.T) {
	store := newStore(t)
	c := &stubConnector{id: "off"}
	c.set([]protocol.ProviderMetric{stubMetric("off.metric", "50")}, nil)

	s := scheduler.New([]scheduler.Task{
		{Connector: c, Policy: scheduler.DefaultPolicy(time.Millisecond), Enabled: false},
	}, scheduler.Options{Store: store, Logger: quietLogger()})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	s.Run(ctx)

	if c.calls.Load() != 0 {
		t.Fatalf("disabled connector collected %d times", c.calls.Load())
	}
	snap := store.Snapshot()
	if len(snap.Metrics) != 0 {
		t.Fatal("disabled connector produced metrics")
	}
	if snap.ConnectorHealth[0].State != protocol.ConnDisabled {
		t.Fatalf("state = %v, want disabled", snap.ConnectorHealth[0].State)
	}
}
