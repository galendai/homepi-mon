package codexusage

import (
	"context"
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/galendai/homepi-mon/internal/config"
	"github.com/galendai/homepi-mon/internal/connector"
	"github.com/galendai/homepi-mon/internal/connector/providerutil"
	"github.com/galendai/homepi-mon/internal/protocol"
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
				`{"limit_name":"Codex Code Review","metered_feature":"github_code_review","rate_limit":{"primary_window":{"used_percent":12,"reset_at":1800000002},"secondary_window":{"used_percent":25,"reset_at":1800007200}}}]`,
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
				_, _ = w.Write([]byte(`{"plan_type":"plus","rate_limit":{"primary_window":{"used_percent":32,"reset_at":1800003600}}` + tc.extra + `}`))
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
		_, _ = w.Write([]byte(`{"plan_type":"plus","rate_limit":{"primary_window":{"used_percent":32,"reset_at":1800003600}}}`))
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
