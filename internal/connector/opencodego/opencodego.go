// Package opencodego reads OpenCode Go subscription quota using its API key.
package opencodego

import (
	"context"
	"time"

	"github.com/galendai/homepi-mon/internal/config"
	"github.com/galendai/homepi-mon/internal/connector"
	"github.com/galendai/homepi-mon/internal/connector/providerutil"
	"github.com/galendai/homepi-mon/internal/decimal"
	"github.com/galendai/homepi-mon/internal/protocol"
	"github.com/galendai/homepi-mon/internal/providermeta"
	"github.com/galendai/homepi-mon/internal/secretstore"
)

const typeID = "opencode_go"

func init() {
	connector.Register(typeID, factory)
	providermeta.Register(providermeta.TypeMeta{
		TypeID: typeID, Label: "OpenCode Go", Provider: "opencode",
		RequiresSecret: true, SecretFieldLabel: "OpenCode Go API Key",
		MinInterval: 30 * time.Second, MinStaleAfter: 30 * time.Second,
		SupportedRegions: []string{"global", "custom"}, DefaultRegion: "global",
		DefaultBaseURL:   "https://opencode.ai",
		Description:      "OpenCode Go subscription remaining quota for 5-hour, weekly and monthly windows. Requires a Go API key from the OpenCode console.",
		MetricIDSuffixes: []string{"5h", "weekly", "monthly"},
	})
}

type Connector struct {
	spec    config.ProviderConfig
	secrets secretstore.Store
	runtime providerutil.Runtime
}

type usageWindow struct {
	Status   string `json:"status"`
	Percent  any    `json:"percent"`
	ResetsAt string `json:"resetsAt"`
	// Older deployments return the analyzer output directly.
	UsagePercent any `json:"usagePercent"`
	ResetInSec   any `json:"resetInSec"`
}

type usage struct {
	Rolling *usageWindow `json:"rolling"`
	Weekly  *usageWindow `json:"weekly"`
	Monthly *usageWindow `json:"monthly"`
}

type response struct {
	Usage   *usage       `json:"usage"`
	Rolling *usageWindow `json:"rollingUsage"`
	Weekly  *usageWindow `json:"weeklyUsage"`
	Monthly *usageWindow `json:"monthlyUsage"`
}

func New(spec config.ProviderConfig, secrets secretstore.Store, runtime providerutil.Runtime) *Connector {
	return &Connector{spec: spec, secrets: secrets, runtime: providerutil.NormalizeRuntime(runtime)}
}

func factory(spec config.ProviderConfig, secrets secretstore.Store) (connector.Connector, error) {
	return New(spec, secrets, providerutil.Runtime{}), nil
}

func (c *Connector) ID() string       { return c.spec.ID }
func (c *Connector) Provider() string { return "opencode" }

func (c *Connector) ValidateConfig() error {
	if c.spec.SecretRef == "" || c.spec.AuthFile != "" {
		return connector.Errorf(protocol.ErrInvalidConfig, "OpenCode Go requires secret_ref and does not accept auth_file")
	}
	if c.spec.Region != "global" && c.spec.Region != "custom" {
		return connector.Errorf(protocol.ErrInvalidConfig, "OpenCode Go region must be global or custom")
	}
	_, err := providerutil.BaseURL(c.spec, "https://opencode.ai", "")
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
	base, _ := providerutil.BaseURL(c.spec, "https://opencode.ai", "")
	var payload response
	if err := connector.GetJSON(ctx, connector.JSONRequest{
		Client: c.runtime.HTTPClient, URL: base + "/zen/go/v1/usage", BearerToken: secret,
	}, &payload); err != nil {
		return nil, err
	}
	return c.parse(&payload)
}

func (c *Connector) parse(payload *response) ([]protocol.ProviderMetric, error) {
	now := c.runtime.Now().UTC()
	rows := usage{Rolling: payload.Rolling, Weekly: payload.Weekly, Monthly: payload.Monthly}
	current := payload.Usage != nil
	if current {
		rows = *payload.Usage
	}
	metrics := make([]protocol.ProviderMetric, 0, 3)
	for i, item := range []struct {
		suffix string
		window protocol.Window
		row    *usageWindow
	}{
		{"5h", protocol.WindowRolling5h, rows.Rolling},
		{"weekly", protocol.WindowWeekly, rows.Weekly},
		{"monthly", protocol.WindowMonthly, rows.Monthly},
	} {
		if item.row == nil {
			return nil, schemaError()
		}
		row := item.row
		if row.Status != "ok" && row.Status != "rate-limited" {
			return nil, schemaError()
		}
		percent := row.Percent
		if !current {
			percent = row.UsagePercent
		}
		used, err := providerutil.Decimal(percent)
		limit := decimal.MustParse("100")
		if err != nil || used.Cmp(decimal.Decimal{}) < 0 || used.Cmp(limit) > 0 {
			return nil, schemaError()
		}
		if row.Status == "rate-limited" && used.Cmp(limit) != 0 {
			return nil, schemaError()
		}
		left, err := limit.Sub(used)
		if err != nil {
			return nil, schemaError()
		}
		var reset time.Time
		if current {
			reset, err = time.Parse(time.RFC3339, row.ResetsAt)
		} else {
			seconds, parseErr := providerutil.Decimal(row.ResetInSec)
			if parseErr != nil || seconds.Cmp(decimal.Decimal{}) < 0 {
				return nil, schemaError()
			}
			var duration time.Duration
			duration, err = time.ParseDuration(seconds.String() + "s")
			reset = now.Add(duration)
		}
		if err != nil || reset.IsZero() {
			return nil, schemaError()
		}
		reset = reset.UTC()
		metrics = append(metrics, protocol.ProviderMetric{
			ID: c.spec.ID + "." + item.suffix, Provider: c.Provider(), AccountLabel: c.spec.AccountLabel,
			DisplayName: "OpenCode Go", MetricKind: protocol.KindQuota,
			Value: &left, Limit: &limit, Unit: "percent", Window: item.window,
			ResetsAt: &reset, ObservedAt: now, Precision: protocol.PrecisionVerified,
			SourceKind: protocol.SourceCompatAPI, Status: protocol.StatusOK,
			Derived: []string{"remaining", "percent"}, Group: "coding", Order: 50 + i,
		})
	}
	return metrics, nil
}

func schemaError() error {
	return connector.Errorf(protocol.ErrSchemaChanged, "OpenCode Go usage windows are missing or invalid")
}

var _ connector.Connector = (*Connector)(nil)
