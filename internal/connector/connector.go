// Package connector defines the contract every data source implements.
//
// A connector performs exactly one read against one upstream and returns
// normalised metrics or a classified error. It never retries on its own:
// MOD-001 4 makes the scheduler the single owner of retry, backoff and
// concurrency, so no connector can create its own request storm.
package connector

import (
	"context"
	"errors"
	"fmt"

	"github.com/galendai/homepi-mon/internal/protocol"
)

// Connector is one upstream data source.
type Connector interface {
	// ID is the stable connector identifier, e.g. "minimax-coding".
	ID() string
	// Provider is the vendor key, e.g. "minimax".
	Provider() string
	// ValidateConfig checks the configuration offline, without network access.
	ValidateConfig() error
	// Collect performs one read. It must honour ctx's deadline and must not
	// retry internally.
	Collect(ctx context.Context) ([]protocol.ProviderMetric, error)
}

// Error is a classified collection failure. The message is already redacted:
// connectors must not put tokens, headers or raw upstream bodies in it.
type Error struct {
	Class protocol.ErrorClass
	// Msg is safe to show to the user and to write to logs.
	Msg string
	// RetryAfterSeconds mirrors an upstream Retry-After header when present.
	RetryAfterSeconds int
}

// Error implements error.
func (e *Error) Error() string {
	if e.Msg == "" {
		return string(e.Class)
	}
	return string(e.Class) + ": " + e.Msg
}

// Errorf builds a classified error with a formatted, redacted message.
func Errorf(class protocol.ErrorClass, format string, args ...any) *Error {
	return &Error{Class: class, Msg: fmt.Sprintf(format, args...)}
}

// Classify extracts the error class from err, defaulting to "upstream" for an
// unclassified failure so it is never silently treated as success.
func Classify(err error) protocol.ErrorClass {
	if err == nil {
		return protocol.ErrNone
	}
	var ce *Error
	if errors.As(err, &ce) {
		return ce.Class
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return protocol.ErrTimeout
	}
	if errors.Is(err, context.Canceled) {
		return protocol.ErrTimeout
	}
	return protocol.ErrUpstream
}

// Message extracts the redacted message from err, or a generic classification
// when the error is not a connector.Error. It never returns the raw error text
// of an unknown error, which could contain a URL with a query-string secret.
func Message(err error) string {
	if err == nil {
		return ""
	}
	var ce *Error
	if errors.As(err, &ce) {
		return ce.Msg
	}
	return "collection failed: " + string(Classify(err))
}

// RetryAfter returns the upstream-requested delay in seconds, or 0.
func RetryAfter(err error) int {
	var ce *Error
	if errors.As(err, &ce) {
		return ce.RetryAfterSeconds
	}
	return 0
}

// AuthActionMessage is the only guidance the daemon gives for an expired
// login. ADR-013 forbids the daemon from logging in, refreshing a token or
// writing back any login state, so the action is always "run the official CLI".
func AuthActionMessage(nodeLabel string) string {
	return "re-authenticate with the official CLI on " + nodeLabel
}
