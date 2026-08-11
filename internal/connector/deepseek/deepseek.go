// Package deepseek implements the official DeepSeek balance endpoint.
package deepseek

import (
	"context"
	"strings"

	"github.com/galendai/homepi-mon/internal/config"
	"github.com/galendai/homepi-mon/internal/connector"
	"github.com/galendai/homepi-mon/internal/connector/providerutil"
	"github.com/galendai/homepi-mon/internal/decimal"
	"github.com/galendai/homepi-mon/internal/protocol"
	"github.com/galendai/homepi-mon/internal/secretstore"
)

const typeID = "deepseek_api"

func init() { connector.Register(typeID, factory) }

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
func (c *Connector) Provider() string { return "deepseek" }

func (c *Connector) ValidateConfig() error {
	if c.spec.SecretRef == "" {
		return connector.Errorf(protocol.ErrInvalidConfig, "DeepSeek secret_ref is required")
	}
	_, err := providerutil.BaseURL(c.spec, "https://api.deepseek.com", "https://api.deepseek.com")
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
	base, _ := providerutil.BaseURL(c.spec, "https://api.deepseek.com", "https://api.deepseek.com")
	var payload struct {
		Available    *bool `json:"is_available"`
		BalanceInfos []struct {
			Currency string `json:"currency"`
			Total    any    `json:"total_balance"`
			Granted  any    `json:"granted_balance"`
			ToppedUp any    `json:"topped_up_balance"`
		} `json:"balance_infos"`
	}
	if err := connector.GetJSON(ctx, connector.JSONRequest{
		Client: c.runtime.HTTPClient, URL: base + "/user/balance", BearerToken: secret,
	}, &payload); err != nil {
		return nil, err
	}
	if payload.Available == nil || len(payload.BalanceInfos) == 0 {
		return nil, connector.Errorf(protocol.ErrSchemaChanged, "DeepSeek balance fields are missing")
	}

	now := c.runtime.Now().UTC()
	metrics := make([]protocol.ProviderMetric, 0, len(payload.BalanceInfos)*3)
	seen := map[string]bool{}
	for _, info := range payload.BalanceInfos {
		currency := strings.ToUpper(info.Currency)
		if currency != "CNY" && currency != "USD" || seen[currency] {
			return nil, connector.Errorf(protocol.ErrSchemaChanged, "DeepSeek currency data is invalid")
		}
		seen[currency] = true
		values := []struct {
			suffix, name string
			raw          any
			order        int
		}{
			{"total", "DeepSeek Total", info.Total, 40},
			{"granted", "DeepSeek Granted", info.Granted, 41},
			{"topped_up", "DeepSeek Topped", info.ToppedUp, 42},
		}
		for _, item := range values {
			value, err := providerutil.Decimal(item.raw)
			if err != nil || value.Cmp(decimal.Decimal{}) < 0 {
				return nil, connector.Errorf(protocol.ErrSchemaChanged, "DeepSeek balance value is invalid")
			}
			status := protocol.StatusOK
			message := ""
			if !*payload.Available {
				status = protocol.StatusError
				message = "account unavailable"
			}
			v := value
			metrics = append(metrics, protocol.ProviderMetric{
				ID:       c.spec.ID + "." + item.suffix + "." + strings.ToLower(currency),
				Provider: "deepseek", AccountLabel: c.spec.AccountLabel,
				DisplayName: item.name, MetricKind: protocol.KindBalance,
				Value: &v, Unit: currency, Window: protocol.WindowPrepaid,
				ObservedAt: now, Precision: protocol.PrecisionExact,
				SourceKind: protocol.SourceOfficialAPI, Status: status,
				Message: message, Group: "api", Order: item.order,
			})
		}
	}
	return metrics, nil
}

var _ connector.Connector = (*Connector)(nil)
