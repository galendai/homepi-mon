package opencodego

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/galendai/homepi-mon/internal/config"
	"github.com/galendai/homepi-mon/internal/connector"
	"github.com/galendai/homepi-mon/internal/connector/providerutil"
	"github.com/galendai/homepi-mon/internal/protocol"
	"github.com/galendai/homepi-mon/internal/providermeta"
	"github.com/galendai/homepi-mon/internal/secretstore"
	"github.com/galendai/homepi-mon/internal/ui"
)

// Synthetic data follows the official /zen/go/v1/usage contract.
const currentUsage = `{"usage":{
  "rolling":{"status":"ok","percent":20,"resetsAt":"2026-10-07T10:00:00Z"},
  "weekly":{"status":"ok","percent":35,"resetsAt":"2026-10-12T00:00:00Z"},
  "monthly":{"status":"ok","percent":50,"resetsAt":"2026-11-01T00:00:00Z"}
}}`

const legacyUsage = `{
  "rollingUsage":{"status":"ok","usagePercent":20,"resetInSec":14400},
  "weeklyUsage":{"status":"ok","usagePercent":35,"resetInSec":410400},
  "monthlyUsage":{"status":"ok","usagePercent":50,"resetInSec":2138400}
}`

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCollectOfficialContractsReachCodingCard(t *testing.T) {
	now := time.Date(2026, 10, 7, 6, 0, 0, 0, time.UTC)
	for _, body := range []string{currentUsage, legacyUsage} {
		t.Run(body[:10], func(t *testing.T) {
			store, err := secretstore.NewFileBackend(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Set(context.Background(), "keyring:go", "synthetic-go-key"); err != nil {
				t.Fatal(err)
			}
			requests := 0
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				requests++
				if r.Method != http.MethodGet || r.URL.String() != "https://opencode.ai/zen/go/v1/usage" ||
					r.Header.Get("Authorization") != "Bearer synthetic-go-key" || r.Header.Get("User-Agent") != "homepi-node/phase1" {
					t.Error("unexpected usage request or client credentials")
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}},
					Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			c := New(config.ProviderConfig{ID: "go-main", Type: typeID, AccountLabel: "main",
				Region: "global", SecretRef: "keyring:go"}, store,
				providerutil.Runtime{HTTPClient: client, Now: func() time.Time { return now }})
			metrics, err := c.Collect(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if requests != 1 || len(metrics) != 3 {
				t.Fatalf("requests=%d metrics=%+v", requests, metrics)
			}
			for i, want := range []struct {
				suffix, value, reset string
				window               protocol.Window
			}{
				{"5h", "80", "2026-10-07T10:00:00Z", protocol.WindowRolling5h},
				{"weekly", "65", "2026-10-12T00:00:00Z", protocol.WindowWeekly},
				{"monthly", "50", "2026-11-01T00:00:00Z", protocol.WindowMonthly},
			} {
				m := metrics[i]
				if err := m.Validate(); err != nil {
					t.Fatal(err)
				}
				if m.ID != "go-main."+want.suffix || m.Value.String() != want.value || m.Limit.String() != "100" ||
					m.Window != want.window || m.ResetsAt == nil || m.ResetsAt.Format(time.RFC3339) != want.reset ||
					m.ObservedAt != now || m.Precision != protocol.PrecisionVerified || m.SourceKind != protocol.SourceCompatAPI {
					t.Fatalf("metric=%+v", m)
				}
			}
			// Input order must not allow the monthly metric to replace the 5h bar.
			metrics[0], metrics[2] = metrics[2], metrics[0]
			vm := ui.Build(&protocol.MetricSnapshot{SourceNode: "node", GeneratedAt: now, Metrics: metrics},
				ui.BuildOptions{Now: now, Connected: true, RotationEnabled: true, Thresholds: protocol.DefaultThresholds()})
			if len(vm.Coding) != 1 || len(vm.API) != 0 || *vm.Coding[0].PercentLeft != 80 ||
				*vm.Coding[0].WeekPercentLeft != 65 || *vm.Coding[0].MonthPercentLeft != 50 {
				t.Fatalf("vm=%+v", vm)
			}
			for _, style := range []ui.Style{ui.StyleASCII, ui.StyleRich} {
				frame := ui.RenderStyledString(vm, style)
				for _, want := range []string{"OpenCode Go", "80% LEFT", "5H 80% | WEEK 65% | MONTH 50% LEFT", "RESET 4H"} {
					if !strings.Contains(frame, want) {
						t.Fatalf("missing %s:\n%s", want, frame)
					}
				}
			}
		})
	}
}

func decode(t *testing.T, body string) *response {
	t.Helper()
	var payload response
	d := json.NewDecoder(strings.NewReader(body))
	d.UseNumber()
	if err := d.Decode(&payload); err != nil {
		t.Fatal(err)
	}
	return &payload
}

func TestPercentAndResetValidation(t *testing.T) {
	now := time.Date(2026, 10, 7, 6, 0, 0, 0, time.UTC)
	c := New(config.ProviderConfig{ID: "go"}, nil, providerutil.Runtime{Now: func() time.Time { return now }})
	for _, tc := range []struct{ name, value, remaining string }{
		{"unused", "0", "100"}, {"exhausted", "100", "0"}, {"decimal string", `"12.5"`, "87.5"},
		{"negative", "-1", ""}, {"over limit", "100.1", ""}, {"null", "null", ""},
		{"boolean", "true", ""}, {"malformed", `"private-value"`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := decode(t, strings.Replace(currentUsage, `"percent":20`, `"percent":`+tc.value, 1))
			metrics, err := c.parse(payload)
			if tc.remaining == "" {
				if connector.Classify(err) != protocol.ErrSchemaChanged || metrics != nil {
					t.Fatalf("metrics=%v err=%v", metrics, err)
				}
				if strings.Contains(err.Error(), "private-value") {
					t.Fatal("raw value leaked")
				}
			} else if err != nil || metrics[0].Value.String() != tc.remaining {
				t.Fatalf("metrics=%v err=%v", metrics, err)
			}
		})
	}
	for _, body := range []string{
		`{}`, `{"usage":{}}`, `{"usage":{"rolling":null}}`,
		strings.Replace(currentUsage, `"percent":20,`, "", 1),
		strings.Replace(currentUsage, "2026-10-07T10:00:00Z", "invalid-date", 1),
		strings.Replace(currentUsage, `"status":"ok"`, `"status":"unknown"`, 1),
		strings.Replace(currentUsage, `"status":"ok"`, `"status":"rate-limited"`, 1),
		strings.Replace(legacyUsage, `"resetInSec":14400`, `"resetInSec":null`, 1),
		strings.Replace(legacyUsage, `"resetInSec":14400`, `"resetInSec":-1`, 1),
		// An invalid current object cannot fall back to older fields.
		strings.Replace(legacyUsage, "{", `{"usage":{},`, 1),
	} {
		if metrics, err := c.parse(decode(t, body)); metrics != nil || connector.Classify(err) != protocol.ErrSchemaChanged {
			t.Fatalf("accepted invalid windows: metrics=%v err=%v", metrics, err)
		}
	}
	metrics, err := c.parse(decode(t, strings.Replace(legacyUsage, `"resetInSec":14400`, `"resetInSec":0`, 1)))
	if err != nil || !metrics[0].ResetsAt.Equal(now) {
		t.Fatalf("zero reset: metrics=%v err=%v", metrics, err)
	}
	metrics, err = c.parse(decode(t, strings.Replace(strings.Replace(currentUsage, `"percent":20`, `"percent":100`, 1), `"status":"ok"`, `"status":"rate-limited"`, 1)))
	if err != nil || metrics[0].Value.String() != "0" {
		t.Fatalf("exhausted: metrics=%v err=%v", metrics, err)
	}
}

func TestRegistrationAndConfiguration(t *testing.T) {
	m, ok := providermeta.Lookup(typeID)
	if !ok || !connector.HasType(typeID) || !m.RequiresSecret || m.RequiresAuthFile || m.HasRegion("cn") ||
		m.DefaultRegion != "global" || m.DefaultBaseURL != "https://opencode.ai" {
		t.Fatalf("meta=%+v", m)
	}
	ids, err := m.StableMetricIDs(config.ProviderConfig{ID: "go"})
	if err != nil || strings.Join(ids, ",") != "go.5h,go.weekly,go.monthly" {
		t.Fatalf("ids=%v err=%v", ids, err)
	}
	for _, spec := range []config.ProviderConfig{
		{Region: "global"}, {Region: "cn", SecretRef: "keyring:go"},
		{Region: "custom", SecretRef: "keyring:go"}, {Region: "global", SecretRef: "keyring:go", AuthFile: "/tmp/auth.json"},
	} {
		if err := New(spec, nil, providerutil.Runtime{}).ValidateConfig(); connector.Classify(err) != protocol.ErrInvalidConfig {
			t.Fatalf("config=%+v err=%v", spec, err)
		}
	}
}

func TestHTTPFailuresAreClassifiedWithoutLeakingSecrets(t *testing.T) {
	store, err := secretstore.NewFileBackend(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set(context.Background(), "keyring:go", "synthetic-go-key"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		code  int
		class protocol.ErrorClass
	}{
		{401, protocol.ErrAuth}, {403, protocol.ErrAuth}, {429, protocol.ErrRateLimited}, {500, protocol.ErrUpstream},
	} {
		calls := 0
		c := New(config.ProviderConfig{ID: "go", Region: "custom", BaseURL: "https://usage.example", SecretRef: "keyring:go"}, store,
			providerutil.Runtime{HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.String() != "https://usage.example/zen/go/v1/usage" {
					t.Error("custom URL ignored")
				}
				return &http.Response{StatusCode: tc.code, Header: http.Header{"Retry-After": {"30"}},
					Body: io.NopCloser(strings.NewReader("synthetic-go-key private-body"))}, nil
			})}})
		metrics, err := c.Collect(context.Background())
		if calls != 1 || metrics != nil || connector.Classify(err) != tc.class {
			t.Fatalf("calls=%d metrics=%v err=%v", calls, metrics, err)
		}
		if strings.Contains(err.Error(), "synthetic-go-key") || strings.Contains(err.Error(), "private-body") {
			t.Fatal("secret leaked")
		}
		if tc.code == 429 && connector.RetryAfter(err) != 30 {
			t.Fatal("retry budget lost")
		}
	}
}
