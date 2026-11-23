package codexusage

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/galendai/homepi-mon/internal/config"
	"github.com/galendai/homepi-mon/internal/connector"
	"github.com/galendai/homepi-mon/internal/connector/providerutil"
	"github.com/galendai/homepi-mon/internal/protocol"
	"github.com/galendai/homepi-mon/internal/ui"
)

func TestCollectReadsAuthJSONWithoutMutationOrRefresh(t *testing.T) {
	const (
		access  = "access-token-test-value"
		refresh = "refresh-token-must-not-be-used"
		account = "account-test-id"
	)
	dir := t.TempDir()
	authPath := filepath.Join(dir, "auth.json")
	body := `{"tokens":{"access_token":"` + access + `","refresh_token":"` + refresh + `","account_id":"` + account + `"},"last_refresh":"old"}`
	if err := os.WriteFile(authPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(authPath)
	beforeHash := sha256.Sum256([]byte(body))

	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/backend-api/wham/usage" || r.Header.Get("Authorization") != "Bearer "+access ||
			r.Header.Get("ChatGPT-Account-Id") != account {
			t.Error("unexpected Codex usage request path or authentication headers")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"plan_type":"plus","rate_limit":{"primary_window":{"used_percent":32,"limit_window_seconds":18000,"reset_at":1800003600},"secondary_window":{"used_percent":10,"limit_window_seconds":604800,"reset_at":1800604800}}}`))
	}))
	defer srv.Close()
	c := New(config.ProviderConfig{
		ID: "codex-main", Type: typeID, AccountLabel: "main", Region: "custom",
		BaseURL: srv.URL, AuthFile: authPath,
	}, providerutil.Runtime{Now: func() time.Time { return time.Unix(1_800_000_000, 0) }})
	metrics, err := c.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	afterBody, _ := os.ReadFile(authPath)
	after, _ := os.Stat(authPath)
	afterHash := sha256.Sum256(afterBody)
	if requests != 1 || beforeHash != afterHash || before.Mode() != after.Mode() ||
		before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("auth file changed or unexpected requests: requests=%d", requests)
	}
	if len(metrics) != 2 || metrics[0].Value.String() != "68" || metrics[1].Value.String() != "90" {
		t.Fatalf("metrics = %+v", metrics)
	}
}

func TestParseClassifiesQuotaWindowsByDuration(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	for _, tc := range []struct {
		name, limits string
		ids          []string
		values       []string
	}{
		{"weekly primary only", `{"primary_window":{"used_percent":46,"limit_window_seconds":604800,"reset_at":1800604800}}`, []string{"c.weekly"}, []string{"54"}},
		{"swapped windows", `{"primary_window":{"used_percent":46,"limit_window_seconds":"604800","reset_at":1800604800},"secondary_window":{"used_percent":12,"limit_window_seconds":"18000","reset_at":1800003600}}`, []string{"c.5h", "c.weekly"}, []string{"88", "54"}},
		{"weekly secondary only", `{"secondary_window":{"used_percent":46,"limit_window_seconds":604800,"reset_at":1800604800}}`, []string{"c.weekly"}, []string{"54"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var payload response
			dec := json.NewDecoder(strings.NewReader(`{"rate_limit":` + tc.limits + `}`))
			dec.UseNumber()
			if err := dec.Decode(&payload); err != nil {
				t.Fatal(err)
			}
			c := New(config.ProviderConfig{ID: "c"}, providerutil.Runtime{Now: func() time.Time { return now }})
			metrics, err := c.parse(&payload)
			if err != nil || len(metrics) != len(tc.ids) {
				t.Fatalf("metrics=%+v err=%v", metrics, err)
			}
			for i, m := range metrics {
				wantWindow, wantReset := protocol.WindowWeekly, int64(1_800_604_800)
				if tc.ids[i] == "c.5h" {
					wantWindow, wantReset = protocol.WindowRolling5h, 1_800_003_600
				}
				if m.ID != tc.ids[i] || m.Window != wantWindow || m.Value.String() != tc.values[i] ||
					m.ResetsAt == nil || m.ResetsAt.Unix() != wantReset {
					t.Fatalf("metric=%+v", m)
				}
			}
			vm := ui.Build(&protocol.MetricSnapshot{GeneratedAt: now, Metrics: metrics}, ui.BuildOptions{Now: now, Connected: true, Page: string(ui.PageCoding)})
			frame := ui.RenderKioskString(vm, ui.StyleASCII)
			want5h := "--"
			if len(tc.ids) == 2 {
				want5h = "88%"
			}
			for _, line := range strings.Split(frame, "\n") {
				if strings.HasPrefix(line, "| Codex ") {
					got := [3]string{strings.TrimSpace(line[19:24]), strings.TrimSpace(line[25:30]), strings.TrimSpace(line[31:36])}
					if got != [3]string{want5h, "54%", "--"} {
						t.Fatalf("wrong window columns %v:\n%s", got, frame)
					}
				}
			}
			if !strings.Contains(frame, "| Codex ") {
				t.Fatal("Codex card missing")
			}
		})
	}
}

func TestParseRejectsUnclassifiableQuotaWindows(t *testing.T) {
	for _, limits := range []string{
		`{}`, `{"primary_window":{"used_percent":10}}`,
		`{"primary_window":{"used_percent":10,"limit_window_seconds":null}}`,
		`{"primary_window":{"used_percent":10,"limit_window_seconds":"not-a-duration"}}`,
		`{"primary_window":{"used_percent":10,"limit_window_seconds":0}}`,
		`{"primary_window":{"used_percent":10,"limit_window_seconds":18000.1}}`,
		`{"primary_window":{"used_percent":10,"limit_window_seconds":86400}}`,
		`{"primary_window":{"used_percent":10,"limit_window_seconds":604800},"secondary_window":{"used_percent":20,"limit_window_seconds":604800}}`,
	} {
		var payload response
		if err := json.Unmarshal([]byte(`{"rate_limit":`+limits+`}`), &payload); err != nil {
			t.Fatal(err)
		}
		_, err := New(config.ProviderConfig{ID: "c"}, providerutil.Runtime{}).parse(&payload)
		if connector.Classify(err) != protocol.ErrSchemaChanged {
			t.Fatalf("limits=%s class=%s err=%v", limits, connector.Classify(err), err)
		}
	}
}

func TestReadAuthFileRejectsLinksAndUnsupportedShapes(t *testing.T) {
	dir := t.TempDir()
	realPath := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(realPath, []byte(`{"access_token":"third-party-shape"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readAuthFile(realPath); connector.Classify(err) != protocol.ErrAuth {
		t.Fatalf("unsupported shape class = %s, err=%v", connector.Classify(err), err)
	}
	linkPath := filepath.Join(dir, "link.json")
	if err := os.Symlink(realPath, linkPath); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	_, _, err := readAuthFile(linkPath)
	if connector.Classify(err) != protocol.ErrInvalidConfig || strings.Contains(err.Error(), "third-party-shape") {
		t.Fatalf("link error = %v", err)
	}
}

func TestCollectRejectsMissingCodexUsageContract(t *testing.T) {
	authPath := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(authPath, []byte(`{"tokens":{"access_token":"access"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"plan_type":"plus"}`))
	}))
	defer srv.Close()
	c := New(config.ProviderConfig{ID: "c", Type: typeID, AccountLabel: "a", Region: "custom", BaseURL: srv.URL, AuthFile: authPath}, providerutil.Runtime{})
	_, err := c.Collect(context.Background())
	if connector.Classify(err) != protocol.ErrSchemaChanged {
		t.Fatalf("class = %s, err=%v", connector.Classify(err), err)
	}
}

func TestCollectParsesCurrentAndLegacyCodeReviewLimits(t *testing.T) {
	tests := []struct {
		name   string
		extra  string
		want   string
		wantAt int64
	}{
		{
			name: "current additional rate limits",
			extra: `,"additional_rate_limits":[` +
				`{"limit_name":"codex_other","metered_feature":"codex_other","rate_limit":{"primary_window":{"used_percent":99,"reset_at":1800000001}}},` +
				`{"limit_name":"Codex Code Review","metered_feature":"github_code_review","rate_limit":{"primary_window":{"used_percent":12,"limit_window_seconds":18000,"reset_at":1800000002},"secondary_window":{"used_percent":25,"limit_window_seconds":604800,"reset_at":1800007200}}}]`,
			want: "75", wantAt: 1_800_007_200,
		},
		{
			name:  "legacy code review limit",
			extra: `,"code_review_rate_limit":{"primary_window":{"used_percent":15,"reset_at":1800003600}}`,
			want:  "85", wantAt: 1_800_003_600,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			authPath := filepath.Join(t.TempDir(), "auth.json")
			if err := os.WriteFile(authPath, []byte(`{"tokens":{"access_token":"access"}}`), 0o600); err != nil {
				t.Fatal(err)
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"plan_type":"plus","rate_limit":{"primary_window":{"used_percent":32,"limit_window_seconds":18000,"reset_at":1800003600}}` + tc.extra + `}`))
			}))
			defer srv.Close()
			c := New(config.ProviderConfig{
				ID: "c", Type: typeID, AccountLabel: "a", Region: "custom",
				BaseURL: srv.URL, AuthFile: authPath,
			}, providerutil.Runtime{})
			metrics, err := c.Collect(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if len(metrics) != 2 || metrics[1].ID != "c.code_review" ||
				metrics[1].Value.String() != tc.want || metrics[1].ResetsAt.Unix() != tc.wantAt {
				t.Fatalf("metrics = %+v", metrics)
			}
		})
	}
}

func TestCollectWeeklyPrimaryRendersOnlyWeek(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	authPath := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(authPath, []byte(`{"tokens":{"access_token":"synthetic-access"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodGet || r.URL.Path != "/backend-api/wham/usage" {
			t.Error("unexpected request")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"rate_limit":{"primary_window":{"used_percent":46,"limit_window_seconds":604800,"reset_at":1800345600},"secondary_window":null}}`))
	}))
	defer srv.Close()
	c := New(config.ProviderConfig{ID: "c", Region: "custom", BaseURL: srv.URL, AuthFile: authPath}, providerutil.Runtime{Now: func() time.Time { return now }})
	metrics, err := c.Collect(context.Background())
	if err != nil || len(metrics) != 1 || metrics[0].ID != "c.weekly" || requests != 1 {
		t.Fatalf("metrics=%+v requests=%d err=%v", metrics, requests, err)
	}
	vm := ui.Build(&protocol.MetricSnapshot{GeneratedAt: now, Metrics: metrics}, ui.BuildOptions{Now: now, Connected: true, Page: string(ui.PageCoding)})
	for _, style := range []ui.Style{ui.StyleASCII, ui.StyleRich} {
		frame := ui.RenderKioskString(vm, style)
		// SGR decoration changes bytes, not the quota text.
		plain := regexp.MustCompile(`\x1b\[[0-9;]*m`).ReplaceAllString(frame, "")
		if !strings.Contains(plain, "--   54%    -- 4D") {
			t.Fatal(frame)
		}
	}
}

func TestCollectUsesHTTP1WithoutApplicationRetry(t *testing.T) {
	authPath := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(authPath, []byte(`{"tokens":{"access_token":"access"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	requests := 0
	protocol := ""
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		protocol = r.Proto
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"plan_type":"plus","rate_limit":{"primary_window":{"used_percent":32,"limit_window_seconds":18000,"reset_at":1800003600}}}`))
	}))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	defer srv.Close()
	c := New(config.ProviderConfig{
		ID: "c", Type: typeID, AccountLabel: "a", Region: "custom",
		BaseURL: srv.URL, AuthFile: authPath,
	}, providerutil.Runtime{HTTPClient: srv.Client()})
	metrics, err := c.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if requests != 1 || protocol != "HTTP/1.1" || len(metrics) != 1 {
		t.Fatalf("requests/protocol/metrics = %d/%s/%d", requests, protocol, len(metrics))
	}
}
