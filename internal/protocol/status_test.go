package protocol_test

import (
	"testing"
	"time"

	"github.com/galendai/homepi-mon/internal/decimal"
	"github.com/galendai/homepi-mon/internal/protocol"
)

func quota(percentLeft string, precision protocol.Precision, observed time.Time) protocol.ProviderMetric {
	return protocol.ProviderMetric{
		ID:         "p.quota",
		Provider:   "p",
		MetricKind: protocol.KindQuota,
		Value:      dec(percentLeft),
		Limit:      dec("100"),
		Unit:       "percent",
		Window:     protocol.WindowRolling5h,
		ObservedAt: observed,
		Precision:  precision,
		SourceKind: protocol.SourceOfficialAPI,
		Status:     protocol.StatusOK,
	}
}

// U-S001: the six availability semantics required by P1-02 each map to their
// own badge, and error classes are evaluated before thresholds.
func TestDeriveDisplayStatusSemantics(t *testing.T) {
	now := ts("2026-08-10T06:32:00Z")
	fresh := now.Add(-10 * time.Second)
	th := protocol.DefaultThresholds()

	cases := []struct {
		name  string
		build func() protocol.ProviderMetric
		want  protocol.DisplayStatus
	}{
		{"live ok", func() protocol.ProviderMetric {
			return quota("68", protocol.PrecisionExact, fresh)
		}, protocol.DisplayOK},

		{"warning threshold", func() protocol.ProviderMetric {
			return quota("18", protocol.PrecisionExact, fresh)
		}, protocol.DisplayWarn},

		{"critical threshold", func() protocol.ProviderMetric {
			return quota("7", protocol.PrecisionExact, fresh)
		}, protocol.DisplayCrit},

		{"auth beats value", func() protocol.ProviderMetric {
			m := quota("7", protocol.PrecisionExact, fresh)
			m.ErrorClass = protocol.ErrAuth
			return m
		}, protocol.DisplayAuth},

		{"rate limited is delayed", func() protocol.ProviderMetric {
			m := quota("68", protocol.PrecisionExact, fresh)
			m.ErrorClass = protocol.ErrRateLimited
			return m
		}, protocol.DisplayDelayed},

		{"unsupported is n/a", func() protocol.ProviderMetric {
			m := quota("68", protocol.PrecisionExact, fresh)
			m.ErrorClass = protocol.ErrUnsupported
			return m
		}, protocol.DisplayNA},

		{"unavailable precision is n/a", func() protocol.ProviderMetric {
			m := quota("68", protocol.PrecisionUnavailable, fresh)
			return m
		}, protocol.DisplayNA},

		{"schema change is error", func() protocol.ProviderMetric {
			m := quota("68", protocol.PrecisionExact, fresh)
			m.ErrorClass = protocol.ErrSchemaChanged
			return m
		}, protocol.DisplayError},

		{"timeout keeps old value as stale", func() protocol.ProviderMetric {
			m := quota("68", protocol.PrecisionExact, fresh)
			m.ErrorClass = protocol.ErrTimeout
			return m
		}, protocol.DisplayStale},

		{"expired freshness budget is stale", func() protocol.ProviderMetric {
			m := quota("68", protocol.PrecisionExact, now.Add(-10*time.Minute))
			return m
		}, protocol.DisplayStale},

		{"per-metric stale_after honoured", func() protocol.ProviderMetric {
			m := quota("68", protocol.PrecisionExact, now.Add(-30*time.Second))
			m.StaleAfter = protocol.Duration(15 * time.Second)
			return m
		}, protocol.DisplayStale},
	}

	for _, c := range cases {
		if got := protocol.DeriveDisplayStatus(c.build(), th, now); got != c.want {
			t.Errorf("%s: got %s, want %s", c.name, got, c.want)
		}
	}
}

// U-S002 (Test-Module-002 U004): an estimated value below the critical
// threshold is capped at WARN so it cannot pose as a precise critical alert.
func TestSoftPrecisionCapsAtWarning(t *testing.T) {
	now := ts("2026-08-10T06:32:00Z")
	fresh := now.Add(-10 * time.Second)
	th := protocol.DefaultThresholds()

	for _, p := range []protocol.Precision{protocol.PrecisionEstimated, protocol.PrecisionManual} {
		got := protocol.DeriveDisplayStatus(quota("3", p, fresh), th, now)
		if got != protocol.DisplayWarn {
			t.Errorf("precision %s below crit threshold = %s, want WARN", p, got)
		}
	}
	if got := protocol.DeriveDisplayStatus(quota("3", protocol.PrecisionExact, fresh), th, now); got != protocol.DisplayCrit {
		t.Errorf("exact precision = %s, want CRIT", got)
	}
}

// U-S003: balance thresholds are per-currency and only fire when configured.
func TestBalanceThresholds(t *testing.T) {
	now := ts("2026-08-10T06:32:00Z")
	warn := decimal.MustParse("10")
	crit := decimal.MustParse("5")
	th := protocol.Thresholds{WarnAmount: &warn, CritAmount: &crit}

	balance := func(v string) protocol.ProviderMetric {
		return protocol.ProviderMetric{
			ID: "p.balance", Provider: "p", MetricKind: protocol.KindBalance,
			Value: dec(v), Unit: "CNY", Window: protocol.WindowPrepaid,
			ObservedAt: now.Add(-time.Second), Precision: protocol.PrecisionExact,
			SourceKind: protocol.SourceOfficialAPI, Status: protocol.StatusOK,
		}
	}

	cases := map[string]protocol.DisplayStatus{
		"49.58": protocol.DisplayOK,
		"8.20":  protocol.DisplayWarn,
		"4.99":  protocol.DisplayCrit,
	}
	for v, want := range cases {
		if got := protocol.DeriveDisplayStatus(balance(v), th, now); got != want {
			t.Errorf("balance %s = %s, want %s", v, got, want)
		}
	}

	// With no configured amount thresholds a balance never self-alerts.
	if got := protocol.DeriveDisplayStatus(balance("0.01"), protocol.Thresholds{}, now); got != protocol.DisplayOK {
		t.Errorf("unconfigured balance = %s, want OK", got)
	}
}

// U-S004: the header badge distinguishes WAIT, OFFLINE, CRIT and LIVE, and
// OFFLINE outranks CRIT because a disconnected display is showing cached data.
func TestDeriveLinkStatus(t *testing.T) {
	cases := []struct {
		hasSnapshot, connected, crit bool
		want                         protocol.LinkStatus
	}{
		{false, false, false, protocol.LinkWait},
		{false, true, false, protocol.LinkWait},
		{false, true, true, protocol.LinkWait},
		{true, true, false, protocol.LinkLive},
		{true, false, false, protocol.LinkOffline},
		{true, true, true, protocol.LinkCrit},
		{true, false, true, protocol.LinkOffline},
	}
	for _, c := range cases {
		got := protocol.DeriveLinkStatus(c.hasSnapshot, c.connected, c.crit)
		if got != c.want {
			t.Errorf("DeriveLinkStatus(%v,%v,%v) = %s, want %s",
				c.hasSnapshot, c.connected, c.crit, got, c.want)
		}
	}
}
