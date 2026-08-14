package deepseek

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

func TestCollectNormalizesDeepSeekBalancesExactly(t *testing.T) {
	const secret = "sk-deepseek-test"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/user/balance" ||
			r.Header.Get("Authorization") != "Bearer "+secret {
			t.Error("unexpected DeepSeek request method, path or authentication header")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":"110.00","granted_balance":"10.00","topped_up_balance":"100.00"}]}`))
	}))
	defer srv.Close()
	store := testStore(t, "keyring:deepseek", secret)
	now := time.Date(2026, 8, 10, 8, 0, 0, 0, time.UTC)
	c := New(config.ProviderConfig{
		ID: "deepseek-main", Type: typeID, AccountLabel: "main", Region: "custom",
		BaseURL: srv.URL, SecretRef: "keyring:deepseek",
	}, store, providerutil.Runtime{Now: func() time.Time { return now }})
	metrics, err := c.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(metrics) != 3 || metrics[0].Value.String() != "110.00" ||
		metrics[1].Value.String() != "10.00" || metrics[2].Value.String() != "100.00" {
		t.Fatalf("metrics = %+v", metrics)
	}
	for i := range metrics {
		if err := metrics[i].Validate(); err != nil {
			t.Fatalf("metric %d invalid: %v", i, err)
		}
	}
}

func TestCollectAllowsNegativeDeepSeekSettledBalances(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"is_available":false,"balance_infos":[{"currency":"CNY","total_balance":"-1.80","granted_balance":"0.00","topped_up_balance":"-1.80"}]}`))
	}))
	defer srv.Close()
	c := New(config.ProviderConfig{
		ID: "deepseek-main", Type: typeID, AccountLabel: "main", Region: "custom",
		BaseURL: srv.URL, SecretRef: "keyring:deepseek",
	}, testStore(t, "keyring:deepseek", "secret"), providerutil.Runtime{})
	metrics, err := c.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(metrics) != 3 || metrics[0].Value.String() != "-1.80" ||
		metrics[1].Value.String() != "0.00" || metrics[2].Value.String() != "-1.80" {
		t.Fatalf("metrics = %+v", metrics)
	}
	for i := range metrics {
		if err := metrics[i].Validate(); err != nil {
			t.Fatalf("metric %d invalid: %v", i, err)
		}
		if metrics[i].Status != protocol.StatusError || metrics[i].Message != "account unavailable" {
			t.Fatalf("metric %d status = %s, message = %q", i, metrics[i].Status, metrics[i].Message)
		}
	}
}

func TestCollectRejectsInvalidDeepSeekBalanceSemantics(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "negative granted",
			body: `{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":"-1.80","granted_balance":"-0.01","topped_up_balance":"-1.79"}]}`,
		},
		{
			name: "invalid total",
			body: `{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":"unknown","granted_balance":"0.00","topped_up_balance":"-1.80"}]}`,
		},
		{
			name: "invalid topped up",
			body: `{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":"-1.80","granted_balance":"0.00","topped_up_balance":null}]}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()
			c := New(config.ProviderConfig{
				ID: "d", Type: typeID, AccountLabel: "a", Region: "custom",
				BaseURL: srv.URL, SecretRef: "keyring:d",
			}, testStore(t, "keyring:d", "secret"), providerutil.Runtime{})
			_, err := c.Collect(context.Background())
			if connector.Classify(err) != protocol.ErrSchemaChanged {
				t.Fatalf("class = %s, err=%v", connector.Classify(err), err)
			}
		})
	}
}

func TestCollectRejectsMissingDeepSeekContractFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"is_available":true}`))
	}))
	defer srv.Close()
	c := New(config.ProviderConfig{ID: "d", Type: typeID, AccountLabel: "a", Region: "custom", BaseURL: srv.URL, SecretRef: "keyring:d"},
		testStore(t, "keyring:d", "secret"), providerutil.Runtime{})
	_, err := c.Collect(context.Background())
	if connector.Classify(err) != protocol.ErrSchemaChanged {
		t.Fatalf("class = %s, err=%v", connector.Classify(err), err)
	}
}

func testStore(t *testing.T, ref, value string) secretstore.Store {
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
