// Package scheduler owns retry, backoff, jitter and concurrency for all
// connectors (MOD-001 6).
package scheduler

import (
	"math"
	"time"

	"github.com/galendai/homepi-mon/internal/protocol"
)

// Policy encodes the MOD-001 6.1 retry rules.
type Policy struct {
	// Interval is the steady-state period after a successful collection.
	Interval time.Duration
	// MinBackoff is the first delay after a transient failure.
	MinBackoff time.Duration
	// MaxBackoff caps exponential growth.
	MaxBackoff time.Duration
	// AuthBackoff is the minimum wait after a 401/403. MOD-001 6.1 sets at
	// least 15 minutes so an expired login cannot become a retry storm.
	AuthBackoff time.Duration
	// UnsupportedBackoff applies to sources with no stable query capability.
	UnsupportedBackoff time.Duration
	// JitterFraction is the maximum random spread applied to every delay,
	// as a fraction of the delay (0.1 == +/-10%).
	JitterFraction float64
	// FastRetries is the number of immediate retries allowed for 5xx and
	// network timeouts before normal backoff takes over.
	FastRetries int
}

// DefaultPolicy returns the MOD-001 6.1 defaults for the given interval.
func DefaultPolicy(interval time.Duration) Policy {
	if interval <= 0 {
		interval = time.Minute
	}
	return Policy{
		Interval:           interval,
		MinBackoff:         5 * time.Second,
		MaxBackoff:         10 * time.Minute,
		AuthBackoff:        15 * time.Minute,
		UnsupportedBackoff: time.Hour,
		JitterFraction:     0.1,
		FastRetries:        1,
	}
}

// Delay returns the base wait before the next attempt, before jitter.
//
// failures counts consecutive failures including the one just observed;
// retryAfter is the upstream Retry-After value in seconds, which always wins
// when present (MOD-001 6.1).
func (p Policy) Delay(class protocol.ErrorClass, failures int, retryAfterSeconds int) time.Duration {
	switch class {
	case protocol.ErrNone:
		return p.Interval

	case protocol.ErrAuth:
		return p.AuthBackoff

	case protocol.ErrUnsupported:
		return p.UnsupportedBackoff

	case protocol.ErrInvalidConfig:
		// Only a configuration change can fix this, so poll rarely rather than
		// hammering an endpoint that cannot succeed.
		return p.UnsupportedBackoff

	case protocol.ErrRateLimited:
		if retryAfterSeconds > 0 {
			return time.Duration(retryAfterSeconds) * time.Second
		}
		return p.exponential(failures)

	case protocol.ErrTimeout, protocol.ErrNetwork, protocol.ErrUpstream:
		if failures <= p.FastRetries {
			return p.MinBackoff
		}
		return p.exponential(failures)

	case protocol.ErrSchemaChanged:
		// A schema change needs a new build; retry slowly so the old value can
		// stay visible without generating load.
		return p.MaxBackoff

	default:
		return p.exponential(failures)
	}
}

func (p Policy) exponential(failures int) time.Duration {
	if failures < 1 {
		failures = 1
	}
	// Cap the shift before it can overflow the multiplication.
	shift := failures - 1
	if shift > 20 {
		shift = 20
	}
	d := time.Duration(float64(p.MinBackoff) * math.Pow(2, float64(shift)))
	if d > p.MaxBackoff || d <= 0 {
		d = p.MaxBackoff
	}
	return d
}

// Jitter spreads a delay by up to JitterFraction, using the caller-supplied
// random value in [0,1). Startup jitter and retry jitter both use this, so
// connectors never line up on the same tick (MOD-001 6.1).
func (p Policy) Jitter(d time.Duration, random float64) time.Duration {
	if p.JitterFraction <= 0 || d <= 0 {
		return d
	}
	if random < 0 {
		random = 0
	}
	if random >= 1 {
		random = 0.999999
	}
	// Map [0,1) to [-f, +f).
	spread := (random*2 - 1) * p.JitterFraction
	out := time.Duration(float64(d) * (1 + spread))
	if out < 0 {
		return 0
	}
	return out
}

// NextDelay returns the fully jittered wait while preserving Retry-After as a
// hard lower bound. An upstream rate-limit window may be lengthened by jitter
// but must never be shortened.
func (p Policy) NextDelay(class protocol.ErrorClass, failures int,
	retryAfterSeconds int, random float64) time.Duration {
	delay := p.Jitter(p.Delay(class, failures, retryAfterSeconds), random)
	if retryAfterSeconds > 0 {
		floor := time.Duration(retryAfterSeconds) * time.Second
		if delay < floor {
			delay = floor
		}
	}
	return delay
}

// StartupJitter returns the initial delay before a connector's first run,
// spreading it across 0..10% of its interval.
func (p Policy) StartupJitter(random float64) time.Duration {
	if random < 0 {
		random = 0
	}
	if random >= 1 {
		random = 0.999999
	}
	return time.Duration(float64(p.Interval) * p.JitterFraction * random)
}

// Blocked reports whether an error class means the connector is waiting on a
// user action rather than on the network.
func Blocked(class protocol.ErrorClass) bool {
	return class == protocol.ErrAuth || class == protocol.ErrInvalidConfig
}

// StateFor maps an error class to the published connector state.
func StateFor(class protocol.ErrorClass, enabled bool) protocol.ConnectorState {
	if !enabled {
		return protocol.ConnDisabled
	}
	switch class {
	case protocol.ErrNone:
		return protocol.ConnOK
	case protocol.ErrAuth:
		return protocol.ConnBlockedAuth
	case protocol.ErrRateLimited, protocol.ErrTimeout, protocol.ErrNetwork:
		return protocol.ConnDegraded
	default:
		return protocol.ConnError
	}
}
