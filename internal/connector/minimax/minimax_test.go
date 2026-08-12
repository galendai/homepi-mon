package minimax

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/galendai/homepi-mon/internal/config"
	"github.com/galendai/homepi-mon/internal/connector"
	"github.com/galendai/homepi-mon/internal/connector/providerutil"
	"github.com/galendai/homepi-mon/internal/protocol"
	"github.com/galendai/homepi-mon/internal/secretstore"
)

func TestCollectFallsBackOnceAndNormalizesQuota(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path == "/v1/token_plan/remains" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"base_resp":{"status_code":0},"model_remains":[{"current_interval_total_count":100,"current_interval_usage_count":32,"end_time":1800003600}]}`))
	}))
	defer srv.Close()
	store := minimaxStore(t, "keyring:minimax", "sk-minimax-test")
	c := New(config.ProviderConfig{
		ID: "minimax-main", Type: typeID, AccountLabel: "main", Region: "custom",
		BaseURL: srv.URL, SecretRef: "keyring:minimax",
	}, store, providerutil.Runtime{Now: func() time.Time { return time.Unix(1_800_000_000, 0) }})
	metrics, err := c.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	wantPaths := []string{"/v1/token_plan/remains", "/v1/api/openplatform/coding_plan/remains"}
	if !reflect.DeepEqual(paths, wantPaths) {
		t.Fatalf("paths = %v, want %v", paths, wantPaths)
	}
	if len(metrics) != 1 || metrics[0].Value.String() != "68" ||
		metrics[0].Limit.String() != "100" || metrics[0].ResetsAt.Unix() != 1_800_003_600 {
		t.Fatalf("metrics = %+v", metrics)
	}
}

func TestCollectDoesNotFallbackOnAuthenticationFailure(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	c := New(config.ProviderConfig{ID: "m", Type: typeID, AccountLabel: "a", Region: "custom", BaseURL: srv.URL, SecretRef: "keyring:m"},
		minimaxStore(t, "keyring:m", "secret"), providerutil.Runtime{})
	_, err := c.Collect(context.Background())
	if connector.Classify(err) != protocol.ErrAuth || requests != 1 {
		t.Fatalf("class/requests = %s/%d, err=%v", connector.Classify(err), requests, err)
	}
}

func TestCollectFallsBackOnPrimarySchemaMismatch(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		if requests == 1 {
			_, _ = w.Write([]byte(`{}`))
			return
		}
		_, _ = w.Write([]byte(`{"base_resp":{"status_code":0},"model_remains":[{"current_interval_total_count":100,"current_interval_remaining_count":68}]}`))
	}))
	defer srv.Close()
	c := New(config.ProviderConfig{ID: "m", Type: typeID, AccountLabel: "a", Region: "custom", BaseURL: srv.URL, SecretRef: "keyring:m"},
		minimaxStore(t, "keyring:m", "secret"), providerutil.Runtime{})
	metrics, err := c.Collect(context.Background())
	if err != nil || requests != 2 || len(metrics) != 1 || metrics[0].Value.String() != "68" {
		t.Fatalf("metrics/requests = %+v/%d, err=%v", metrics, requests, err)
	}
}

func TestCollectSelectsChatQuotaAndUsesRemainingPercentages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
  "base_resp":{"status_code":0},
  "model_remains":[
    {"model_name":"speech-hd","current_interval_total_count":0,"current_interval_usage_count":0,"current_interval_status":3},
    {"model_name":"general","current_interval_total_count":0,"current_interval_usage_count":0,"current_interval_remaining_percent":97,"current_interval_status":1,"end_time":1800003600,"current_weekly_total_count":0,"current_weekly_usage_count":0,"current_weekly_remaining_percent":77,"current_weekly_status":1,"weekly_end_time":1800604800}
  ]
}`))
	}))
	defer srv.Close()
	c := New(config.ProviderConfig{ID: "m", Type: typeID, AccountLabel: "a", Region: "custom", BaseURL: srv.URL, SecretRef: "keyring:m"},
		minimaxStore(t, "keyring:m", "secret"), providerutil.Runtime{Now: func() time.Time { return time.Unix(1_800_000_000, 0) }})

	metrics, err := c.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(metrics) != 2 {
		t.Fatalf("metrics = %+v, want interval and weekly", metrics)
	}
	if metrics[0].Value.String() != "97" || metrics[0].Limit.String() != "100" ||
		metrics[0].Window != protocol.WindowRolling5h || metrics[0].ResetsAt.Unix() != 1_800_003_600 {
		t.Fatalf("interval metric = %+v", metrics[0])
	}
	if metrics[1].Value.String() != "77" || metrics[1].Limit.String() != "100" ||
		metrics[1].Window != protocol.WindowWeekly || metrics[1].ResetsAt.Unix() != 1_800_604_800 {
		t.Fatalf("weekly metric = %+v", metrics[1])
	}
}

func TestCollectSkipsZeroQuotaMediaRowForLegacyCounts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"base_resp":{"status_code":0},"model_remains":[{"model_name":"image-01","current_interval_total_count":0,"current_interval_usage_count":0},{"model_name":"MiniMax-M2","current_interval_total_count":100,"current_interval_usage_count":32}]}`))
	}))
	defer srv.Close()
	c := New(config.ProviderConfig{ID: "m", Type: typeID, AccountLabel: "a", Region: "custom", BaseURL: srv.URL, SecretRef: "keyring:m"},
		minimaxStore(t, "keyring:m", "secret"), providerutil.Runtime{})

	metrics, err := c.Collect(context.Background())
	if err != nil || len(metrics) != 1 || metrics[0].Value.String() != "68" {
		t.Fatalf("metrics = %+v, err = %v", metrics, err)
	}
}

func TestCollectRepresentsUnlimitedChatQuotaAsUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"base_resp":{"status_code":0},"model_remains":[{"model_name":"general","current_interval_total_count":0,"current_interval_usage_count":0,"current_interval_remaining_percent":100,"current_interval_status":3}]}`))
	}))
	defer srv.Close()
	c := New(config.ProviderConfig{ID: "m", Type: typeID, AccountLabel: "a", Region: "custom", BaseURL: srv.URL, SecretRef: "keyring:m"},
		minimaxStore(t, "keyring:m", "secret"), providerutil.Runtime{})

	metrics, err := c.Collect(context.Background())
	if err != nil || len(metrics) != 1 {
		t.Fatalf("metrics = %+v, err = %v", metrics, err)
	}
	if metrics[0].Value != nil || metrics[0].Limit != nil || metrics[0].Precision != protocol.PrecisionUnavailable ||
		metrics[0].Message != "MiniMax interval quota is unlimited" {
		t.Fatalf("metric = %+v", metrics[0])
	}
}

func minimaxStore(t *testing.T, ref, value string) secretstore.Store {
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
