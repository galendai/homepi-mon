// Package kimiapi implements the official Moonshot/Kimi balance endpoint.
package kimiapi

import (
	"context"
	"strings"

	"github.com/galendai/homepi-mon/internal/config"
	"github.com/galendai/homepi-mon/internal/connector"
	"github.com/galendai/homepi-mon/internal/connector/providerutil"
	"github.com/galendai/homepi-mon/internal/decimal"
	"github.com/galendai/homepi-mon/internal/protocol"
	"github.com/galendai/homepi-mon/internal/providermeta"
	"github.com/galendai/homepi-mon/internal/secretstore"
)

const typeID = "kimi_api"

func init() {
	connector.Register(typeID, factory)
	providermeta.Register(providermeta.TypeMeta{
		TypeID:           typeID,
		Label:            "Kimi (Moonshot) API",
		Provider:         "kimi",
		RequiresSecret:   true,
		SecretFieldLabel: "Kimi API Key",
		MinInterval:      15 * 1e9, // 15s
		MinStaleAfter:    15 * 1e9,
		SupportedRegions: []string{"global", "cn", "custom"},
		DefaultRegion:    "global",
		DefaultBaseURL:   "https://api.moonshot.cn",
		Description:      "Official Moonshot /v1/users/me/balance endpoint. Requires a Provider API Key.",
		MetricIDResolver: func(spec config.ProviderConfig) ([]string, error) {
			currency := "cny"
			if spec.Region == "global" {
				currency = "usd"
			}
			return []string{
				spec.ID + ".available." + currency,
				spec.ID + ".voucher." + currency,
				spec.ID + ".cash." + currency,
			}, nil
		},
	})
}

type Connector struct {
	spec    config.ProviderConfig
	secrets secretstore.Store
	runtime providerutil.Runtime
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
		return connector.Errorf(protocol.ErrInvalidConfig, "Kimi API secret_ref is required")
	}
	_, err := providerutil.BaseURL(c.spec, "https://api.moonshot.ai", "https://api.moonshot.cn")
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
	base, _ := providerutil.BaseURL(c.spec, "https://api.moonshot.ai", "https://api.moonshot.cn")
	var payload struct {
		Code   *int  `json:"code"`
		Status *bool `json:"status"`
		Data   *struct {
			Available any `json:"available_balance"`
			Voucher   any `json:"voucher_balance"`
			Cash      any `json:"cash_balance"`
		} `json:"data"`
	}
	if err := connector.GetJSON(ctx, connector.JSONRequest{
		Client: c.runtime.HTTPClient, URL: base + "/v1/users/me/balance", BearerToken: secret,
	}, &payload); err != nil {
		return nil, err
	}
	if payload.Data == nil || payload.Code == nil || *payload.Code != 0 || payload.Status == nil {
		return nil, connector.Errorf(protocol.ErrSchemaChanged, "Kimi API balance fields are missing")
	}
	currency := "USD"
	if c.spec.Region != "global" {
		currency = "CNY"
	}
	now := c.runtime.Now().UTC()
	values := []struct {
		suffix, name  string
		raw           any
		order         int
		allowNegative bool
	}{
		{"available", "Kimi Available", payload.Data.Available, 50, false},
		{"voucher", "Kimi Voucher", payload.Data.Voucher, 51, false},
		{"cash", "Kimi Cash", payload.Data.Cash, 52, true},
	}
	metrics := make([]protocol.ProviderMetric, 0, len(values))
	for _, item := range values {
		value, err := providerutil.Decimal(item.raw)
		if err != nil || !item.allowNegative && value.Cmp(decimal.Decimal{}) < 0 {
			return nil, connector.Errorf(protocol.ErrSchemaChanged, "Kimi API balance value is invalid")
		}
		status := protocol.StatusOK
		message := ""
		if !*payload.Status {
			status, message = protocol.StatusError, "account unavailable"
		}
		v := value
		metrics = append(metrics, protocol.ProviderMetric{
			ID:       c.spec.ID + "." + item.suffix + "." + strings.ToLower(currency),
			Provider: "kimi", AccountLabel: c.spec.AccountLabel, DisplayName: item.name,
			MetricKind: protocol.KindBalance, Value: &v, Unit: currency,
			Window: protocol.WindowPrepaid, ObservedAt: now,
			Precision: protocol.PrecisionExact, SourceKind: protocol.SourceOfficialAPI,
			Status: status, Message: message, Group: "api", Order: item.order,
		})
	}
	return metrics, nil
}

var _ connector.Connector = (*Connector)(nil)
