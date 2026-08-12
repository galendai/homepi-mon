// Package minimax implements MiniMax Token Plan with the legacy Coding Plan
// endpoint as a single bounded compatibility fallback.
package minimax

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/galendai/homepi-mon/internal/config"
	"github.com/galendai/homepi-mon/internal/connector"
	"github.com/galendai/homepi-mon/internal/connector/providerutil"
	"github.com/galendai/homepi-mon/internal/decimal"
	"github.com/galendai/homepi-mon/internal/protocol"
	"github.com/galendai/homepi-mon/internal/providermeta"
	"github.com/galendai/homepi-mon/internal/secretstore"
)

const typeID = "minimax_coding"

func init() {
	connector.Register(typeID, factory)
	providermeta.Register(providermeta.TypeMeta{
		TypeID:           typeID,
		Label:            "MiniMax Coding Plan",
		Provider:         "minimax",
		RequiresSecret:   true,
		SecretFieldLabel: "MiniMax API Key",
		MinInterval:      15 * 1e9, // 15s
		MinStaleAfter:    15 * 1e9,
		SupportedRegions: []string{"global", "cn", "custom"},
		DefaultRegion:    "global",
		DefaultBaseURL:   "https://api.MiniMax.chat",
		Description:      "MiniMax Token Plan primary endpoint, with the legacy Coding Plan endpoint as a single bounded fallback. Requires a Provider API Key.",
		MetricIDSuffixes: []string{"5h", "weekly"},
	})
}

type Connector struct {
	spec    config.ProviderConfig
	secrets secretstore.Store
	runtime providerutil.Runtime
}

type modelRemain struct {
	ModelName                string `json:"model_name"`
	IntervalTotal            any    `json:"current_interval_total_count"`
	IntervalUsed             any    `json:"current_interval_usage_count"`
	IntervalRemaining        any    `json:"current_interval_remaining_count"`
	IntervalRemains          any    `json:"current_interval_remains_count"`
	IntervalRemainingPercent any    `json:"current_interval_remaining_percent"`
	IntervalStatus           any    `json:"current_interval_status"`
	WeeklyTotal              any    `json:"current_weekly_total_count"`
	WeeklyUsed               any    `json:"current_weekly_usage_count"`
	WeeklyRemaining          any    `json:"current_weekly_remaining_count"`
	WeeklyRemains            any    `json:"current_weekly_remains_count"`
	WeeklyRemainingPercent   any    `json:"current_weekly_remaining_percent"`
	WeeklyStatus             any    `json:"current_weekly_status"`
	EndTime                  any    `json:"end_time"`
	RemainsTime              any    `json:"remains_time"`
	WeeklyEndTime            any    `json:"weekly_end_time"`
}

type response struct {
	BaseResp *struct {
		StatusCode *int `json:"status_code"`
	} `json:"base_resp"`
	PlanName             string        `json:"plan_name"`
	CurrentSubscribeName string        `json:"current_subscribe_title"`
	ModelRemains         []modelRemain `json:"model_remains"`
}

func New(spec config.ProviderConfig, secrets secretstore.Store, runtime providerutil.Runtime) *Connector {
	return &Connector{spec: spec, secrets: secrets, runtime: providerutil.NormalizeRuntime(runtime)}
}

func factory(spec config.ProviderConfig, secrets secretstore.Store) (connector.Connector, error) {
	return New(spec, secrets, providerutil.Runtime{}), nil
}

func (c *Connector) ID() string       { return c.spec.ID }
func (c *Connector) Provider() string { return "minimax" }

func (c *Connector) ValidateConfig() error {
	if c.spec.SecretRef == "" {
		return connector.Errorf(protocol.ErrInvalidConfig, "MiniMax secret_ref is required")
	}
	_, _, err := c.baseURLs()
	return err
}

func (c *Connector) Collect(ctx context.Context) ([]protocol.ProviderMetric, error) {
	if err := c.ValidateConfig(); err != nil {
		return nil, err
	}
	secret, err := providerutil.Secret(ctx, c.spec, c.secrets)
	if err != nil {
		return nil, err
	}
	primaryBase, compatBase, _ := c.baseURLs()
	primary := primaryBase + "/v1/token_plan/remains"
	compat := compatBase + "/v1/api/openplatform/coding_plan/remains"

	payload, err := c.fetch(ctx, primary, secret)
	if err == nil {
		if metrics, parseErr := c.parse(payload); parseErr == nil {
			return metrics, nil
		} else {
			err = parseErr
		}
	}
	if connector.StatusCode(err) != 404 && connector.Classify(err) != protocol.ErrSchemaChanged {
		return nil, err
	}
	payload, err = c.fetch(ctx, compat, secret)
	if err != nil {
		return nil, err
	}
	return c.parse(payload)
}

func (c *Connector) baseURLs() (string, string, error) {
	if c.spec.Region == "custom" {
		base, err := providerutil.BaseURL(c.spec, "", "")
		return base, base, err
	}
	primary, err := providerutil.BaseURL(c.spec, "https://www.minimax.io", "https://www.minimaxi.com")
	if err != nil {
		return "", "", err
	}
	compat, err := providerutil.BaseURL(c.spec, "https://api.minimax.io", "https://api.minimaxi.com")
	return primary, compat, err
}

func (c *Connector) fetch(ctx context.Context, endpoint, secret string) (*response, error) {
	var payload response
	err := connector.GetJSON(ctx, connector.JSONRequest{
		Client: c.runtime.HTTPClient, URL: endpoint, BearerToken: secret,
	}, &payload)
	return &payload, err
}

func (c *Connector) parse(payload *response) ([]protocol.ProviderMetric, error) {
	if payload.BaseResp != nil && payload.BaseResp.StatusCode != nil && *payload.BaseResp.StatusCode != 0 {
		return nil, connector.Errorf(protocol.ErrUpstream, "MiniMax reported an upstream error")
	}
	if len(payload.ModelRemains) == 0 {
		return nil, connector.Errorf(protocol.ErrSchemaChanged, "MiniMax quota fields are missing")
	}
	row, ok := selectQuotaRow(payload.ModelRemains)
	if !ok {
		return nil, connector.Errorf(protocol.ErrSchemaChanged, "MiniMax quota fields are missing")
	}
	now := c.runtime.Now().UTC()
	metrics := make([]protocol.ProviderMetric, 0, 2)
	intervalStatus := quotaStatus(row.IntervalStatus)
	if intervalStatus != 3 {
		left, limit, derived, unit, err := quotaValues(
			row.IntervalTotal, row.IntervalUsed, firstNonNil(row.IntervalRemaining, row.IntervalRemains),
			row.IntervalRemainingPercent,
		)
		if err != nil {
			return nil, connector.Errorf(protocol.ErrSchemaChanged, "MiniMax interval quota is invalid")
		}
		intervalReset := firstNonNil(row.EndTime, row.RemainsTime)
		metrics = append(metrics, quotaMetric(c.spec, ".5h", protocol.WindowRolling5h, 10,
			left, limit, unit, providerutil.ResetTime(intervalReset, nil, now), now, derived))
	}

	weeklyStatus := quotaStatus(row.WeeklyStatus)
	if weeklyStatus != 3 && hasQuotaValue(row.WeeklyTotal, row.WeeklyRemainingPercent) {
		left, limit, derived, unit, err := quotaValues(
			row.WeeklyTotal, row.WeeklyUsed, firstNonNil(row.WeeklyRemaining, row.WeeklyRemains),
			row.WeeklyRemainingPercent,
		)
		if err != nil {
			return nil, connector.Errorf(protocol.ErrSchemaChanged, "MiniMax weekly quota is invalid")
		}
		metrics = append(metrics, quotaMetric(c.spec, ".weekly", protocol.WindowWeekly, 11,
			left, limit, unit, providerutil.ResetTime(row.WeeklyEndTime, nil, now), now, derived))
	}
	if len(metrics) == 0 && (intervalStatus == 3 || weeklyStatus == 3) {
		metrics = append(metrics, protocol.ProviderMetric{
			ID: c.spec.ID + ".5h", Provider: "minimax", AccountLabel: c.spec.AccountLabel,
			DisplayName: "MiniMax", MetricKind: protocol.KindQuota, Unit: "requests",
			Window: protocol.WindowRolling5h, ObservedAt: now,
			Precision: protocol.PrecisionUnavailable, SourceKind: protocol.SourceOfficialAPI,
			Status: protocol.StatusUnknown, Message: "MiniMax interval quota is unlimited",
			Group: "coding", Order: 10,
		})
	}
	return metrics, nil
}

func selectQuotaRow(rows []modelRemain) (modelRemain, bool) {
	candidates := make([]modelRemain, 0, len(rows))
	for _, row := range rows {
		if row.IntervalRemainingPercent != nil || row.WeeklyRemainingPercent != nil ||
			positive(row.IntervalTotal) || positive(row.WeeklyTotal) {
			candidates = append(candidates, row)
		}
	}
	for _, row := range candidates {
		name := strings.ToLower(strings.TrimSpace(row.ModelName))
		if name == "general" || strings.HasPrefix(name, "minimax-m") {
			return row, true
		}
	}
	for _, row := range candidates {
		if status := quotaStatus(row.IntervalStatus); status == 1 || status == 2 {
			return row, true
		}
		if status := quotaStatus(row.WeeklyStatus); status == 1 || status == 2 {
			return row, true
		}
	}
	if len(candidates) == 0 {
		return modelRemain{}, false
	}
	return candidates[0], true
}

func quotaValues(total, used, remaining, remainingPercent any) (
	decimal.Decimal, decimal.Decimal, []string, string, error,
) {
	if remainingPercent != nil {
		left, err := providerutil.Decimal(remainingPercent)
		limit := decimal.New(100, 0)
		if err != nil || left.Cmp(decimal.Decimal{}) < 0 || left.Cmp(limit) > 0 {
			return decimal.Decimal{}, decimal.Decimal{}, nil, "", errInvalidQuota
		}
		return left, limit, []string{"limit"}, "percent", nil
	}
	left, limit, derived, err := providerutil.Remaining(total, used, remaining)
	return left, limit, derived, "requests", err
}

func quotaMetric(spec config.ProviderConfig, suffix string, window protocol.Window, order int,
	left, limit decimal.Decimal, unit string, resetsAt *time.Time, now time.Time, derived []string,
) protocol.ProviderMetric {
	return protocol.ProviderMetric{
		ID: spec.ID + suffix, Provider: "minimax", AccountLabel: spec.AccountLabel,
		DisplayName: "MiniMax", MetricKind: protocol.KindQuota,
		Value: &left, Limit: &limit, Unit: unit, Window: window,
		ResetsAt: resetsAt, ObservedAt: now,
		Precision: protocol.PrecisionExact, SourceKind: protocol.SourceOfficialAPI,
		Status: protocol.StatusOK, Derived: derived, Group: "coding", Order: order,
	}
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func positive(value any) bool {
	n, err := providerutil.Decimal(value)
	return err == nil && n.Cmp(decimal.Decimal{}) > 0
}

func hasQuotaValue(total, remainingPercent any) bool {
	return remainingPercent != nil || positive(total)
}

func quotaStatus(value any) int {
	n, err := providerutil.Decimal(value)
	if err != nil {
		return 0
	}
	switch n.String() {
	case "1":
		return 1
	case "2":
		return 2
	case "3":
		return 3
	default:
		return 0
	}
}

var errInvalidQuota = errors.New("invalid quota")

var _ connector.Connector = (*Connector)(nil)
