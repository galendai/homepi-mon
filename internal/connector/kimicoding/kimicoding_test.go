package kimicoding

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
