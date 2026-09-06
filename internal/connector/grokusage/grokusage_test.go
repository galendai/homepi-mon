package grokusage

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/galendai/homepi-mon/internal/config"
	"github.com/galendai/homepi-mon/internal/connector"
	"github.com/galendai/homepi-mon/internal/connector/providerutil"
	"github.com/galendai/homepi-mon/internal/protocol"
	"github.com/galendai/homepi-mon/internal/ui"
)

func TestCollectActivelyFetchesCreditsWithoutMutatingAuth(t *testing.T) {
	dir := t.TempDir()
	authPath := writeAuth(t, dir, map[string]map[string]any{
		"old": {
			"key":        "old-key-fixture",
			"user_id":    "old-user-fixture",
			"auth_mode":  "oidc",
			"expires_at": "2026-08-25T00:00:00Z",
		},
		"new": {
			"key":           "new-key-fixture",
			"user_id":       "new-user-fixture",
			"auth_mode":     "oidc",
			"expires_at":    "2026-09-01T00:00:00Z",
			"refresh_token": "refresh-token-must-not-be-sent",
			"email":         "fixture@example.invalid",
		},
		"api": {
			"key":        "api-key-must-not-be-selected",
			"user_id":    "api-user-fixture",
			"auth_mode":  "api_key",
			"expires_at": "2026-12-01T00:00:00Z",
		},
	})
	versionPath := filepath.Join(dir, "version.json")
	if err := os.WriteFile(versionPath, []byte(`{"version":"1.0.5"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	beforeRaw, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatal(err)
	}
	beforeInfo, err := os.Lstat(authPath)
	if err != nil {
		t.Fatal(err)
	}

	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/v1/billing" || r.URL.Query().Get("format") != "credits" {
			t.Errorf("billing request path/query = %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		if r.Header.Get("Authorization") != "Bearer new-key-fixture" ||
			r.Header.Get("X-XAI-Token-Auth") != tokenAuthHeader ||
			r.Header.Get("x-userid") != "new-user-fixture" ||
			r.Header.Get("x-grok-client-version") != "1.0.5" ||
			r.Header.Get("x-grok-client-mode") != clientModeHeaderValue {
			t.Error("billing request headers did not match the official CLI contract")
		}
		if r.Header.Get("x-email") != "" || r.Header.Get("x-refresh-token") != "" {
			t.Error("billing request included a non-whitelisted identity header")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"config":{"creditUsagePercent":58.0,"currentPeriod":{"type":"USAGE_PERIOD_TYPE_WEEKLY","start":"2026-08-21T03:08:36Z","end":"2026-08-28T03:08:36Z"}}}`))
	}))
	defer server.Close()

	now := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)
	c := New(config.ProviderConfig{
		ID: "grok-main", Type: typeID, AccountLabel: "main", Region: "custom",
		BaseURL: server.URL + "/v1", AuthFile: authPath,
	}, providerutil.Runtime{HTTPClient: server.Client(), Now: func() time.Time { return now }})
	metrics, err := c.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 {
		t.Fatalf("billing requests = %d, want 1", requests.Load())
	}
	afterRaw, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatal(err)
	}
	afterInfo, err := os.Lstat(authPath)
	if err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(beforeRaw) != sha256.Sum256(afterRaw) ||
		beforeInfo.Mode() != afterInfo.Mode() || beforeInfo.Size() != afterInfo.Size() ||
		!beforeInfo.ModTime().Equal(afterInfo.ModTime()) || !os.SameFile(beforeInfo, afterInfo) {
		t.Fatal("Grok auth.json changed during collection")
	}
	if len(metrics) != 1 {
		t.Fatalf("metrics = %+v, want one weekly metric", metrics)
	}
	metric := metrics[0]
	if metric.ID != "grok-main.weekly" || metric.Provider != "grok" || metric.DisplayName != "Grok" ||
		metric.Value.String() != "42.0" || metric.Limit.String() != "100" || metric.Unit != "percent" ||
		metric.Window != protocol.WindowWeekly || metric.ResetsAt == nil ||
		metric.ResetsAt.Format(time.RFC3339) != "2026-08-28T03:08:36Z" ||
		!metric.ObservedAt.Equal(now) || metric.Precision != protocol.PrecisionVerified ||
		metric.SourceKind != protocol.SourceCompatAPI || metric.Group != "coding" || metric.Order != 23 {
		t.Fatalf("metric = %+v", metric)
	}
}

func TestCollectRejectsInvalidAuthAndBillingResponses(t *testing.T) {
	dir := t.TempDir()
	authPath := writeAuth(t, dir, map[string]map[string]any{
		"main": {
			"key":        "fixture-key",
			"user_id":    "fixture-user",
			"auth_mode":  "oidc",
			"expires_at": "2026-09-01T00:00:00Z",
		},
	})
	now := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name  string
		body  string
		class protocol.ErrorClass
	}{
		{name: "missing config", body: `{}`, class: protocol.ErrSchemaChanged},
		{name: "non-unified missing percent", body: `{"config":{"currentPeriod":{"type":"weekly","start":"2026-08-21T03:08:36Z","end":"2026-08-28T03:08:36Z"}}}`, class: protocol.ErrSchemaChanged},
		{name: "monthly period", body: `{"config":{"creditUsagePercent":58,"currentPeriod":{"type":"monthly","start":"2026-08-21T03:08:36Z","end":"2026-09-21T03:08:36Z"}}}`, class: protocol.ErrSchemaChanged},
		{name: "invalid percent", body: `{"config":{"creditUsagePercent":101,"currentPeriod":{"type":"weekly","start":"2026-08-21T03:08:36Z","end":"2026-08-28T03:08:36Z"}}}`, class: protocol.ErrSchemaChanged},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewTLSServer(jsonResponse(tc.body))
			defer server.Close()
			c := New(config.ProviderConfig{ID: "g", Type: typeID, AccountLabel: "a", Region: "custom", BaseURL: server.URL, AuthFile: authPath}, providerutil.Runtime{HTTPClient: server.Client(), Now: func() time.Time { return now }})
			_, err := c.Collect(context.Background())
			if connector.Classify(err) != tc.class {
				t.Fatalf("class = %s, err=%v", connector.Classify(err), err)
			}
		})
	}

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	c := New(config.ProviderConfig{ID: "g", Type: typeID, AccountLabel: "a", Region: "custom", BaseURL: server.URL, AuthFile: authPath}, providerutil.Runtime{HTTPClient: server.Client(), Now: func() time.Time { return now }})
	_, err := c.Collect(context.Background())
	if connector.Classify(err) != protocol.ErrAuth {
		t.Fatalf("HTTP 401 class = %s, err=%v", connector.Classify(err), err)
	}

	expiredPath := writeAuth(t, dir, map[string]map[string]any{
		"expired": {"key": "expired", "user_id": "expired-user", "auth_mode": "oidc", "expires_at": "2026-08-23T00:00:00Z"},
	})
	var calls atomic.Int32
	noRequestServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer noRequestServer.Close()
	c = New(config.ProviderConfig{ID: "g", Type: typeID, AccountLabel: "a", Region: "custom", BaseURL: noRequestServer.URL, AuthFile: expiredPath}, providerutil.Runtime{HTTPClient: noRequestServer.Client(), Now: func() time.Time { return now }})
	_, err = c.Collect(context.Background())
	if connector.Classify(err) != protocol.ErrAuth || calls.Load() != 0 {
		t.Fatalf("expired auth class=%s calls=%d err=%v", connector.Classify(err), calls.Load(), err)
	}
}

// U052: Only an omitted percent in a current Unified Billing weekly period
// follows the official CLI's zero-usage fallback; null remains unavailable.
func TestCollectUnifiedBillingPercentSemantics(t *testing.T) {
	dir := t.TempDir()
	authPath := writeAuth(t, dir, map[string]map[string]any{
		"main": {"key": "fixture-key", "user_id": "fixture-user", "auth_mode": "oidc", "expires_at": "2026-09-12T00:00:00Z"},
	})
	before, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 6, 10, 22, 0, 0, time.UTC)
	cases := []struct {
		name        string
		percent     string
		at          time.Time
		left        string
		unavailable bool
		schemaError bool
	}{
		{name: "omitted current", left: "100"},
		{name: "explicit zero", percent: "0", left: "100"},
		{name: "string zero", percent: `"0"`, left: "100"},
		{name: "nonzero", percent: "58.5", left: "41.5"},
		{name: "exhausted", percent: "100", left: "0"},
		{name: "null", percent: "null", unavailable: true},
		{name: "negative", percent: "-1", schemaError: true},
		{name: "above limit", percent: "101", schemaError: true},
		{name: "invalid type", percent: "false", schemaError: true},
		{name: "empty string", percent: `""`, schemaError: true},
		{name: "period start inclusive", at: time.Date(2026, 9, 4, 3, 8, 36, 180930000, time.UTC), left: "100"},
		{name: "future period", at: time.Date(2026, 9, 4, 3, 8, 36, 180929999, time.UTC), schemaError: true},
		{name: "period end exclusive", at: time.Date(2026, 9, 11, 3, 8, 36, 180930000, time.UTC), schemaError: true},
		{name: "expired period", at: time.Date(2026, 9, 11, 4, 0, 0, 0, time.UTC), schemaError: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			at := tc.at
			if at.IsZero() {
				at = now
			}
			percentField := ""
			if tc.percent != "" {
				percentField = `"creditUsagePercent":` + tc.percent + ","
			}
			// Extra paid credits must not influence the subscription percentage.
			body := fmt.Sprintf(`{"config":{%s"isUnifiedBillingUser":true,"currentPeriod":{"type":"USAGE_PERIOD_TYPE_WEEKLY","start":"2026-09-04T03:08:36.180930Z","end":"2026-09-11T03:08:36.180930Z"},"onDemandCap":{"val":5000},"onDemandUsed":{"val":2500},"prepaidBalance":{"val":1000}}}`, percentField)
			server := httptest.NewTLSServer(jsonResponse(body))
			defer server.Close()
			c := New(config.ProviderConfig{ID: "grok-main", Type: typeID, AccountLabel: "main", Region: "custom", BaseURL: server.URL, AuthFile: authPath}, providerutil.Runtime{HTTPClient: server.Client(), Now: func() time.Time { return at }})
			metrics, err := c.Collect(context.Background())
			if tc.schemaError {
				if err == nil || connector.Classify(err) != protocol.ErrSchemaChanged || len(metrics) != 0 {
					t.Fatalf("metrics=%+v err=%v, want schema_changed and no metrics", metrics, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(metrics) != 1 {
				t.Fatalf("metrics=%+v, want one weekly metric", metrics)
			}
			m := metrics[0]
			if m.ID != "grok-main.weekly" || m.Window != protocol.WindowWeekly || m.ResetsAt == nil || m.ResetsAt.Format(time.RFC3339Nano) != "2026-09-11T03:08:36.18093Z" || !m.ObservedAt.Equal(at) || m.SourceKind != protocol.SourceCompatAPI {
				t.Fatalf("metric=%+v", m)
			}
			if tc.unavailable {
				if m.Value != nil || m.Limit != nil || m.Precision != protocol.PrecisionUnavailable || m.Status != protocol.StatusUnknown {
					t.Fatalf("null metric=%+v", m)
				}
			} else if m.Value == nil || m.Limit == nil || m.Value.String() != tc.left || m.Limit.String() != "100" || m.Precision != protocol.PrecisionVerified || m.Status != protocol.StatusOK {
				t.Fatalf("metric=%+v, want %s left", m, tc.left)
			}
			frame := ui.RenderString(ui.Build(&protocol.MetricSnapshot{GeneratedAt: at, Metrics: metrics}, ui.BuildOptions{Now: at, Connected: true}))
			if !strings.Contains(frame, "Grok") || !strings.Contains(frame, "5H --") {
				t.Fatalf("missing Grok card/placeholder:\n%s", frame)
			}
			if tc.unavailable && !strings.Contains(frame, "N/A") {
				t.Fatalf("null should render N/A:\n%s", frame)
			}
			if tc.left == "100" && !strings.Contains(frame, "100% LEFT") {
				t.Fatalf("zero usage should render 100%% LEFT:\n%s", frame)
			}
		})
	}
	after, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(before) != sha256.Sum256(after) {
		t.Fatal("Grok auth.json changed during collection")
	}
}

func TestCollectRejectsUnsafeOrInvalidGrokAuthFile(t *testing.T) {
	dir := t.TempDir()
	valid := writeAuth(t, dir, map[string]map[string]any{
		"main": {"key": "fixture-key", "user_id": "fixture-user", "auth_mode": "oidc", "expires_at": "2026-09-01T00:00:00Z"},
	})
	now := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)
	base := func(path string) *Connector {
		return New(config.ProviderConfig{ID: "g", Type: typeID, AccountLabel: "a", Region: "global", AuthFile: path}, providerutil.Runtime{Now: func() time.Time { return now }})
	}
	if _, err := base("relative/auth.json").Collect(context.Background()); connector.Classify(err) != protocol.ErrInvalidConfig {
		t.Fatalf("relative auth class = %s, err=%v", connector.Classify(err), err)
	}
	missing := filepath.Join(dir, "missing.json")
	if _, err := base(missing).Collect(context.Background()); connector.Classify(err) != protocol.ErrAuth {
		t.Fatalf("missing auth class = %s, err=%v", connector.Classify(err), err)
	}
	linkPath := filepath.Join(dir, "link.json")
	if err := os.Symlink(valid, linkPath); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := base(linkPath).Collect(context.Background()); connector.Classify(err) != protocol.ErrInvalidConfig {
		t.Fatalf("symlink auth class = %s, err=%v", connector.Classify(err), err)
	}
	large := filepath.Join(dir, "large.json")
	if err := os.WriteFile(large, make([]byte, maxAuthBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := base(large).Collect(context.Background()); connector.Classify(err) != protocol.ErrAuth {
		t.Fatalf("oversized auth class = %s, err=%v", connector.Classify(err), err)
	}
}

func TestValidateConfigSupportsOnlyGlobalOrCustomGrokEndpoint(t *testing.T) {
	valid := config.ProviderConfig{ID: "g", Type: typeID, AccountLabel: "a", Region: "global"}
	if err := New(valid, providerutil.Runtime{}).ValidateConfig(); err != nil {
		t.Fatalf("global config rejected: %v", err)
	}
	for name, spec := range map[string]config.ProviderConfig{
		"secret":         {ID: "g", Type: typeID, AccountLabel: "a", Region: "global", SecretRef: "keyring:g"},
		"cn":             {ID: "g", Type: typeID, AccountLabel: "a", Region: "cn"},
		"global base":    {ID: "g", Type: typeID, AccountLabel: "a", Region: "global", BaseURL: "https://example.test"},
		"custom missing": {ID: "g", Type: typeID, AccountLabel: "a", Region: "custom"},
	} {
		t.Run(name, func(t *testing.T) {
			if connector.Classify(New(spec, providerutil.Runtime{}).ValidateConfig()) != protocol.ErrInvalidConfig {
				t.Fatalf("ValidateConfig accepted invalid spec: %+v", spec)
			}
		})
	}
	custom := config.ProviderConfig{ID: "g", Type: typeID, AccountLabel: "a", Region: "custom", BaseURL: "https://example.test/v1"}
	if err := New(custom, providerutil.Runtime{}).ValidateConfig(); err != nil {
		t.Fatalf("custom config rejected: %v", err)
	}
}

func writeAuth(t *testing.T, dir string, entries map[string]map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func jsonResponse(body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(strings.TrimSpace(body)))
	})
}
