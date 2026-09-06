// Package grokusage reads official Grok CLI auth state without modifying it
// and queries the CLI's compatibility billing endpoint.
package grokusage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
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

const (
	typeID                = "grok_usage"
	defaultBaseURL        = "https://cli-chat-proxy.grok.com/v1"
	billingPath           = "/billing?format=credits"
	tokenAuthHeader       = "xai-grok-cli"
	clientModeHeaderValue = "interactive"
	defaultClientVersion  = "homepi-node/phase1"
	maxAuthBytes          = 1 << 20
	maxVersionBytes       = 64 << 10
)

func init() {
	connector.Register(typeID, factory)
	providermeta.Register(providermeta.TypeMeta{
		TypeID:             typeID,
		Label:              "Grok Usage",
		Provider:           "grok",
		RequiresAuthFile:   true,
		AuthFileFieldLabel: "Grok CLI auth.json Path",
		MinInterval:        30 * time.Second,
		MinStaleAfter:      30 * time.Second,
		SupportedRegions:   []string{"global", "custom"},
		DefaultRegion:      "global",
		DefaultBaseURL:     defaultBaseURL,
		Description:        "Reads the official Grok CLI auth.json locally and actively queries its billing credits endpoint.",
		MetricIDSuffixes:   []string{"weekly"},
	})
}

// Connector reads the current consumer weekly subscription quota.
type Connector struct {
	spec    config.ProviderConfig
	runtime providerutil.Runtime
}

type billingResponse struct {
	Config *billingConfig `json:"config"`
}

type billingConfig struct {
	CreditUsagePercent json.RawMessage `json:"creditUsagePercent"`
	CurrentPeriod      *usagePeriod    `json:"currentPeriod"`
	UnifiedBillingUser bool            `json:"isUnifiedBillingUser"`
}

type usagePeriod struct {
	Type  string    `json:"type"`
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

type billingRecord struct {
	observedAt time.Time
	period     usagePeriod
	used       *decimal.Decimal
}

type authEntry struct {
	Key       string          `json:"key"`
	UserID    string          `json:"user_id"`
	AuthMode  string          `json:"auth_mode"`
	ExpiresAt json.RawMessage `json:"expires_at"`
}

type authState struct {
	key    string
	userID string
}

type authCandidate struct {
	state   authState
	mode    int
	expires time.Time
	scope   string
}

// New builds a Grok usage connector with injectable clock and HTTP
// dependencies.
func New(spec config.ProviderConfig, runtime providerutil.Runtime) *Connector {
	return &Connector{spec: spec, runtime: providerutil.NormalizeRuntime(runtime)}
}

func factory(spec config.ProviderConfig, _ secretstore.Store) (connector.Connector, error) {
	return New(spec, providerutil.Runtime{}), nil
}

func (c *Connector) ID() string       { return c.spec.ID }
func (c *Connector) Provider() string { return "grok" }

// ValidateConfig checks the local auth-file and endpoint shape without
// reading credentials or making a network request.
func (c *Connector) ValidateConfig() error {
	if c.spec.SecretRef != "" {
		return connector.Errorf(protocol.ErrInvalidConfig, "Grok Usage does not accept secret_ref")
	}
	if c.spec.Region == "cn" {
		return connector.Errorf(protocol.ErrInvalidConfig, "Grok Usage has no cn endpoint")
	}
	if c.spec.Region != "custom" && c.spec.BaseURL != "" {
		return connector.Errorf(protocol.ErrInvalidConfig, "Grok Usage only accepts base_url with region=custom")
	}
	if _, err := providerutil.ExpandGrokAuthFile(c.spec.AuthFile); err != nil {
		return err
	}
	_, err := providerutil.BaseURL(c.spec, defaultBaseURL, defaultBaseURL)
	return err
}

// Collect reads the local auth state once and actively fetches one bounded
// billing response from the official CLI proxy.
func (c *Connector) Collect(ctx context.Context) ([]protocol.ProviderMetric, error) {
	if err := c.ValidateConfig(); err != nil {
		return nil, err
	}
	now := c.runtime.Now().UTC()
	auth, authPath, err := readAuthFile(c.spec.AuthFile, now)
	if err != nil {
		return nil, err
	}
	base, err := providerutil.BaseURL(c.spec, defaultBaseURL, defaultBaseURL)
	if err != nil {
		return nil, err
	}
	var payload billingResponse
	if err := connector.GetJSON(ctx, connector.JSONRequest{
		Client:      grokHTTPClient(c.runtime.HTTPClient),
		URL:         base + billingPath,
		BearerToken: auth.key,
		Headers: map[string]string{
			"X-XAI-Token-Auth":      tokenAuthHeader,
			"x-userid":              auth.userID,
			"x-grok-client-version": readClientVersion(authPath),
			"x-grok-client-mode":    clientModeHeaderValue,
		},
	}, &payload); err != nil {
		return nil, err
	}
	record, err := parseBilling(&payload, now)
	if err != nil {
		return nil, err
	}
	return c.metric(record)
}

func grokHTTPClient(base *http.Client) *http.Client {
	if base == nil {
		return &http.Client{Timeout: 15 * time.Second}
	}
	client := *base
	if client.Timeout <= 0 {
		client.Timeout = 15 * time.Second
	}
	return &client
}

func parseBilling(payload *billingResponse, observedAt time.Time) (billingRecord, error) {
	if payload == nil || payload.Config == nil || payload.Config.CurrentPeriod == nil {
		return billingRecord{}, connector.Errorf(protocol.ErrSchemaChanged, "Grok billing config is missing")
	}
	period := *payload.Config.CurrentPeriod
	if period.Start.IsZero() || period.End.IsZero() || !period.End.After(period.Start) {
		return billingRecord{}, connector.Errorf(protocol.ErrSchemaChanged, "Grok billing timestamps are invalid")
	}
	if !isWeeklyPeriod(period.Type) {
		return billingRecord{}, connector.Errorf(protocol.ErrSchemaChanged, "Grok billing period is not weekly")
	}
	rawPercent := bytes.TrimSpace(payload.Config.CreditUsagePercent)
	if len(rawPercent) == 0 || bytes.Equal(rawPercent, []byte("null")) {
		if payload.Config.UnifiedBillingUser {
			return billingRecord{observedAt: observedAt.UTC(), period: period}, nil
		}
		return billingRecord{}, connector.Errorf(protocol.ErrSchemaChanged, "Grok credit usage is invalid")
	}
	used, err := parsePercent(rawPercent)
	limit := decimal.MustParse("100")
	if err != nil || used.Cmp(decimal.Decimal{}) < 0 || used.Cmp(limit) > 0 {
		return billingRecord{}, connector.Errorf(protocol.ErrSchemaChanged, "Grok credit usage is invalid")
	}
	return billingRecord{observedAt: observedAt.UTC(), period: period, used: &used}, nil
}

func (c *Connector) metric(record billingRecord) ([]protocol.ProviderMetric, error) {
	reset := record.period.End.UTC()
	observed := record.observedAt.UTC()
	metric := protocol.ProviderMetric{
		ID: c.spec.ID + ".weekly", Provider: "grok", AccountLabel: c.spec.AccountLabel,
		DisplayName: "Grok", MetricKind: protocol.KindQuota,
		Unit: "percent", Window: protocol.WindowWeekly,
		ResetsAt: &reset, ObservedAt: observed,
		Precision: protocol.PrecisionUnavailable, SourceKind: protocol.SourceCompatAPI,
		Status: protocol.StatusUnknown,
		Group:  "coding", Order: 23,
	}
	if record.used == nil {
		return []protocol.ProviderMetric{metric}, nil
	}
	limit := decimal.MustParse("100")
	left, err := limit.Sub(*record.used)
	if err != nil {
		return nil, connector.Errorf(protocol.ErrSchemaChanged, "Grok credit usage is invalid")
	}
	metric.Value = &left
	metric.Limit = &limit
	metric.Precision = protocol.PrecisionVerified
	metric.Status = protocol.StatusOK
	metric.Derived = []string{"remaining", "percent"}
	return []protocol.ProviderMetric{metric}, nil
}

func readAuthFile(configured string, now time.Time) (authState, string, error) {
	path, err := providerutil.ExpandGrokAuthFile(configured)
	if err != nil {
		return authState{}, "", err
	}
	raw, err := readStableAuth(path)
	if err != nil {
		return authState{}, "", err
	}
	var entries map[string]authEntry
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(&entries); err != nil || entries == nil {
		return authState{}, "", connector.Errorf(protocol.ErrAuth, "Grok login state is invalid; run the official CLI")
	}
	var trailing any
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		return authState{}, "", connector.Errorf(protocol.ErrAuth, "Grok login state contains trailing data")
	}
	selected, ok := selectAuthEntry(entries, now)
	if !ok {
		return authState{}, "", connector.Errorf(protocol.ErrAuth, "Grok login state is unavailable or expired; run the official CLI")
	}
	return selected.state, path, nil
}

func readStableAuth(path string) ([]byte, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, connector.Errorf(protocol.ErrAuth, "Grok login state is unavailable; run the official CLI")
	}
	if before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() {
		return nil, connector.Errorf(protocol.ErrInvalidConfig, "Grok auth_file must be a regular file, not a link")
	}
	if before.Size() > maxAuthBytes {
		return nil, connector.Errorf(protocol.ErrAuth, "Grok login state cannot be read safely")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, connector.Errorf(protocol.ErrAuth, "Grok login state cannot be read; run the official CLI")
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(before, opened) {
		return nil, connector.Errorf(protocol.ErrAuth, "Grok login state changed while opening")
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxAuthBytes+1))
	if err != nil || len(raw) > maxAuthBytes {
		return nil, connector.Errorf(protocol.ErrAuth, "Grok login state cannot be read safely")
	}
	after, err := os.Lstat(path)
	if err != nil || !os.SameFile(opened, after) || opened.Size() != after.Size() ||
		opened.Mode() != after.Mode() || !opened.ModTime().Equal(after.ModTime()) {
		return nil, connector.Errorf(protocol.ErrAuth, "Grok login state changed during collection")
	}
	return raw, nil
}

func selectAuthEntry(entries map[string]authEntry, now time.Time) (authCandidate, bool) {
	var best authCandidate
	found := false
	for scope, entry := range entries {
		mode := authModeRank(scope, entry.AuthMode)
		if mode == 0 || strings.TrimSpace(entry.Key) == "" || strings.TrimSpace(entry.UserID) == "" {
			continue
		}
		expires, ok := parseExpiry(entry.ExpiresAt)
		if !ok || (!expires.IsZero() && !expires.After(now)) {
			continue
		}
		candidate := authCandidate{
			state: authState{key: strings.TrimSpace(entry.Key), userID: strings.TrimSpace(entry.UserID)},
			mode:  mode, expires: expires, scope: scope,
		}
		if !found || betterCandidate(candidate, best) {
			best, found = candidate, true
		}
	}
	return best, found
}

func authModeRank(scope, raw string) int {
	mode := strings.ToLower(strings.TrimSpace(raw))
	if strings.Contains(mode, "api") || strings.Contains(strings.ToLower(scope), "api_key") {
		return 0
	}
	switch mode {
	case "oidc", "oauth", "oauth2", "web_login", "grok", "":
		if mode == "oidc" || mode == "oauth" || mode == "oauth2" {
			return 2
		}
		return 1
	default:
		return 0
	}
}

func betterCandidate(a, b authCandidate) bool {
	if a.mode != b.mode {
		return a.mode > b.mode
	}
	if a.expires.IsZero() != b.expires.IsZero() {
		return !a.expires.IsZero()
	}
	if !a.expires.Equal(b.expires) {
		return a.expires.After(b.expires)
	}
	return a.scope < b.scope
}

func parseExpiry(raw json.RawMessage) (time.Time, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return time.Time{}, true
	}
	var value string
	if err := json.Unmarshal(trimmed, &value); err != nil {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, false
	}
	return parsed.UTC(), true
}

func readClientVersion(authPath string) string {
	path := filepath.Join(filepath.Dir(authPath), "version.json")
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() > maxVersionBytes {
		return defaultClientVersion
	}
	f, err := os.Open(path)
	if err != nil {
		return defaultClientVersion
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxVersionBytes+1))
	if err != nil || len(raw) > maxVersionBytes {
		return defaultClientVersion
	}
	var version struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(raw, &version); err != nil || strings.TrimSpace(version.Version) == "" {
		return defaultClientVersion
	}
	return strings.TrimSpace(version.Version)
}

func isWeeklyPeriod(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "weekly", "usage_period_type_weekly":
		return true
	default:
		return false
	}
}

func parsePercent(raw json.RawMessage) (decimal.Decimal, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return decimal.Decimal{}, err
	}
	switch typed := value.(type) {
	case json.Number:
		return providerutil.Decimal(typed)
	case string:
		return providerutil.Decimal(typed)
	default:
		return decimal.Decimal{}, errors.New("credit usage is not numeric")
	}
}

var _ connector.Connector = (*Connector)(nil)
