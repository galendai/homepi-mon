// Package codexusage reads the official Codex CLI login state without
// modifying it and queries the compatibility wham/usage endpoint once.
package codexusage

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/galendai/homepi-mon/internal/config"
	"github.com/galendai/homepi-mon/internal/connector"
	"github.com/galendai/homepi-mon/internal/connector/providerutil"
	"github.com/galendai/homepi-mon/internal/decimal"
	"github.com/galendai/homepi-mon/internal/protocol"
	"github.com/galendai/homepi-mon/internal/secretstore"
)

const typeID = "codex_usage"

func init() { connector.Register(typeID, factory) }

type Connector struct {
	spec    config.ProviderConfig
	runtime providerutil.Runtime
}

type usageWindow struct {
	UsedPercent        any `json:"used_percent"`
	LimitWindowSeconds any `json:"limit_window_seconds"`
	ResetAt            any `json:"reset_at"`
}

type rateLimitStatus struct {
	Primary   *usageWindow `json:"primary_window"`
	Secondary *usageWindow `json:"secondary_window"`
}

type response struct {
	PlanType   string           `json:"plan_type"`
	RateLimit  *rateLimitStatus `json:"rate_limit"`
	CodeReview *rateLimitStatus `json:"code_review_rate_limit"`
	Additional []struct {
		LimitName      string           `json:"limit_name"`
		MeteredFeature string           `json:"metered_feature"`
		RateLimit      *rateLimitStatus `json:"rate_limit"`
	} `json:"additional_rate_limits"`
}

func New(spec config.ProviderConfig, runtime providerutil.Runtime) *Connector {
	return &Connector{spec: spec, runtime: providerutil.NormalizeRuntime(runtime)}
}

func factory(spec config.ProviderConfig, _ secretstore.Store) (connector.Connector, error) {
	return New(spec, providerutil.Runtime{}), nil
}

func (c *Connector) ID() string       { return c.spec.ID }
func (c *Connector) Provider() string { return "openai" }

func (c *Connector) ValidateConfig() error {
	if c.spec.SecretRef != "" {
		return connector.Errorf(protocol.ErrInvalidConfig, "Codex Usage does not accept secret_ref")
	}
	if c.spec.Region == "cn" {
		return connector.Errorf(protocol.ErrInvalidConfig, "Codex Usage has no cn endpoint")
	}
	_, err := providerutil.ExpandAuthFile(c.spec.AuthFile)
	if err != nil {
		return err
	}
	_, err = providerutil.BaseURL(c.spec, "https://chatgpt.com", "https://chatgpt.com")
	return err
}

func (c *Connector) Collect(ctx context.Context) ([]protocol.ProviderMetric, error) {
	if err := c.ValidateConfig(); err != nil {
		return nil, err
	}
	token, accountID, err := readAuthFile(c.spec.AuthFile)
	if err != nil {
		return nil, err
	}
	base, _ := providerutil.BaseURL(c.spec, "https://chatgpt.com", "https://chatgpt.com")
	headers := map[string]string{}
	if accountID != "" {
		headers["ChatGPT-Account-Id"] = accountID
	}
	var payload response
	if err := connector.GetJSON(ctx, connector.JSONRequest{
		Client: codexHTTP1Client(c.runtime.HTTPClient), URL: base + "/backend-api/wham/usage",
		BearerToken: token, Headers: headers,
	}, &payload); err != nil {
		return nil, err
	}
	return c.parse(&payload)
}

func codexHTTP1Client(base *http.Client) *http.Client {
	if base == nil {
		base = &http.Client{}
	}
	client := *base
	transport := client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	if standard, ok := transport.(*http.Transport); ok {
		clone := standard.Clone()
		protocols := new(http.Protocols)
		protocols.SetHTTP1(true)
		clone.Protocols = protocols
		clone.ForceAttemptHTTP2 = false
		if clone.TLSClientConfig != nil {
			clone.TLSClientConfig = clone.TLSClientConfig.Clone()
			clone.TLSClientConfig.NextProtos = []string{"http/1.1"}
		}
		client.Transport = clone
	}
	return &client
}

func (c *Connector) parse(payload *response) ([]protocol.ProviderMetric, error) {
	if payload.RateLimit == nil || payload.RateLimit.Primary == nil {
		return nil, connector.Errorf(protocol.ErrSchemaChanged, "Codex usage windows are missing")
	}
	now := c.runtime.Now().UTC()
	type metricRow struct {
		suffix, name string
		window       protocol.Window
		row          *usageWindow
		order        int
	}
	rows := []metricRow{
		{"5h", "Codex", protocol.WindowRolling5h, payload.RateLimit.Primary, 20},
		{"weekly", "Codex", protocol.WindowWeekly, payload.RateLimit.Secondary, 21},
	}
	if review := codeReviewWindow(payload); review != nil {
		rows = append(rows, metricRow{"code_review", "Codex Review", protocol.WindowWeekly, review, 22})
	}
	metrics := make([]protocol.ProviderMetric, 0, 3)
	for _, item := range rows {
		if item.row == nil {
			continue
		}
		used, err := providerutil.Decimal(item.row.UsedPercent)
		limit := decimal.MustParse("100")
		if err != nil || used.Cmp(decimal.Decimal{}) < 0 || used.Cmp(limit) > 0 {
			return nil, connector.Errorf(protocol.ErrSchemaChanged, "Codex used_percent is invalid")
		}
		left, err := limit.Sub(used)
		if err != nil {
			return nil, connector.Errorf(protocol.ErrSchemaChanged, "Codex used_percent is invalid")
		}
		metrics = append(metrics, protocol.ProviderMetric{
			ID: c.spec.ID + "." + item.suffix, Provider: "openai", AccountLabel: c.spec.AccountLabel,
			DisplayName: item.name, MetricKind: protocol.KindQuota,
			Value: &left, Limit: &limit, Unit: "percent", Window: item.window,
			ResetsAt: providerutil.ResetTime(item.row.ResetAt, nil, now), ObservedAt: now,
			Precision: protocol.PrecisionVerified, SourceKind: protocol.SourceCompatAPI,
			Status: protocol.StatusOK, Derived: []string{"remaining", "percent"},
			Group: "coding", Order: item.order,
		})
	}
	return metrics, nil
}

func codeReviewWindow(payload *response) *usageWindow {
	if payload.CodeReview != nil {
		if payload.CodeReview.Secondary != nil {
			return payload.CodeReview.Secondary
		}
		if payload.CodeReview.Primary != nil {
			return payload.CodeReview.Primary
		}
	}
	for _, additional := range payload.Additional {
		if additional.RateLimit == nil || !isReviewLimit(additional.LimitName, additional.MeteredFeature) {
			continue
		}
		if additional.RateLimit.Secondary != nil {
			return additional.RateLimit.Secondary
		}
		if additional.RateLimit.Primary != nil {
			return additional.RateLimit.Primary
		}
	}
	return nil
}

func isReviewLimit(values ...string) bool {
	for _, value := range values {
		for _, token := range strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
			return r == '_' || r == '-' || r == ' ' || r == '/'
		}) {
			if token == "review" {
				return true
			}
		}
	}
	return false
}

func readAuthFile(configured string) (string, string, error) {
	path, err := providerutil.ExpandAuthFile(configured)
	if err != nil {
		return "", "", err
	}
	before, err := os.Lstat(path)
	if err != nil {
		return "", "", connector.Errorf(protocol.ErrAuth, "Codex login state is unavailable; run the official CLI")
	}
	if before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() {
		return "", "", connector.Errorf(protocol.ErrInvalidConfig, "Codex auth_file must be a regular file, not a link")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", "", connector.Errorf(protocol.ErrAuth, "Codex login state cannot be read; run the official CLI")
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(before, opened) {
		return "", "", connector.Errorf(protocol.ErrAuth, "Codex login state changed while opening")
	}
	const maxAuthBytes = 1 << 20
	raw, err := io.ReadAll(io.LimitReader(f, maxAuthBytes+1))
	if err != nil || len(raw) > maxAuthBytes {
		return "", "", connector.Errorf(protocol.ErrAuth, "Codex login state cannot be read safely")
	}
	after, err := os.Lstat(path)
	if err != nil || !os.SameFile(opened, after) || opened.Size() != after.Size() ||
		opened.Mode() != after.Mode() || !opened.ModTime().Equal(after.ModTime()) {
		return "", "", connector.Errorf(protocol.ErrAuth, "Codex login state changed during collection")
	}
	var auth struct {
		Tokens *struct {
			AccessToken string `json:"access_token"`
			AccountID   string `json:"account_id"`
		} `json:"tokens"`
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	if err := dec.Decode(&auth); err != nil || auth.Tokens == nil || strings.TrimSpace(auth.Tokens.AccessToken) == "" {
		return "", "", connector.Errorf(protocol.ErrAuth, "Codex login state is invalid; run the official CLI")
	}
	var trailing any
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		return "", "", connector.Errorf(protocol.ErrAuth, "Codex login state contains trailing data")
	}
	return auth.Tokens.AccessToken, auth.Tokens.AccountID, nil
}

var _ connector.Connector = (*Connector)(nil)
