package portainer

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

func TestCollectUsesOnlyReadOnlyPortainerAPIsAndAggregatesCounts(t *testing.T) {
	const token = "portainer-test-token"
	var requests []string
	offlineGatewayCalled := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		if r.Method != http.MethodGet || r.Header.Get("X-API-Key") != token {
			t.Errorf("request=%s header=%q", r.Method, r.Header.Get("X-API-Key"))
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/system/status":
			_, _ = w.Write([]byte(`{"Version":"2.38.0"}`))
		case "/api/endpoints":
			_, _ = w.Write([]byte(`[{"Id":1,"Name":"local","Status":1},{"Id":2,"Name":"edge","Status":2}]`))
		case "/api/stacks":
			_, _ = w.Write([]byte(`[{},{}]`))
		case "/api/endpoints/1/docker/containers/json":
			if r.URL.Query().Get("all") != "true" {
				t.Error("all=true missing")
			}
			_, _ = w.Write([]byte(`[{"State":"running"},{"State":"running"},{"State":"exited"}]`))
		case "/api/endpoints/2/docker/containers/json":
			offlineGatewayCalled = true
			_, _ = w.Write([]byte(`[{"State":"dead"}]`))
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
	if len(requests) != 4 || offlineGatewayCalled || len(report.Services) != 1 {
		t.Fatalf("requests=%v report=%+v", requests, report)
	}
	s := report.Services[0]
	if s.Version != "2.38.0" || s.APIGeneration != "system-status" || *s.EnvironmentsTotal != 2 || *s.EnvironmentsOnline != 1 || *s.ContainersRunning != 2 || *s.ContainersStopped != 1 || *s.ContainersFailed != 0 || *s.Stacks != 2 || s.Status != protocol.StatusWarning {
		t.Fatalf("service=%+v", s)
	}
}

func TestCollectFallsBackToLegacyStatusOnlyAfter404(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/system/status":
			http.NotFound(w, r)
		case "/api/status":
			_, _ = w.Write([]byte(`{"Version":"2.17.1"}`))
		case "/api/endpoints", "/api/stacks":
			_, _ = w.Write([]byte(`[]`))
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
	if report.Services[0].APIGeneration != "legacy-status" {
		t.Fatalf("service=%+v", report.Services[0])
	}
}

func TestCollectRejectsEnvironmentCardinalityBeforeGatewayRequests(t *testing.T) {
	gatewayCalled := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/system/status":
			_, _ = w.Write([]byte(`{"Version":"2.38.0"}`))
		case "/api/endpoints":
			_, _ = w.Write([]byte(`[{"Id":1,"Name":"a","Status":1},{"Id":2,"Name":"b","Status":1}]`))
		case "/api/stacks":
			_, _ = w.Write([]byte(`[]`))
		default:
			gatewayCalled = true
			_, _ = w.Write([]byte(`[]`))
		}
	}))
	defer server.Close()
	item := spec(server.URL)
	item.Options = map[string]string{"max_environments": "1"}
	c := New(item, testStore(t, "token"), providerutil.Runtime{HTTPClient: server.Client()})
	_, err := c.Collect(context.Background())
	if connector.Classify(err) != protocol.ErrInvalidConfig || gatewayCalled {
		t.Fatalf("err=%v gateway=%v", err, gatewayCalled)
	}
}

func TestCollectDoesNotFallbackOnPortainerAuthFailure(t *testing.T) {
	legacyCalled := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/status" {
			legacyCalled = true
		}
		http.Error(w, "denied", http.StatusUnauthorized)
	}))
	defer server.Close()
	c := New(spec(server.URL), testStore(t, "token"), providerutil.Runtime{HTTPClient: server.Client()})
	_, err := c.Collect(context.Background())
	if connector.Classify(err) != protocol.ErrAuth || legacyCalled {
		t.Fatalf("err=%v legacy=%v", err, legacyCalled)
	}
}

func TestValidateConfigRejectsPortainerUnsafeValues(t *testing.T) {
	tests := []config.ProviderConfig{
		{ID: "p", Type: typeID, AccountLabel: "p", Region: "custom", BaseURL: "http://portainer.local", SecretRef: "keyring:p"},
		{ID: "p", Type: typeID, AccountLabel: "p", Region: "custom", BaseURL: "https://portainer.local", SecretRef: "keyring:p", Options: map[string]string{"other": "x"}},
		{ID: "p", Type: typeID, AccountLabel: "p", Region: "custom", BaseURL: "https://portainer.local", SecretRef: "keyring:p", Options: map[string]string{"max_environments": "8"}},
		{ID: "p", Type: typeID, AccountLabel: "p", Region: "custom", BaseURL: "https://portainer.local", SecretRef: "keyring:p", Options: map[string]string{"max_containers": "5001"}},
	}
	for _, item := range tests {
		if err := New(item, nil, providerutil.Runtime{}).ValidateConfig(); connector.Classify(err) != protocol.ErrInvalidConfig {
			t.Errorf("err=%v", err)
		}
	}
}

func spec(base string) config.ProviderConfig {
	return config.ProviderConfig{ID: "portainer-main", Type: typeID, AccountLabel: "Portainer", Region: "custom", BaseURL: base, SecretRef: "keyring:portainer"}
}
func testStore(t *testing.T, token string) secretstore.Store {
	t.Helper()
	store, err := secretstore.NewFileBackend(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set(context.Background(), "keyring:portainer", token); err != nil {
		t.Fatal(err)
	}
	return store
}
