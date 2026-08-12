package grafana

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/galendai/homepi-mon/internal/config"
	"github.com/galendai/homepi-mon/internal/connector"
	"github.com/galendai/homepi-mon/internal/connector/providerutil"
	"github.com/galendai/homepi-mon/internal/protocol"
	"github.com/galendai/homepi-mon/internal/secretstore"
)

func TestCollectUsesModernReadOnlyGrafanaAPIs(t *testing.T) {
	const token = "grafana-test-token"
	var paths []string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer "+token {
			t.Errorf("request = %s auth=%q", r.Method, r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/health":
			_, _ = w.Write([]byte(`{"commit":"abc","database":"ok","version":"12.1.0"}`))
		case "/api/alertmanager/grafana/api/v2/alerts":
			_, _ = w.Write([]byte(`[{"labels":{"severity":"critical"},"status":{"state":"active"}},{"labels":{"severity":"warning"},"status":{"state":"active"}}]`))
		case "/apis/rules.alerting.grafana.app/v0alpha1/namespaces/default/alertrules":
			_, _ = w.Write([]byte(`{"items":[{},{}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	now := time.Date(2026, 8, 12, 15, 0, 0, 0, time.UTC)
	c := New(spec(server.URL), testStore(t, token), providerutil.Runtime{HTTPClient: server.Client(), Now: func() time.Time { return now }})
	if _, err := c.Collect(context.Background()); err != nil {
		t.Fatal(err)
	}
	report, _ := c.HomeLab()
	if len(paths) != 3 || len(report.Services) != 1 {
		t.Fatalf("paths=%v report=%+v", paths, report)
	}
	service := report.Services[0]
	if service.Version != "12.1.0" || service.APIGeneration != "apis-v0alpha1" || service.FiringAlerts == nil || *service.FiringAlerts != 2 || service.Status != protocol.StatusCritical {
		t.Fatalf("service=%+v", service)
	}
}

func TestCollectFallsBackToLegacyRulesOnlyAfter404(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/health":
			_, _ = w.Write([]byte(`{"database":"ok","version":"11.6.0"}`))
		case "/api/alertmanager/grafana/api/v2/alerts":
			_, _ = w.Write([]byte(`[]`))
		case "/apis/rules.alerting.grafana.app/v0alpha1/namespaces/default/alertrules":
			http.NotFound(w, r)
		case "/api/v1/provisioning/alert-rules":
			_, _ = w.Write([]byte(`[{},{}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	c := New(spec(server.URL), testStore(t, "token"), providerutil.Runtime{HTTPClient: server.Client()})
	if _, err := c.Collect(context.Background()); err != nil {
		t.Fatal(err)
	}
	report, _ := c.HomeLab()
	if report.Services[0].APIGeneration != "api-legacy" || report.Services[0].Message != "rules=2" {
		t.Fatalf("service=%+v", report.Services[0])
	}
}

func TestCollectDoesNotFallbackOnAuthenticationFailure(t *testing.T) {
	legacyCalled := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/health":
			_, _ = w.Write([]byte(`{"database":"ok","version":"12.0.0"}`))
		case "/api/alertmanager/grafana/api/v2/alerts":
			_, _ = w.Write([]byte(`[]`))
		case "/api/v1/provisioning/alert-rules":
			legacyCalled = true
			_, _ = w.Write([]byte(`[]`))
		default:
			http.Error(w, "denied", http.StatusForbidden)
		}
	}))
	defer server.Close()
	c := New(spec(server.URL), testStore(t, "token"), providerutil.Runtime{HTTPClient: server.Client()})
	_, err := c.Collect(context.Background())
	if connector.Classify(err) != protocol.ErrAuth || legacyCalled {
		t.Fatalf("err=%v legacy=%v", err, legacyCalled)
	}
}

func TestValidateConfigRejectsGrafanaUnsafeValues(t *testing.T) {
	tests := []config.ProviderConfig{
		{ID: "g", Type: typeID, AccountLabel: "g", Region: "custom", BaseURL: "http://grafana.local", SecretRef: "keyring:g"},
		{ID: "g", Type: typeID, AccountLabel: "g", Region: "custom", BaseURL: "https://grafana.local", SecretRef: "keyring:g", Options: map[string]string{"other": "x"}},
		{ID: "g", Type: typeID, AccountLabel: "g", Region: "custom", BaseURL: "https://grafana.local", SecretRef: "keyring:g", Options: map[string]string{"namespace": "bad/name"}},
		{ID: "g", Type: typeID, AccountLabel: "g", Region: "custom", BaseURL: "https://grafana.local", SecretRef: "keyring:g", Options: map[string]string{"max_alerts": "1001"}},
	}
	for _, item := range tests {
		if err := New(item, nil, providerutil.Runtime{}).ValidateConfig(); connector.Classify(err) != protocol.ErrInvalidConfig {
			t.Errorf("err=%v spec=%+v", err, item)
		}
	}
}

func spec(base string) config.ProviderConfig {
	return config.ProviderConfig{ID: "grafana-main", Type: typeID, AccountLabel: "Grafana", Region: "custom", BaseURL: base, SecretRef: "keyring:grafana"}
}

func testStore(t *testing.T, token string) secretstore.Store {
	t.Helper()
	store, err := secretstore.NewFileBackend(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set(context.Background(), "keyring:grafana", token); err != nil {
		t.Fatal(err)
	}
	return store
}
