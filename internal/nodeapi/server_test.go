package nodeapi_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/galendai/homepi-mon/internal/decimal"
	"github.com/galendai/homepi-mon/internal/nodeapi"
	"github.com/galendai/homepi-mon/internal/protocol"
	"github.com/galendai/homepi-mon/internal/state"
)

const (
	deviceID    = "pi-kiosk"
	deviceToken = "test-device-token-0123456789"
	otherToken  = "test-other-token-9876543210"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func metric(id, value string) protocol.ProviderMetric {
	v := decimal.MustParse(value)
	l := decimal.MustParse("100")
	now := time.Now().UTC()
	return protocol.ProviderMetric{
		ID: id, Provider: "mock", DisplayName: "Mock",
		MetricKind: protocol.KindQuota, Value: &v, Limit: &l,
		Unit: "percent", Window: protocol.WindowRolling5h,
		ObservedAt: now, Precision: protocol.PrecisionExact,
		SourceKind: protocol.SourceMock, Status: protocol.StatusOK,
		Group: "coding",
	}
}

func newServer(t *testing.T) (*httptest.Server, *state.Current) {
	t.Helper()
	store, err := state.New(state.Options{NodeID: "dev-mac", NodeLabel: "DEV-MAC", Epoch: "epoch-a"})
	if err != nil {
		t.Fatal(err)
	}
	srv, err := nodeapi.New(nodeapi.Options{
		Store:        store,
		Devices:      []nodeapi.Device{{ID: deviceID, Token: deviceToken}, {ID: "pi-other", Token: otherToken}},
		Logger:       quiet(),
		PollInterval: 10 * time.Millisecond,
		Heartbeat:    50 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts, store
}

func get(t *testing.T, ts *httptest.Server, path, token, etag string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, ts.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// E-A001 (Test-Module-001 E001): an authenticated device gets the full
// snapshot with an ETag and no secrets.
func TestSnapshotRequiresTokenAndReturnsETag(t *testing.T) {
	ts, store := newServer(t)
	if _, err := store.ApplyMetrics(1, []protocol.ProviderMetric{metric("m1", "68")}); err != nil {
		t.Fatal(err)
	}

	resp := get(t, ts, "/v1/devices/"+deviceID+"/snapshot", deviceToken, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if resp.Header.Get("ETag") == "" {
		t.Error("missing ETag")
	}
	raw, _ := io.ReadAll(resp.Body)

	snap, err := protocol.DecodeSnapshot(raw)
	if err != nil {
		t.Fatalf("response is not a valid snapshot: %v", err)
	}
	if len(snap.Metrics) != 1 || snap.SourceNode != "dev-mac" {
		t.Fatalf("unexpected snapshot: %+v", snap)
	}
	if hits := protocol.AuditJSON(raw, []string{deviceToken}); len(hits) != 0 {
		t.Fatalf("snapshot response leaked: %v", hits)
	}
}

// E-A002 (Test-Module-001 E002): an unchanged snapshot answers 304.
func TestIfNoneMatchReturns304(t *testing.T) {
	ts, store := newServer(t)
	if _, err := store.ApplyMetrics(1, []protocol.ProviderMetric{metric("m1", "68")}); err != nil {
		t.Fatal(err)
	}

	first := get(t, ts, "/v1/devices/"+deviceID+"/snapshot", deviceToken, "")
	etag := first.Header.Get("ETag")

	second := get(t, ts, "/v1/devices/"+deviceID+"/snapshot", deviceToken, etag)
	if second.StatusCode != http.StatusNotModified {
		t.Fatalf("status = %d, want 304", second.StatusCode)
	}

	// A data change must invalidate the tag.
	if _, err := store.ApplyMetrics(2, []protocol.ProviderMetric{metric("m1", "42")}); err != nil {
		t.Fatal(err)
	}
	third := get(t, ts, "/v1/devices/"+deviceID+"/snapshot", deviceToken, etag)
	if third.StatusCode != http.StatusOK {
		t.Fatalf("status after change = %d, want 200", third.StatusCode)
	}
}

// E-A003 (Test-Module-001 E005): a device cannot read another device's path,
// and the rejection does not reveal whether that device exists.
func TestDeviceScopedAuthorization(t *testing.T) {
	ts, store := newServer(t)
	if _, err := store.ApplyMetrics(1, []protocol.ProviderMetric{metric("m1", "68")}); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name, path, token string
	}{
		{"no token", "/v1/devices/" + deviceID + "/snapshot", ""},
		{"wrong token", "/v1/devices/" + deviceID + "/snapshot", otherToken},
		{"other device path", "/v1/devices/pi-other/snapshot", deviceToken},
		{"unknown device", "/v1/devices/pi-ghost/snapshot", deviceToken},
	}
	for _, c := range cases {
		resp := get(t, ts, c.path, c.token, "")
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s: status = %d, want 401", c.name, resp.StatusCode)
		}
		body, _ := io.ReadAll(resp.Body)
		var se protocol.StreamError
		if err := json.Unmarshal(body, &se); err != nil {
			t.Errorf("%s: error body is not JSON: %s", c.name, body)
			continue
		}
		if se.Code != "unauthorized" {
			t.Errorf("%s: code = %q, want unauthorized", c.name, se.Code)
		}
		// All rejections are identical, so the response cannot enumerate devices.
		if se.Message != "device token is missing or invalid" {
			t.Errorf("%s: message %q distinguishes failure modes", c.name, se.Message)
		}
	}
}

// E-A004: the health probe never exposes device tokens or metric values.
func TestHealthProbeIsMinimal(t *testing.T) {
	ts, store := newServer(t)
	if _, err := store.ApplyMetrics(1, []protocol.ProviderMetric{metric("m1", "68")}); err != nil {
		t.Fatal(err)
	}
	resp := get(t, ts, "/healthz", "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	raw, _ := io.ReadAll(resp.Body)
	if hits := protocol.AuditJSON(raw, []string{deviceToken, otherToken}); len(hits) != 0 {
		t.Fatalf("health probe leaked: %v", hits)
	}
}

// E-A005 (Test-Module-001 E003): an empty daemon still serves a valid, empty
// snapshot instead of an error, so the display can show its wait state.
func TestEmptyDaemonServesValidSnapshot(t *testing.T) {
	ts, _ := newServer(t)
	resp := get(t, ts, "/v1/devices/"+deviceID+"/snapshot", deviceToken, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	raw, _ := io.ReadAll(resp.Body)
	snap, err := protocol.DecodeSnapshot(raw)
	if err != nil {
		t.Fatalf("empty snapshot invalid: %v", err)
	}
	if len(snap.Metrics) != 0 || snap.SnapshotVersion != 0 {
		t.Fatalf("unexpected empty snapshot: %+v", snap)
	}
}

func TestHomeLabOnlySnapshotIsPublishable(t *testing.T) {
	ts, store := newServer(t)
	healthy := true
	if err := store.ApplyConnectorHomeLab("grafana-main", protocol.HomeLabReport{Services: []protocol.HomeLabService{{
		ID: "grafana-main.service", Name: "Grafana", Kind: "grafana", Version: "12.1.0",
		Healthy: &healthy, ObservedAt: time.Now().UTC(), Status: protocol.StatusOK,
	}}}); err != nil {
		t.Fatal(err)
	}
	resp := get(t, ts, "/v1/devices/"+deviceID+"/snapshot", deviceToken, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	raw, _ := io.ReadAll(resp.Body)
	snap, err := protocol.DecodeSnapshot(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Metrics) != 0 || len(snap.HomeLabServices) != 1 || !store.HasData() {
		t.Fatalf("HomeLab-only snapshot was not publishable: %+v", snap)
	}
}

// E-A006: the stream requires the same device token as the snapshot endpoint.
func TestStreamRequiresToken(t *testing.T) {
	ts, _ := newServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		ts.URL+"/v1/devices/"+deviceID+"/stream", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated stream status = %d, want 401", resp.StatusCode)
	}
}

func TestServerAllowsZeroPairedDevices(t *testing.T) {
	store, err := state.New(state.Options{NodeID: "dev-mac", Epoch: "epoch-empty"})
	if err != nil {
		t.Fatal(err)
	}
	srv, err := nodeapi.New(nodeapi.Options{Store: store, Logger: quiet()})
	if err != nil {
		t.Fatalf("zero-device server failed to start: %v", err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	resp := get(t, ts, "/v1/devices/pi-kiosk/snapshot", deviceToken, "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("zero-device API status = %d, want 401", resp.StatusCode)
	}
}

func TestDynamicRevocationRejectsRequestsAndClosesExistingStream(t *testing.T) {
	store, err := state.New(state.Options{NodeID: "dev-mac", Epoch: "epoch-revoke"})
	if err != nil {
		t.Fatal(err)
	}
	var revoked atomic.Bool
	srv, err := nodeapi.New(nodeapi.Options{
		Store:   store,
		Devices: []nodeapi.Device{{ID: deviceID, Token: deviceToken}},
		Logger:  quiet(), PollInterval: 10 * time.Millisecond,
		Heartbeat: 50 * time.Millisecond,
		RevocationChecker: func(token string) (bool, error) {
			return token == deviceToken && revoked.Load(), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	header := http.Header{}
	header.Set("Authorization", "Bearer "+deviceToken)
	conn, _, err := websocket.Dial(ctx,
		strings.Replace(ts.URL, "http://", "ws://", 1)+"/v1/devices/"+deviceID+"/stream",
		&websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	hello, err := protocol.Encode(protocol.MsgHello, time.Now().UTC(), protocol.Hello{
		DeviceID: deviceID, ClientVersion: "test", SchemaMajor: protocol.SchemaMajor,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Write(ctx, websocket.MessageText, hello); err != nil {
		t.Fatal(err)
	}

	revoked.Store(true)
	resp := get(t, ts, "/v1/devices/"+deviceID+"/snapshot", deviceToken, "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoked request status = %d, want 401", resp.StatusCode)
	}
	if _, _, err := conn.Read(ctx); err == nil {
		t.Fatal("existing stream remained open after token revocation")
	}
}

// E-A007 (Test-Module-002 E013): before the daemon has collected any metrics,
// the stream sends heartbeats but no persistable empty snapshot. The first
// successful metric batch then produces the initial full snapshot.
func TestStreamWaitsForDataBeforeFirstSnapshot(t *testing.T) {
	ts, store := newServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	header := http.Header{}
	header.Set("Authorization", "Bearer "+deviceToken)
	conn, _, err := websocket.Dial(ctx,
		strings.Replace(ts.URL, "http://", "ws://", 1)+"/v1/devices/"+deviceID+"/stream",
		&websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()

	hello, err := protocol.Encode(protocol.MsgHello, time.Now().UTC(), protocol.Hello{
		DeviceID: deviceID, ClientVersion: "test", SchemaMajor: protocol.SchemaMajor,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Write(ctx, websocket.MessageText, hello); err != nil {
		t.Fatal(err)
	}

	_, raw, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	env, err := protocol.DecodeEnvelope(raw)
	if err != nil {
		t.Fatal(err)
	}
	if env.Type != protocol.MsgHeartbeat {
		t.Fatalf("first empty-daemon message = %q, want heartbeat", env.Type)
	}

	if _, err := store.ApplyMetrics(1, []protocol.ProviderMetric{metric("m1", "68")}); err != nil {
		t.Fatal(err)
	}
	_, raw, err = conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	env, err = protocol.DecodeEnvelope(raw)
	if err != nil {
		t.Fatal(err)
	}
	if env.Type != protocol.MsgSnapshotFull {
		t.Fatalf("first data message = %q, want snapshot_full", env.Type)
	}
}
