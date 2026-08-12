package prometheus

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/galendai/homepi-mon/internal/config"
	"github.com/galendai/homepi-mon/internal/connector"
	"github.com/galendai/homepi-mon/internal/connector/providerutil"
	"github.com/galendai/homepi-mon/internal/protocol"
	"github.com/galendai/homepi-mon/internal/secretstore"
)

func TestCollectUsesBoundedInstantQueriesAndBuildsCurrentSummary(t *testing.T) {
	requests := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodGet {
			t.Errorf("method = %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/status/buildinfo" {
			_, _ = w.Write([]byte(`{"status":"success","data":{"version":"3.5.0"}}`))
			return
		}
		if r.URL.Path != "/api/v1/query" || r.URL.Query().Get("limit") != "65" || r.URL.Query().Get("timeout") != "10s" {
			t.Errorf("unexpected query request %s", r.URL.String())
		}
		query := r.URL.Query().Get("query")
		value := "1"
		labels := `{"instance":"nas-01:9100"}`
		switch {
		case strings.Contains(query, "node_cpu_seconds_total"):
			if !strings.Contains(query, "[5m]") {
				t.Error("CPU query is missing the fixed smoothing window")
			}
			value = "28.4"
		case strings.Contains(query, "node_memory_MemAvailable"):
			value = "42.2"
		case strings.Contains(query, "node_filesystem_avail"):
			value = "86.1"
		case strings.Contains(query, "node_network_receive"):
			value = "12000000"
		case strings.Contains(query, "node_network_transmit"):
			value = "3000000"
		case strings.Contains(query, "ALERTS"):
			value, labels = "2", `{}`
		}
		_, _ = fmt.Fprintf(w, `{"status":"success","data":{"resultType":"vector","result":[{"metric":%s,"value":[1786500000,"%s"]}]}}`, labels, value)
	}))
	defer server.Close()
	now := time.Date(2026, 8, 12, 15, 0, 0, 0, time.UTC)
	c := New(config.ProviderConfig{
		ID: "prom-main", Type: typeID, AccountLabel: "Prometheus", Region: "custom", BaseURL: server.URL,
	}, nil, providerutil.Runtime{HTTPClient: server.Client(), Now: func() time.Time { return now }})
	if _, err := c.Collect(context.Background()); err != nil {
		t.Fatal(err)
	}
	report, err := c.HomeLab()
	if err != nil {
		t.Fatal(err)
	}
	if requests != 8 || len(report.Nodes) != 1 || len(report.Services) != 1 {
		t.Fatalf("requests=%d report=%+v", requests, report)
	}
	node := report.Nodes[0]
	if node.Name != "nas-01:9100" || node.CPUPercent == nil || *node.CPUPercent != 28 ||
		node.MemoryPercent == nil || *node.MemoryPercent != 42 || node.DiskPercent == nil || *node.DiskPercent != 86 ||
		node.Status != protocol.StatusWarning {
		t.Fatalf("node = %+v", node)
	}
	service := report.Services[0]
	if service.Version != "3.5.0" || service.FiringAlerts == nil || *service.FiringAlerts != 2 || service.Status != protocol.StatusWarning {
		t.Fatalf("service = %+v", service)
	}
}

func TestCollectRejectsHighCardinalityWithoutTruncating(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[{"metric":{"instance":"a"},"value":[1,"1"]},{"metric":{"instance":"b"},"value":[1,"1"]},{"metric":{"instance":"c"},"value":[1,"1"]}]}}`))
	}))
	defer server.Close()
	c := New(config.ProviderConfig{
		ID: "prom", Type: typeID, AccountLabel: "Prom", Region: "custom", BaseURL: server.URL,
		Options: map[string]string{"max_series": "2"},
	}, nil, providerutil.Runtime{HTTPClient: server.Client()})
	_, err := c.Collect(context.Background())
	if connector.Classify(err) != protocol.ErrInvalidConfig || !strings.Contains(err.Error(), "series limit") {
		t.Fatalf("err = %v", err)
	}
}

func TestBuildReportRejectsOutOfRangeMeasurements(t *testing.T) {
	settings := settings{entityLabel: "instance", maxSeries: 64}
	base := map[string][]sample{
		"up":         {{labels: map[string]string{"instance": "nas"}, value: 1}},
		"cpu":        {{labels: map[string]string{"instance": "nas"}, value: 25}},
		"memory":     {{labels: map[string]string{"instance": "nas"}, value: 50}},
		"disk":       {{labels: map[string]string{"instance": "nas"}, value: 75}},
		"network_rx": {{labels: map[string]string{"instance": "nas"}, value: 1}},
		"network_tx": {{labels: map[string]string{"instance": "nas"}, value: 1}},
		"alerts":     {{labels: map[string]string{}, value: 0}},
	}
	spec := config.ProviderConfig{ID: "prom", AccountLabel: "Prometheus"}
	for _, tc := range []struct {
		name, key string
		value     float64
	}{
		{"negative percentage", "cpu", -1},
		{"percentage over 100", "memory", 101},
		{"negative network rate", "network_rx", -1},
		{"negative alert count", "alerts", -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			results := make(map[string][]sample, len(base))
			for key, samples := range base {
				results[key] = append([]sample(nil), samples...)
			}
			results[tc.key][0].value = tc.value
			if _, err := buildReport(spec, settings, results, "3.5.0", time.Now().UTC()); connector.Classify(err) != protocol.ErrSchemaChanged {
				t.Fatalf("buildReport() error = %v", err)
			}
		})
	}
}

func TestBuildReportMarksCriticalDiskUsage(t *testing.T) {
	results := map[string][]sample{
		"up":         {{labels: map[string]string{"instance": "nas"}, value: 1}},
		"cpu":        {{labels: map[string]string{"instance": "nas"}, value: 25}},
		"memory":     {{labels: map[string]string{"instance": "nas"}, value: 50}},
		"disk":       {{labels: map[string]string{"instance": "nas"}, value: 96}},
		"network_rx": {{labels: map[string]string{"instance": "nas"}, value: 1}},
		"network_tx": {{labels: map[string]string{"instance": "nas"}, value: 1}},
		"alerts":     {{labels: map[string]string{}, value: 0}},
	}
	report, err := buildReport(config.ProviderConfig{ID: "prom", AccountLabel: "Prometheus"},
		settings{entityLabel: "instance", maxSeries: 64}, results, "3.5.0", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if report.Nodes[0].Status != protocol.StatusCritical {
		t.Fatalf("disk 96%% status = %s", report.Nodes[0].Status)
	}
}

func TestCollectRejectsMalformedInstantVectorsAtomically(t *testing.T) {
	for _, tc := range []struct {
		name, result string
	}{
		{"missing entity label", `[{"metric":{},"value":[1,"1"]}]`},
		{"non finite value", `[{"metric":{"instance":"nas"},"value":[1,"NaN"]}]`},
		{"duplicate entity", `[{"metric":{"instance":"nas"},"value":[1,"1"]},{"metric":{"instance":"nas"},"value":[1,"2"]}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/api/v1/status/buildinfo" {
					_, _ = w.Write([]byte(`{"status":"success","data":{"version":"3.5.0"}}`))
					return
				}
				_, _ = fmt.Fprintf(w, `{"status":"success","data":{"resultType":"vector","result":%s}}`, tc.result)
			}))
			defer server.Close()
			c := New(config.ProviderConfig{ID: "prom", Type: typeID, AccountLabel: "Prom", Region: "custom", BaseURL: server.URL},
				nil, providerutil.Runtime{HTTPClient: server.Client()})
			if _, err := c.Collect(context.Background()); connector.Classify(err) != protocol.ErrSchemaChanged {
				t.Fatalf("Collect() error = %v", err)
			}
			report, _ := c.HomeLab()
			if len(report.Nodes) != 0 || len(report.Services) != 0 {
				t.Fatalf("malformed response published partial report: %+v", report)
			}
		})
	}
}

func TestCollectDoesNotTrustUnknownTLSCertificates(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
	}))
	defer server.Close()
	c := New(config.ProviderConfig{ID: "prom", Type: typeID, AccountLabel: "Prom", Region: "custom", BaseURL: server.URL}, nil, providerutil.Runtime{})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := c.Collect(ctx); connector.Classify(err) != protocol.ErrNetwork {
		t.Fatalf("unknown TLS certificate error = %v", err)
	}
}

func TestValidateConfigRejectsUnsafePrometheusOptionsAndPlainHTTP(t *testing.T) {
	tests := []config.ProviderConfig{
		{ID: "p", Type: typeID, AccountLabel: "p", Region: "custom", BaseURL: "http://127.0.0.1:9090"},
		{ID: "p", Type: typeID, AccountLabel: "p", Region: "custom", BaseURL: "https://prom.example", Options: map[string]string{"unknown": "x"}},
		{ID: "p", Type: typeID, AccountLabel: "p", Region: "custom", BaseURL: "https://prom.example", Options: map[string]string{"max_series": "1001"}},
		{ID: "p", Type: typeID, AccountLabel: "p", Region: "custom", BaseURL: "https://prom.example", Options: map[string]string{"cpu_query": "bad\nquery"}},
	}
	for _, spec := range tests {
		if err := New(spec, nil, providerutil.Runtime{}).ValidateConfig(); connector.Classify(err) != protocol.ErrInvalidConfig {
			t.Errorf("ValidateConfig(%+v) = %v", spec, err)
		}
	}
}

func TestCollectClassifiesAuthenticationWithoutLeakingSecret(t *testing.T) {
	const secret = "prometheus-test-secret"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+secret {
			t.Error("missing bearer token")
		}
		http.Error(w, "do not echo body secret", http.StatusUnauthorized)
	}))
	defer server.Close()
	store := testSecretStore(t, "keyring:prom", secret)
	c := New(config.ProviderConfig{ID: "prom", Type: typeID, AccountLabel: "p", Region: "custom", BaseURL: server.URL, SecretRef: "keyring:prom"}, store, providerutil.Runtime{HTTPClient: server.Client()})
	_, err := c.Collect(context.Background())
	if connector.Classify(err) != protocol.ErrAuth || strings.Contains(err.Error(), secret) {
		t.Fatalf("err = %v", err)
	}
}

func testSecretStore(t *testing.T, ref, value string) secretstore.Store {
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
