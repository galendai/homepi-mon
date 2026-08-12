// Package minimax implements MiniMax Token Plan with the legacy Coding Plan
// endpoint as a single bounded compatibility fallback.
package minimax

import (
	"context"

	"github.com/galendai/homepi-mon/internal/config"
	"github.com/galendai/homepi-mon/internal/connector"
	"github.com/galendai/homepi-mon/internal/connector/providerutil"
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

type response struct {
	BaseResp *struct {
		StatusCode *int `json:"status_code"`
	} `json:"base_resp"`
	PlanName             string `json:"plan_name"`
	CurrentSubscribeName string `json:"current_subscribe_title"`
	ModelRemains         []struct {
		IntervalTotal     any `json:"current_interval_total_count"`
		IntervalUsed      any `json:"current_interval_usage_count"`
		IntervalRemaining any `json:"current_interval_remaining_count"`
		IntervalRemains   any `json:"current_interval_remains_count"`
		WeeklyTotal       any `json:"current_weekly_total_count"`
		WeeklyUsed        any `json:"current_weekly_usage_count"`
		WeeklyRemaining   any `json:"current_weekly_remaining_count"`
		WeeklyRemains     any `json:"current_weekly_remains_count"`
		EndTime           any `json:"end_time"`
		RemainsTime       any `json:"remains_time"`
		WeeklyEndTime     any `json:"weekly_end_time"`
	} `json:"model_remains"`
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
	row := payload.ModelRemains[0]
	remaining := row.IntervalRemaining
	if remaining == nil {
		remaining = row.IntervalRemains
	}
	left, limit, derived, err := providerutil.Remaining(row.IntervalTotal, row.IntervalUsed, remaining)
	if err != nil {
		return nil, connector.Errorf(protocol.ErrSchemaChanged, "MiniMax interval quota is invalid")
	}
	now := c.runtime.Now().UTC()
	intervalReset := row.EndTime
	if intervalReset == nil {
		intervalReset = row.RemainsTime
	}
	metrics := []protocol.ProviderMetric{{
		ID: c.spec.ID + ".5h", Provider: "minimax", AccountLabel: c.spec.AccountLabel,
		DisplayName: "MiniMax", MetricKind: protocol.KindQuota,
		Value: &left, Limit: &limit, Unit: "requests", Window: protocol.WindowRolling5h,
		ResetsAt: providerutil.ResetTime(intervalReset, nil, now), ObservedAt: now,
		Precision: protocol.PrecisionExact, SourceKind: protocol.SourceOfficialAPI,
		Status: protocol.StatusOK, Derived: derived, Group: "coding", Order: 10,
	}}
	weeklyRemaining := row.WeeklyRemaining
	if weeklyRemaining == nil {
		weeklyRemaining = row.WeeklyRemains
	}
	if row.WeeklyTotal != nil {
		weeklyLeft, weeklyLimit, weeklyDerived, weeklyErr := providerutil.Remaining(
			row.WeeklyTotal, row.WeeklyUsed, weeklyRemaining)
		if weeklyErr != nil {
			return nil, connector.Errorf(protocol.ErrSchemaChanged, "MiniMax weekly quota is invalid")
		}
		metrics = append(metrics, protocol.ProviderMetric{
			ID: c.spec.ID + ".weekly", Provider: "minimax", AccountLabel: c.spec.AccountLabel,
			DisplayName: "MiniMax", MetricKind: protocol.KindQuota,
			Value: &weeklyLeft, Limit: &weeklyLimit, Unit: "requests", Window: protocol.WindowWeekly,
			ResetsAt: providerutil.ResetTime(row.WeeklyEndTime, nil, now), ObservedAt: now,
			Precision: protocol.PrecisionExact, SourceKind: protocol.SourceOfficialAPI,
			Status: protocol.StatusOK, Derived: weeklyDerived, Group: "coding", Order: 11,
		})
	}
	return metrics, nil
}

var _ connector.Connector = (*Connector)(nil)
