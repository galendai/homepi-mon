package protocol

import (
	"time"

	"github.com/galendai/homepi-mon/internal/decimal"
)

// DefaultStaleAfter is used when a metric does not carry its own freshness
// budget. HL-Spec 9 targets P95 <= 90s for ordinary metrics, so anything older
// than three minutes is presented as stale rather than current.
const DefaultStaleAfter = 3 * time.Minute

// Thresholds turns a value into a severity (PRD 8.1 P1-FR-005).
//
// Quota metrics are compared as "percent remaining"; balance metrics are
// compared as an absolute amount in the metric's own currency. A zero field
// disables that comparison.
type Thresholds struct {
	// WarnPercentLeft triggers warning when percent remaining <= this value.
	WarnPercentLeft int
	// CritPercentLeft triggers critical when percent remaining <= this value.
	CritPercentLeft int
	// WarnAmount triggers warning when the balance <= this value.
	WarnAmount *decimal.Decimal
	// CritAmount triggers critical when the balance <= this value.
	CritAmount *decimal.Decimal
}

// DefaultThresholds are the PRD 8.1 suggested defaults: <=20% remaining is
// warning, <=10% remaining is critical. Amount thresholds are per-currency and
// therefore configured explicitly, never guessed.
func DefaultThresholds() Thresholds {
	return Thresholds{WarnPercentLeft: 20, CritPercentLeft: 10}
}

// Severity evaluates the numeric part of a metric only. Freshness, precision
// and error handling are applied later by DeriveDisplayStatus.
func (t Thresholds) Severity(m ProviderMetric) MetricStatus {
	switch m.MetricKind {
	case KindQuota:
		pct, ok := m.Percent()
		if !ok {
			// MOD-001 7: with no upstream limit there is no percentage, so we
			// must not manufacture a severity from one.
			return StatusUnknown
		}
		if t.CritPercentLeft > 0 && pct <= t.CritPercentLeft {
			return StatusCritical
		}
		if t.WarnPercentLeft > 0 && pct <= t.WarnPercentLeft {
			return StatusWarning
		}
		return StatusOK
	case KindBalance:
		if m.Value == nil {
			return StatusUnknown
		}
		if t.CritAmount != nil && m.Value.Cmp(*t.CritAmount) <= 0 {
			return StatusCritical
		}
		if t.WarnAmount != nil && m.Value.Cmp(*t.WarnAmount) <= 0 {
			return StatusWarning
		}
		return StatusOK
	default:
		if m.Status.Valid() {
			return m.Status
		}
		return StatusUnknown
	}
}

// IsStale reports whether the reading is older than its freshness budget.
func IsStale(m ProviderMetric, now time.Time) bool {
	budget := m.StaleAfter.D()
	if budget <= 0 {
		budget = DefaultStaleAfter
	}
	return now.Sub(m.ObservedAt) > budget
}

// DeriveDisplayStatus maps a metric to the badge the TUI prints.
//
// Evaluation order follows HL-Spec 7: authentication and availability first,
// then transport errors, then freshness, then the numeric thresholds. Only the
// last step can raise a value-based alert, and estimated or manual precision is
// capped at WARN so an approximation can never masquerade as a precise CRIT.
func DeriveDisplayStatus(m ProviderMetric, t Thresholds, now time.Time) DisplayStatus {
	switch m.ErrorClass {
	case ErrAuth:
		return DisplayAuth
	case ErrUnsupported:
		return DisplayNA
	case ErrSchemaChanged, ErrInvalidConfig, ErrUpstream:
		return DisplayError
	case ErrRateLimited:
		return DisplayDelayed
	}

	if m.Precision == PrecisionUnavailable {
		return DisplayNA
	}
	if m.Status == StatusError {
		return DisplayError
	}

	if m.ErrorClass == ErrTimeout || m.ErrorClass == ErrNetwork || IsStale(m, now) {
		// The previous value is still shown, but explicitly labelled stale so
		// it cannot be mistaken for a live reading (HL-Spec 7).
		return DisplayStale
	}

	severity := t.Severity(m)
	if m.Precision.Soft() && severity == StatusCritical {
		severity = StatusWarning
	}
	switch severity {
	case StatusCritical:
		return DisplayCrit
	case StatusWarning:
		return DisplayWarn
	case StatusUnknown:
		return DisplayNA
	default:
		return DisplayOK
	}
}

// Alerting reports whether a badge counts toward the header alert total.
func (d DisplayStatus) Alerting() bool {
	switch d {
	case DisplayWarn, DisplayCrit, DisplayAuth, DisplayError:
		return true
	default:
		return false
	}
}

// Critical reports whether a badge should escalate the header to CRIT.
func (d DisplayStatus) Critical() bool { return d == DisplayCrit }

// DeriveLinkStatus maps the sync client's view to the header badge
// (UI-001 4.1, 5.1, 5.3, 5.4).
//
// Precedence is WAIT, then OFFLINE, then CRIT, then LIVE. OFFLINE deliberately
// outranks CRIT: while the link is down every value on screen is a
// last-known-good reading, and HL-Spec 7 evaluates freshness before value, so
// the header must not assert a live critical condition it cannot confirm.
// The footer alert count still reports the severity of the cached data.
func DeriveLinkStatus(hasSnapshot, connected, anyCritical bool) LinkStatus {
	switch {
	case !hasSnapshot:
		return LinkWait
	case !connected:
		return LinkOffline
	case anyCritical:
		return LinkCrit
	default:
		return LinkLive
	}
}
