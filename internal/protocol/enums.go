package protocol

import "fmt"

// MetricKind classifies what a ProviderMetric measures (HL-Spec 5.2).
// Kinds are never mixed: a prepaid balance must not be rendered as a quota
// percentage.
type MetricKind string

const (
	KindQuota        MetricKind = "quota"
	KindBalance      MetricKind = "balance"
	KindCost         MetricKind = "cost"
	KindTokens       MetricKind = "tokens"
	KindRequests     MetricKind = "requests"
	KindAvailability MetricKind = "availability"
)

var metricKinds = map[MetricKind]bool{
	KindQuota: true, KindBalance: true, KindCost: true,
	KindTokens: true, KindRequests: true, KindAvailability: true,
}

// Valid reports whether the kind is a known enum member.
func (k MetricKind) Valid() bool { return metricKinds[k] }

// Window is the statistical window a metric is measured over (HL-Spec 5.2).
type Window string

const (
	WindowRolling5h  Window = "rolling_5h"
	WindowDaily      Window = "daily"
	WindowWeekly     Window = "weekly"
	WindowMonthly    Window = "monthly"
	WindowBillingCyc Window = "billing_cycle"
	WindowPrepaid    Window = "prepaid"
	WindowInstant    Window = "instant"
)

var windows = map[Window]bool{
	WindowRolling5h: true, WindowDaily: true, WindowWeekly: true,
	WindowMonthly: true, WindowBillingCyc: true, WindowPrepaid: true, WindowInstant: true,
}

// Valid reports whether the window is a known enum member.
func (w Window) Valid() bool { return windows[w] }

// Precision is the confidence tier of a value (PRD 5.2, HL-Spec 5.2).
// Estimated and manual values may never raise a precise critical alert.
type Precision string

const (
	PrecisionExact       Precision = "exact"
	PrecisionVerified    Precision = "verified"
	PrecisionEstimated   Precision = "estimated"
	PrecisionManual      Precision = "manual"
	PrecisionUnavailable Precision = "unavailable"
)

var precisions = map[Precision]bool{
	PrecisionExact: true, PrecisionVerified: true, PrecisionEstimated: true,
	PrecisionManual: true, PrecisionUnavailable: true,
}

// Valid reports whether the precision is a known enum member.
func (p Precision) Valid() bool { return precisions[p] }

// Soft reports whether the precision tier is capped at warning severity.
func (p Precision) Soft() bool {
	return p == PrecisionEstimated || p == PrecisionManual
}

// SourceKind records where a reading came from (HL-Spec 5.2).
type SourceKind string

const (
	SourceOfficialAPI    SourceKind = "official_api"
	SourceOfficialCLI    SourceKind = "official_cli"
	SourceOfficialExport SourceKind = "official_export"
	SourceManual         SourceKind = "manual"
	SourceCompatAPI      SourceKind = "compatibility_api"
	SourceMock           SourceKind = "mock"
)

var sourceKinds = map[SourceKind]bool{
	SourceOfficialAPI: true, SourceOfficialCLI: true, SourceOfficialExport: true,
	SourceManual: true, SourceCompatAPI: true, SourceMock: true,
}

// Valid reports whether the source kind is a known enum member.
func (s SourceKind) Valid() bool { return sourceKinds[s] }

// MetricStatus is the health of a single metric (HL-Spec 5.2).
type MetricStatus string

const (
	StatusOK       MetricStatus = "ok"
	StatusWarning  MetricStatus = "warning"
	StatusCritical MetricStatus = "critical"
	StatusUnknown  MetricStatus = "unknown"
	StatusError    MetricStatus = "error"
	StatusStale    MetricStatus = "stale"
)

var metricStatuses = map[MetricStatus]bool{
	StatusOK: true, StatusWarning: true, StatusCritical: true,
	StatusUnknown: true, StatusError: true, StatusStale: true,
}

// Valid reports whether the status is a known enum member.
func (s MetricStatus) Valid() bool { return metricStatuses[s] }

// ErrorClass is the collector-side failure taxonomy (MOD-001 6.2). It drives
// retry policy and the AUTH / N/A / ERROR display semantics.
type ErrorClass string

const (
	ErrNone          ErrorClass = ""
	ErrAuth          ErrorClass = "auth"
	ErrRateLimited   ErrorClass = "rate_limited"
	ErrTimeout       ErrorClass = "timeout"
	ErrNetwork       ErrorClass = "network"
	ErrSchemaChanged ErrorClass = "schema_changed"
	ErrUnsupported   ErrorClass = "unsupported"
	ErrInvalidConfig ErrorClass = "invalid_config"
	ErrUpstream      ErrorClass = "upstream"
)

var errorClasses = map[ErrorClass]bool{
	ErrNone: true, ErrAuth: true, ErrRateLimited: true, ErrTimeout: true,
	ErrNetwork: true, ErrSchemaChanged: true, ErrUnsupported: true,
	ErrInvalidConfig: true, ErrUpstream: true,
}

// Valid reports whether the error class is a known enum member.
func (e ErrorClass) Valid() bool { return errorClasses[e] }

// ConnectorState summarises a connector for the diagnostics page.
type ConnectorState string

const (
	ConnOK          ConnectorState = "ok"
	ConnDegraded    ConnectorState = "degraded"
	ConnBlockedAuth ConnectorState = "blocked_auth"
	ConnError       ConnectorState = "error"
	ConnDisabled    ConnectorState = "disabled"
)

var connectorStates = map[ConnectorState]bool{
	ConnOK: true, ConnDegraded: true, ConnBlockedAuth: true,
	ConnError: true, ConnDisabled: true,
}

// Valid reports whether the connector state is a known enum member.
func (c ConnectorState) Valid() bool { return connectorStates[c] }

// DisplayStatus is the badge the TUI prints next to a metric (UI-001 8).
// It is always derived, never sent by a connector, so the wire format cannot
// be used to force a misleading badge.
type DisplayStatus string

const (
	DisplayOK      DisplayStatus = "OK"
	DisplayWarn    DisplayStatus = "WARN"
	DisplayCrit    DisplayStatus = "CRIT"
	DisplayDelayed DisplayStatus = "DELAY"
	DisplayStale   DisplayStatus = "STALE"
	DisplayAuth    DisplayStatus = "AUTH"
	DisplayNA      DisplayStatus = "N/A"
	DisplayError   DisplayStatus = "ERROR"
)

// LinkStatus is the connection badge in the header (UI-001 4.1, 5.1, 5.3).
type LinkStatus string

const (
	LinkLive    LinkStatus = "LIVE"
	LinkOffline LinkStatus = "OFFLINE"
	LinkWait    LinkStatus = "WAIT"
	LinkCrit    LinkStatus = "CRIT"
)

// enumError builds a consistent validation message that names the offending
// field and value without echoing anything secret.
func enumError(field string, value any) error {
	return fmt.Errorf("%s: unknown value %q", field, fmt.Sprint(value))
}
