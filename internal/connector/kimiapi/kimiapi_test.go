package kimiapi

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

func TestCollectNormalizesKimiBalancesExactly(t *testing.T) {
	const secret = "sk-kimi-api-test"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/users/me/balance" || r.Header.Get("Authorization") != "Bearer "+secret {
			t.Error("unexpected Kimi API request path or authentication header")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"status":true,"data":{"available_balance":49.59,"voucher_balance":46.59,"cash_balance":3.00}}`))
	}))
	defer srv.Close()
	store := kimiStore(t, "keyring:kimi-api", secret)
	c := New(config.ProviderConfig{
		ID: "kimi-api-main", Type: typeID, AccountLabel: "main", Region: "custom",
		BaseURL: srv.URL, SecretRef: "keyring:kimi-api",
	}, store, providerutil.Runtime{Now: func() time.Time { return time.Unix(1_800_000_000, 0) }})
	metrics, err := c.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(metrics) != 3 || metrics[0].Value.String() != "49.59" ||
		metrics[1].Value.String() != "46.59" || metrics[2].Value.String() != "3.00" {
		t.Fatalf("metrics = %+v", metrics)
	}
}

func TestCollectAllowsNegativeKimiCashBalance(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"status":true,"data":{"available_balance":0,"voucher_balance":0,"cash_balance":-1.0373199}}`))
	}))
	defer srv.Close()
	c := New(config.ProviderConfig{
		ID: "kimi-api-main", Type: typeID, AccountLabel: "main", Region: "custom",
		BaseURL: srv.URL, SecretRef: "keyring:kimi-api",
	}, kimiStore(t, "keyring:kimi-api", "secret"), providerutil.Runtime{})

	metrics, err := c.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(metrics) != 3 || metrics[0].Value.String() != "0" ||
		metrics[1].Value.String() != "0" || metrics[2].Value.String() != "-1.0373199" {
		t.Fatalf("metrics = %+v", metrics)
	}
	for _, metric := range metrics {
		if metric.Status != protocol.StatusOK || metric.Precision != protocol.PrecisionExact {
			t.Fatalf("metric = %+v", metric)
		}
	}
}

func TestCollectRejectsInvalidKimiBalanceSemantics(t *testing.T) {
	tests := map[string]string{
		"negative available": `{"code":0,"status":true,"data":{"available_balance":-0.01,"voucher_balance":0,"cash_balance":0}}`,
		"negative voucher":   `{"code":0,"status":true,"data":{"available_balance":0,"voucher_balance":-0.01,"cash_balance":0}}`,
		"invalid cash":       `{"code":0,"status":true,"data":{"available_balance":0,"voucher_balance":0,"cash_balance":"unknown"}}`,
	}
	for name, payload := range tests {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(payload))
			}))
			defer srv.Close()
			c := New(config.ProviderConfig{
				ID: "k", Type: typeID, AccountLabel: "a", Region: "custom",
				BaseURL: srv.URL, SecretRef: "keyring:k",
			}, kimiStore(t, "keyring:k", "secret"), providerutil.Runtime{})
			_, err := c.Collect(context.Background())
			if connector.Classify(err) != protocol.ErrSchemaChanged {
				t.Fatalf("class = %s, err=%v", connector.Classify(err), err)
			}
		})
	}
}

func TestCollectRejectsMissingKimiAPIContractFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"status":true}`))
	}))
	defer srv.Close()
	c := New(config.ProviderConfig{ID: "k", Type: typeID, AccountLabel: "a", Region: "custom", BaseURL: srv.URL, SecretRef: "keyring:k"},
		kimiStore(t, "keyring:k", "secret"), providerutil.Runtime{})
	_, err := c.Collect(context.Background())
	if connector.Classify(err) != protocol.ErrSchemaChanged {
		t.Fatalf("class = %s, err=%v", connector.Classify(err), err)
	}
}

func kimiStore(t *testing.T, ref, value string) secretstore.Store {
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
