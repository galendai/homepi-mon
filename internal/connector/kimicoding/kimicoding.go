// Package kimicoding implements the compatibility Kimi Coding Plan endpoint.
package kimicoding

import (
	"context"
	"sort"
	"strings"

	"github.com/galendai/homepi-mon/internal/config"
	"github.com/galendai/homepi-mon/internal/connector"
	"github.com/galendai/homepi-mon/internal/connector/providerutil"
	"github.com/galendai/homepi-mon/internal/decimal"
	"github.com/galendai/homepi-mon/internal/protocol"
	"github.com/galendai/homepi-mon/internal/providermeta"
	"github.com/galendai/homepi-mon/internal/secretstore"
)

const typeID = "kimi_coding"

func init() {
	connector.Register(typeID, factory)
	providermeta.Register(providermeta.TypeMeta{
		TypeID:           typeID,
		Label:            "Kimi Coding Plan",
		Provider:         "kimi",
		RequiresSecret:   true,
		SecretFieldLabel: "Kimi Coding API Key",
		MinInterval:      15 * 1e9, // 15s
		MinStaleAfter:    15 * 1e9,
		SupportedRegions: []string{"cn", "global", "custom"},
		DefaultRegion:    "cn",
		DefaultBaseURL:   "https://api.kimi.com",
		Description:      "Kimi Coding Plan quota (China mainland: cn). Shows available 5-hour, weekly and monthly quota. Requires a Coding Plan API Key, not a Moonshot Open Platform key.",
		MetricIDSuffixes: []string{"5h", "weekly", "monthly-total", "monthly-code"},
	})
}

type Connector struct {
	spec    config.ProviderConfig
	secrets secretstore.Store
	runtime providerutil.Runtime
}

type limitItem struct {
	Name        string     `json:"name"`
	Title       string     `json:"title"`
	ModelName   string     `json:"model_name"`
	Used        any        `json:"used"`
	UsedAmount  any        `json:"used_amount"`
	Limit       any        `json:"limit"`
	LimitAmount any        `json:"limit_amount"`
	Remaining   any        `json:"remaining"`
	ResetTime   any        `json:"resetTime"`
	ResetAt     any        `json:"reset_at"`
	ResetTime2  any        `json:"reset_time"`
	ResetIn     any        `json:"reset_in"`
	Duration    any        `json:"duration"`
	TimeUnit    string     `json:"timeUnit"`
	Window      windowSpec `json:"window"`
	Detail      *struct {
		Used        any        `json:"used"`
		UsedAmount  any        `json:"used_amount"`
		Limit       any        `json:"limit"`
		LimitAmount any        `json:"limit_amount"`
		Remaining   any        `json:"remaining"`
		ResetTime   any        `json:"resetTime"`
		ResetAt     any        `json:"reset_at"`
		ResetTime2  any        `json:"reset_time"`
		ResetIn     any        `json:"reset_in"`
		Duration    any        `json:"duration"`
		TimeUnit    string     `json:"timeUnit"`
		Window      windowSpec `json:"window"`
	} `json:"detail"`
}

type windowSpec struct {
	Duration any    `json:"duration"`
	TimeUnit string `json:"timeUnit"`
}

type response struct {
	Data   []limitItem          `json:"data"`
	Usage  *limitItem           `json:"usage"`
	Limits []limitItem          `json:"limits"`
	Usages map[string]ratioItem `json:"usages"`
}

type ratioItem struct {
	UsedRatio any `json:"used_ratio"`
	ResetTime any `json:"reset_time"`
}

func New(spec config.ProviderConfig, secrets secretstore.Store, runtime providerutil.Runtime) *Connector {
	return &Connector{spec: spec, secrets: secrets, runtime: providerutil.NormalizeRuntime(runtime)}
}

func factory(spec config.ProviderConfig, secrets secretstore.Store) (connector.Connector, error) {
	return New(spec, secrets, providerutil.Runtime{}), nil
}

func (c *Connector) ID() string       { return c.spec.ID }
func (c *Connector) Provider() string { return "kimi" }

func (c *Connector) ValidateConfig() error {
	if c.spec.SecretRef == "" {
		return connector.Errorf(protocol.ErrInvalidConfig, "Kimi Coding secret_ref is required")
	}
	_, err := providerutil.BaseURL(c.spec, "https://api.kimi.com", "https://api.kimi.com")
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
	base, _ := providerutil.BaseURL(c.spec, "https://api.kimi.com", "https://api.kimi.com")
	payload, err := c.fetch(ctx, base+"/coding/v1/usages", secret)
	if err != nil && connector.StatusCode(err) == 404 {
		payload, err = c.fetch(ctx, base+"/coding/v1/usage", secret)
	}
	if err != nil {
		return nil, err
	}
	return c.parse(payload)
}

func (c *Connector) fetch(ctx context.Context, endpoint, secret string) (*response, error) {
	var payload response
	err := connector.GetJSON(ctx, connector.JSONRequest{
		Client: c.runtime.HTTPClient, URL: endpoint, BearerToken: secret,
	}, &payload)
	return &payload, err
}

func (c *Connector) parse(payload *response) ([]protocol.ProviderMetric, error) {
	rows := payload.Data
	if len(rows) == 0 {
		if payload.Usage != nil {
			usage := *payload.Usage
			// The top-level usage object is the weekly summary, regardless
			// of its display label (matching the official CLI contract).
			usage.Name = "weekly"
			rows = append(rows, usage)
		}
		rows = append(rows, payload.Limits...)
	}
	now := c.runtime.Now().UTC()
	metrics := make([]protocol.ProviderMetric, 0, len(rows))
	seen := map[protocol.Window]bool{}
	// Current ratio windows take precedence over the legacy count windows.
	for _, item := range []struct {
		key, suffix string
		window      protocol.Window
		order       int
	}{
		{"limit_5h", "5h", protocol.WindowRolling5h, 30},
		{"limit_7d", "weekly", protocol.WindowWeekly, 31},
		{"limit_month_total", "monthly-total", protocol.WindowMonthly, 32},
		{"limit_month_code", "monthly-code", protocol.WindowMonthly, 33},
	} {
		row, ok := payload.Usages[item.key]
		if !ok {
			continue
		}
		used, err := providerutil.Decimal(row.UsedRatio)
		limit := decimal.MustParse("1")
		if err != nil || used.Cmp(decimal.MustParse("0")) < 0 || used.Cmp(limit) > 0 {
			return nil, connector.Errorf(protocol.ErrSchemaChanged, "Kimi Coding quota ratio is invalid")
		}
		left, err := limit.Sub(used)
		if err != nil {
			return nil, connector.Errorf(protocol.ErrSchemaChanged, "Kimi Coding quota ratio is invalid")
		}
		metrics = append(metrics, protocol.ProviderMetric{
			ID: c.spec.ID + "." + item.suffix, Provider: "kimi", AccountLabel: c.spec.AccountLabel,
			DisplayName: "Kimi Coding", MetricKind: protocol.KindQuota,
			Value: &left, Limit: &limit, Unit: "ratio", Window: item.window,
			ResetsAt: providerutil.ResetTime(row.ResetTime, nil, now), ObservedAt: now,
			Precision: protocol.PrecisionVerified, SourceKind: protocol.SourceCompatAPI,
			Status: protocol.StatusOK, Derived: []string{"remaining"}, Group: "coding", Order: item.order,
		})
		seen[item.window] = true
	}
	for _, row := range rows {
		row.applyDetail()
		window, ok := classifyWindow(row)
		if !ok || seen[window] {
			continue
		}
		total := row.Limit
		if total == nil {
			total = row.LimitAmount
		}
		used := row.Used
		if used == nil {
			used = row.UsedAmount
		}
		left, limit, derived, err := providerutil.Remaining(total, used, row.Remaining)
		if err != nil {
			return nil, connector.Errorf(protocol.ErrSchemaChanged, "Kimi Coding quota value is invalid")
		}
		reset := row.ResetTime
		if reset == nil {
			reset = row.ResetAt
		}
		if reset == nil {
			reset = row.ResetTime2
		}
		order, suffix := 31, "weekly"
		if window == protocol.WindowRolling5h {
			order, suffix = 30, "5h"
		}
		metrics = append(metrics, protocol.ProviderMetric{
			ID: c.spec.ID + "." + suffix, Provider: "kimi", AccountLabel: c.spec.AccountLabel,
			DisplayName: "Kimi Coding", MetricKind: protocol.KindQuota,
			Value: &left, Limit: &limit, Unit: "requests", Window: window,
			ResetsAt: providerutil.ResetTime(reset, row.ResetIn, now), ObservedAt: now,
			Precision: protocol.PrecisionVerified, SourceKind: protocol.SourceCompatAPI,
			Status: protocol.StatusOK, Derived: derived, Group: "coding", Order: order,
		})
		seen[window] = true
	}
	if len(metrics) == 0 {
		return nil, connector.Errorf(protocol.ErrSchemaChanged, "Kimi Coding windows are unsupported")
	}
	sort.Slice(metrics, func(i, j int) bool { return metrics[i].Order < metrics[j].Order })
	return metrics, nil
}

func (r *limitItem) applyDetail() {
	if r.Detail == nil {
		return
	}
	d := r.Detail
	if d.Used != nil {
		r.Used = d.Used
	}
	if d.UsedAmount != nil {
		r.UsedAmount = d.UsedAmount
	}
	if d.Limit != nil {
		r.Limit = d.Limit
	}
	if d.LimitAmount != nil {
		r.LimitAmount = d.LimitAmount
	}
	if d.Remaining != nil {
		r.Remaining = d.Remaining
	}
	if d.ResetTime != nil {
		r.ResetTime = d.ResetTime
	}
	if d.ResetAt != nil {
		r.ResetAt = d.ResetAt
	}
	if d.ResetTime2 != nil {
		r.ResetTime2 = d.ResetTime2
	}
	if d.ResetIn != nil {
		r.ResetIn = d.ResetIn
	}
	if d.Duration != nil {
		r.Duration = d.Duration
	}
	if d.TimeUnit != "" {
		r.TimeUnit = d.TimeUnit
	}
	if d.Window.Duration != nil || d.Window.TimeUnit != "" {
		r.Window = d.Window
	}
}

func classifyWindow(row limitItem) (protocol.Window, bool) {
	name := strings.ToLower(strings.Join([]string{row.Name, row.Title, row.ModelName}, " "))
	if row.ModelName == "all" || strings.Contains(name, "week") || strings.Contains(name, "7 day") {
		return protocol.WindowWeekly, true
	}
	duration := row.Window.Duration
	unit := row.Window.TimeUnit
	if duration == nil {
		duration, unit = row.Duration, row.TimeUnit
	}
	if duration != nil {
		d, err := providerutil.Decimal(duration)
		if err == nil {
			u := strings.ToUpper(unit)
			if strings.Contains(u, "WEEK") || (strings.Contains(u, "DAY") && d.Cmp(decimal.MustParse("7")) >= 0) {
				return protocol.WindowWeekly, true
			}
			if strings.Contains(u, "HOUR") && d.Cmp(decimal.MustParse("5")) == 0 ||
				strings.Contains(u, "MINUTE") && d.Cmp(decimal.MustParse("300")) == 0 {
				return protocol.WindowRolling5h, true
			}
		}
	}
	if strings.Contains(name, "5h") || strings.Contains(name, "5 hour") {
		return protocol.WindowRolling5h, true
	}
	return "", false
}

var _ connector.Connector = (*Connector)(nil)
