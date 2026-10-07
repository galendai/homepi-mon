package kimicoding

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
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

// Synthetic quota data follows the official CLI's usage/limits/detail contract.
const mainlandQuota = `{
  "usage":{"name":"Coding Plan","limit":"100","remaining":"75","resetTime":"2026-10-14T06:00:00Z"},
  "limits":[{"window":{"duration":300,"timeUnit":"TIME_UNIT_MINUTE"},
    "detail":{"limit":"100","remaining":"68","resetTime":"2026-10-07T10:00:00Z"}}]
}`

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCollectRegionalQuotaReachesCodingCard(t *testing.T) {
	for _, region := range []struct{ name, host string }{
		{"cn", "api.kimi.com"}, {"global", "api.kimi.com"},
	} {
		t.Run(region.name, func(t *testing.T) {
			now := time.Date(2026, 10, 7, 6, 0, 0, 0, time.UTC)
			requests := 0
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				requests++
				if r.Method != http.MethodGet || r.URL.String() != "https://"+region.host+"/coding/v1/usages" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
				}
				if r.Header.Get("Authorization") != "Bearer synthetic-coding-key" {
					t.Error("missing Coding Plan bearer credential")
				}
				if r.Header.Get("User-Agent") != "homepi-node/phase1" {
					t.Errorf("client identity = %q", r.Header.Get("User-Agent"))
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}},
					Body: io.NopCloser(strings.NewReader(mainlandQuota))}, nil
			})}
			c := New(config.ProviderConfig{ID: "kimi-cn", Type: typeID, AccountLabel: "main",
				Region: region.name, SecretRef: "keyring:kimi-cn"},
				codingStore(t, "keyring:kimi-cn", "synthetic-coding-key"),
				providerutil.Runtime{HTTPClient: client, Now: func() time.Time { return now }})
			metrics, err := c.Collect(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if requests != 1 || len(metrics) != 2 {
				t.Fatalf("requests=%d, metrics=%+v", requests, metrics)
			}
			for i, want := range []struct {
				suffix, value, reset string
				window               protocol.Window
			}{
				{"5h", "68", "2026-10-07T10:00:00Z", protocol.WindowRolling5h},
				{"weekly", "75", "2026-10-14T06:00:00Z", protocol.WindowWeekly},
			} {
				m := metrics[i]
				if err := m.Validate(); err != nil {
					t.Fatal(err)
				}
				if m.ID != "kimi-cn."+want.suffix || m.Value.String() != want.value || m.Limit.String() != "100" ||
					m.Window != want.window || m.ResetsAt == nil || m.ResetsAt.Format(time.RFC3339) != want.reset ||
					m.ObservedAt != now || m.Precision != protocol.PrecisionVerified || m.SourceKind != protocol.SourceCompatAPI {
					t.Fatalf("metric = %+v", m)
				}
			}
			vm := ui.Build(&protocol.MetricSnapshot{SourceNode: "dev-mac", GeneratedAt: now, Metrics: metrics},
				ui.BuildOptions{Page: "CODING", Now: now, Connected: true, RotationEnabled: true})
			if len(vm.Coding) != 1 || len(vm.API) != 0 || vm.Coding[0].PercentLeft == nil ||
				*vm.Coding[0].PercentLeft != 68 || vm.Coding[0].WeekPercentLeft == nil || *vm.Coding[0].WeekPercentLeft != 75 {
				t.Fatalf("coding cards = %+v", vm.Coding)
			}
			frame := ui.RenderString(vm)
			for _, want := range []string{"Kimi Coding", "68% LEFT", "WEEK 75%", "RESET 4H"} {
				if !strings.Contains(frame, want) {
					t.Errorf("missing %q:\n%s", want, frame)
				}
			}
			lines := ui.Render(vm)
			if len(lines) != 20 {
				t.Fatalf("frame height = %d", len(lines))
			}
			for _, line := range lines {
				if len(line) != 60 {
					t.Fatalf("frame width = %d", len(line))
				}
			}
		})
	}
}

func TestCodingMetadataDefaultsToMainland(t *testing.T) {
	m, ok := providermeta.Lookup(typeID)
	if !ok || m.DefaultRegion != "cn" || m.DefaultBaseURL != "https://api.kimi.com" || !m.HasRegion("cn") {
		t.Fatalf("metadata = %+v", m)
	}
	if !reflect.DeepEqual(m.MetricIDSuffixes, []string{"5h", "weekly", "monthly-total", "monthly-code"}) {
		t.Fatalf("metric suffixes = %v", m.MetricIDSuffixes)
	}
}

func TestCollectCurrentMonthlyQuotaReachesCodingCard(t *testing.T) {
	now := time.Date(2026, 10, 7, 6, 0, 0, 0, time.UTC)
	// Synthetic fixture for the current official ratio contract.
	body := `{"usages":{
		"limit_5h":{"used_ratio":0,"reset_time":"2026-10-07T10:00:00Z"},
		"limit_month_total":{"used_ratio":0.9047,"reset_time":"2026-10-18T02:25:29Z"},
		"limit_month_code":{"used_ratio":"0.9029","reset_time":"2026-10-18T02:25:29Z"}
	},"limits":[{"window":{"duration":300,"timeUnit":"TIME_UNIT_MINUTE"},
		"detail":{"limit":100,"remaining":25}}]}`
	metrics, err := collectBody(t, body, now)
	if err != nil || len(metrics) != 3 {
		t.Fatalf("metrics=%+v, err=%v", metrics, err)
	}
	for i, want := range []struct {
		suffix, value, reset string
		window               protocol.Window
	}{
		{"5h", "1", "2026-10-07T10:00:00Z", protocol.WindowRolling5h},
		{"monthly-total", "0.0953", "2026-10-18T02:25:29Z", protocol.WindowMonthly},
		{"monthly-code", "0.0971", "2026-10-18T02:25:29Z", protocol.WindowMonthly},
	} {
		m := metrics[i]
		if err := m.Validate(); err != nil {
			t.Fatal(err)
		}
		if m.ID != "kimi-cn."+want.suffix || m.Value.String() != want.value ||
			m.Limit.String() != "1" || m.Unit != "ratio" || m.Window != want.window ||
			m.ResetsAt == nil || m.ResetsAt.Format(time.RFC3339) != want.reset ||
			m.Precision != protocol.PrecisionVerified || m.SourceKind != protocol.SourceCompatAPI {
			t.Fatalf("metric = %+v", m)
		}
	}
	vm := ui.Build(&protocol.MetricSnapshot{SourceNode: "dev-mac", GeneratedAt: now, Metrics: metrics},
		ui.BuildOptions{Now: now, Connected: true, RotationEnabled: true, Thresholds: protocol.DefaultThresholds()})
	card := vm.Coding[0]
	if *card.PercentLeft != 100 || card.WeekPercentLeft != nil || card.MonthTotalUsed != "90.5%" ||
		card.MonthCodeUsed != "90.3%" || card.Status != protocol.DisplayCrit || vm.AlertCount != 1 {
		t.Fatalf("card=%+v, alerts=%d", card, vm.AlertCount)
	}
	frame := ui.RenderString(vm)
	if !strings.Contains(frame, "MONTH USED: TOTAL 90.5% | CODE 90.3%") ||
		strings.Contains(frame, "WEEK") || !strings.Contains(frame, "100% LEFT") {
		t.Fatal(frame)
	}
}

func collectBody(t *testing.T, body string, now time.Time) ([]protocol.ProviderMetric, error) {
	t.Helper()
	c := New(config.ProviderConfig{ID: "kimi-cn", Region: "cn", SecretRef: "keyring:k"},
		codingStore(t, "keyring:k", "synthetic-coding-key"), providerutil.Runtime{
			Now: func() time.Time { return now },
			HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}},
					Body: io.NopCloser(strings.NewReader(body))}, nil
			})},
		})
	return c.Collect(context.Background())
}

func TestCurrentQuotaRatioValidation(t *testing.T) {
	for _, tc := range []struct{ name, value, remaining string }{
		{"unused", "0", "1"}, {"exhausted", "1", "0"},
		{"decimal string", `"0.125"`, "0.875"},
		{"negative", "-0.01", ""}, {"over limit", "1.001", ""},
		{"null", "null", ""}, {"invalid", `"unknown"`, ""},
		{"boolean", "true", ""}, {"object", "{}", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			metrics, err := collectBody(t, `{"usages":{"limit_5h":{"used_ratio":0},
				"limit_month_total":{"used_ratio":`+tc.value+`}}}`, time.Now())
			if tc.remaining == "" {
				if len(metrics) != 0 || connector.Classify(err) != protocol.ErrSchemaChanged {
					t.Fatalf("metrics=%+v, err=%v", metrics, err)
				}
			} else if err != nil || len(metrics) != 2 || metrics[1].Value.String() != tc.remaining {
				t.Fatalf("metrics=%+v, err=%v", metrics, err)
			}
		})
	}
}

func TestCurrentQuotaOptionalWindowsAndLegacyFallback(t *testing.T) {
	for _, tc := range []struct {
		name, body  string
		ids, values []string
	}{
		{"monthly only", `{"usages":{"limit_month_code":{"used_ratio":0.25}}}`,
			[]string{"monthly-code"}, []string{"0.75"}},
		{"weekly ratio preferred", `{"usages":{"limit_7d":{"used_ratio":0.2}},"usage":{"limit":100,"remaining":99}}`,
			[]string{"weekly"}, []string{"0.8"}},
		{"legacy 5h with monthly", `{"usages":{"limit_month_total":{"used_ratio":0.5}},
			"limits":[{"name":"5h","limit":100,"remaining":68}]}`,
			[]string{"5h", "monthly-total"}, []string{"68", "0.5"}},
		{"unknown only", `{"usages":{"future":{"used_ratio":0}}}`, nil, nil},
		{"missing ratio", `{"usages":{"limit_month_code":{}}}`, nil, nil},
		{"null entry", `{"usages":{"limit_month_code":null}}`, nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			metrics, err := collectBody(t, tc.body, time.Now())
			if tc.ids == nil {
				if connector.Classify(err) != protocol.ErrSchemaChanged || len(metrics) != 0 {
					t.Fatalf("metrics=%+v, err=%v", metrics, err)
				}
				return
			}
			if err != nil || len(metrics) != len(tc.ids) {
				t.Fatalf("metrics=%+v, err=%v", metrics, err)
			}
			for i, m := range metrics {
				if m.ID != "kimi-cn."+tc.ids[i] || m.Value.String() != tc.values[i] {
					t.Fatalf("metric=%+v", m)
				}
			}
		})
	}
}

func TestCollectQuotaValueSemantics(t *testing.T) {
	for _, tc := range []struct {
		name, body, value string
		derived           bool
	}{
		{"empty", `{}`, "", false},
		{"missing limit", `{"usage":{"remaining":50}}`, "", false},
		{"null remaining without used", `{"usage":{"limit":100,"remaining":null}}`, "", false},
		{"zero limit", `{"usage":{"limit":0,"remaining":0}}`, "", false},
		{"negative", `{"usage":{"limit":100,"remaining":-1}}`, "", false},
		{"over limit", `{"usage":{"limit":100,"remaining":101}}`, "", false},
		{"invalid number", `{"usage":{"limit":100,"remaining":"unknown"}}`, "", false},
		{"unknown window", `{"limits":[{"limit":100,"remaining":50,"window":{"duration":1,"timeUnit":"HOUR"}}]}`, "", false},
		{"exhausted", `{"usage":{"limit":"100","remaining":"0"}}`, "0", false},
		{"full", `{"usage":{"limit":100,"remaining":100}}`, "100", false},
		{"derive used", `{"usage":{"limit":100,"used":25}}`, "75", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := New(config.ProviderConfig{ID: "kimi-cn", Region: "cn", SecretRef: "keyring:k"},
				codingStore(t, "keyring:k", "synthetic-coding-key"), providerutil.Runtime{HTTPClient: &http.Client{
					Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
						return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}},
							Body: io.NopCloser(strings.NewReader(tc.body))}, nil
					}),
				}})
			metrics, err := c.Collect(context.Background())
			if tc.value == "" {
				if connector.Classify(err) != protocol.ErrSchemaChanged || len(metrics) != 0 {
					t.Fatalf("metrics=%+v, err=%v", metrics, err)
				}
				return
			}
			if err != nil || len(metrics) != 1 || metrics[0].Value.String() != tc.value ||
				(len(metrics[0].Derived) > 0) != tc.derived {
				t.Fatalf("metrics=%+v, err=%v", metrics, err)
			}
		})
	}
}

func TestCollectErrorsDoNotFallbackOrExposeSecrets(t *testing.T) {
	for _, tc := range []struct {
		status int
		class  protocol.ErrorClass
	}{
		{401, protocol.ErrAuth}, {403, protocol.ErrAuth}, {429, protocol.ErrRateLimited},
		{500, protocol.ErrUpstream}, {200, protocol.ErrSchemaChanged},
	} {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			requests := 0
			c := New(config.ProviderConfig{ID: "kimi-cn", Region: "cn", SecretRef: "keyring:k"},
				codingStore(t, "keyring:k", "synthetic-coding-key"), providerutil.Runtime{HTTPClient: &http.Client{
					Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
						requests++
						return &http.Response{StatusCode: tc.status,
							Header: http.Header{"Content-Type": {"application/json"}, "Retry-After": {"120"}},
							Body:   io.NopCloser(strings.NewReader(`{"error":"synthetic-coding-key"}`))}, nil
					}),
				}})
			_, err := c.Collect(context.Background())
			if connector.Classify(err) != tc.class || requests != 1 {
				t.Fatalf("requests=%d, err=%v", requests, err)
			}
			if strings.Contains(err.Error(), "synthetic-coding-key") {
				t.Fatal("upstream credential appeared in error")
			}
			if tc.status == 429 && connector.RetryAfter(err) != 120 {
				t.Fatalf("Retry-After=%d", connector.RetryAfter(err))
			}
		})
	}
}

func TestCollectFallsBackOnlyOn404AndSortsWindows(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path == "/coding/v1/usages" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"usage":{"used":25,"limit":100},"limits":[{"used":8,"limit":40,"window":{"duration":5,"timeUnit":"HOUR"},"reset_in":3600}]}`))
	}))
	defer srv.Close()
	store := codingStore(t, "keyring:kimi-coding", "sk-kimi-test")
	c := New(config.ProviderConfig{
		ID: "kimi-coding-main", Type: typeID, AccountLabel: "main", Region: "custom",
		BaseURL: srv.URL, SecretRef: "keyring:kimi-coding",
	}, store, providerutil.Runtime{Now: func() time.Time { return time.Unix(1_800_000_000, 0) }})
	metrics, err := c.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	wantPaths := []string{"/coding/v1/usages", "/coding/v1/usage"}
	if !reflect.DeepEqual(paths, wantPaths) {
		t.Fatalf("paths = %v, want %v", paths, wantPaths)
	}
	if len(metrics) != 2 || metrics[0].ID != "kimi-coding-main.5h" ||
		metrics[0].Value.String() != "32" || metrics[1].Value.String() != "75" {
		t.Fatalf("metrics = %+v", metrics)
	}
}

func TestCollectDoesNotFallbackOnUpstreamFailure(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := New(config.ProviderConfig{ID: "k", Type: typeID, AccountLabel: "a", Region: "custom", BaseURL: srv.URL, SecretRef: "keyring:k"},
		codingStore(t, "keyring:k", "secret"), providerutil.Runtime{})
	_, err := c.Collect(context.Background())
	if connector.Classify(err) != protocol.ErrUpstream || requests != 1 {
		t.Fatalf("class/requests = %s/%d, err=%v", connector.Classify(err), requests, err)
	}
}

func codingStore(t *testing.T, ref, value string) secretstore.Store {
	t.Helper()
	store, err := secretstore.NewFileBackend(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set(context.Background(), ref, value); err != nil {
		t.Fatal(err)
	}
	return store
}
