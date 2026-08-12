// Package e2e exercises the full Phase 1 vertical slice in one process:
// mock fixture -> scheduler -> current state -> LAN API -> sync client ->
// last-known-good snapshot -> 60x20 screen.
//
// No real provider key, no network beyond loopback and no Raspberry Pi is
// involved, which is exactly the P1-03 acceptance condition.
package e2e

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/galendai/homepi-mon/internal/connector/mock"
	"github.com/galendai/homepi-mon/internal/nodeapi"
	"github.com/galendai/homepi-mon/internal/protocol"
	"github.com/galendai/homepi-mon/internal/scheduler"
	"github.com/galendai/homepi-mon/internal/snapstore"
	"github.com/galendai/homepi-mon/internal/state"
	"github.com/galendai/homepi-mon/internal/syncclient"
	"github.com/galendai/homepi-mon/internal/ui"
)

const (
	nodeID      = "dev-mac"
	nodeLabel   = "DEV-MAC"
	deviceID    = "pi-kiosk"
	deviceToken = "e2e-device-token-0123456789"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// fixtureJSON is the mock data file the operator edits during manual
// acceptance. It mirrors the UI-001 4 reference screen.
func fixtureJSON(codexPercent string) string {
	return `{
  "metrics": [
    {"id":"codex.5h","provider":"codex","display_name":"Codex","group":"coding","order":10,
     "metric_kind":"quota","value":"` + codexPercent + `","limit":"100","unit":"percent",
     "window":"rolling_5h","resets_in":"2h","precision":"verified","source_kind":"compatibility_api"},
    {"id":"codex.week","provider":"codex","display_name":"Codex","group":"coding","order":10,
     "metric_kind":"quota","value":"67","limit":"100","unit":"percent",
     "window":"weekly","precision":"verified","source_kind":"compatibility_api"},

    {"id":"minimax.5h","provider":"minimax","display_name":"MiniMax","group":"coding","order":20,
     "metric_kind":"quota","value":"68","limit":"100","unit":"percent",
     "window":"rolling_5h","resets_in":"2h","precision":"exact","source_kind":"official_api"},

    {"id":"kimi.coding.5h","provider":"kimi-coding","display_name":"Kimi Code","group":"coding","order":30,
     "metric_kind":"quota","value":"35","limit":"100","unit":"percent",
     "window":"rolling_5h","resets_in":"4h","precision":"verified","source_kind":"compatibility_api"},
    {"id":"kimi.coding.week","provider":"kimi-coding","display_name":"Kimi Code","group":"coding","order":30,
     "metric_kind":"quota","value":"51","limit":"100","unit":"percent",
     "window":"weekly","precision":"verified","source_kind":"compatibility_api"},

    {"id":"deepseek.balance","provider":"deepseek","display_name":"DeepSeek API","group":"api","order":40,
     "metric_kind":"balance","value":"8.20","unit":"CNY",
     "window":"prepaid","precision":"exact","source_kind":"official_api"},
    {"id":"kimi.api.balance","provider":"kimi-api","display_name":"Kimi API","group":"api","order":50,
     "metric_kind":"balance","value":"49.58","unit":"CNY",
     "window":"prepaid","precision":"exact","source_kind":"official_api"}
  ],
  "connectors": [
    {"id":"mock-codex","provider":"codex","state":"ok"},
    {"id":"mock-minimax","provider":"minimax","state":"ok"}
  ]
}`
}

// rig is one complete daemon plus one display, wired over loopback HTTP.
type rig struct {
	fixturePath string
	dataDir     string
	store       *state.Current
	baseURL     string
	client      *syncclient.Client
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	stopOnce    sync.Once

	httpSrv *http.Server
	conns   *trackingListener
}

// stopDaemon takes the daemon down hard, closing the listener and every
// connection including hijacked WebSocket streams.
//
// http.Server.Close explicitly does not touch hijacked connections, so a test
// that only called it would leave the display happily reading from a dead
// daemon until its own read deadline expired.
func (r *rig) stopDaemon() {
	_ = r.httpSrv.Close()
	r.conns.closeAll()
}

func (r *rig) stop() {
	r.stopOnce.Do(func() {
		r.cancel()
		_ = r.httpSrv.Close()
		r.conns.closeAll()
		r.wg.Wait()
	})
}

// trackingListener remembers every accepted connection so the test can drop
// them all at once.
type trackingListener struct {
	net.Listener
	mu    sync.Mutex
	conns []net.Conn
}

func (l *trackingListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	l.mu.Lock()
	l.conns = append(l.conns, c)
	l.mu.Unlock()
	return c, nil
}

func (l *trackingListener) closeAll() {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, c := range l.conns {
		_ = c.Close()
	}
	l.conns = nil
}

func newRig(t *testing.T, fixture string) *rig {
	t.Helper()

	dir := t.TempDir()
	fixturePath := filepath.Join(dir, "mock.json")
	if err := os.WriteFile(fixturePath, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}

	// --- daemon side ---
	store, err := state.New(state.Options{NodeID: nodeID, NodeLabel: nodeLabel, Epoch: "epoch-e2e"})
	if err != nil {
		t.Fatal(err)
	}
	conn, err := mock.New(mock.Options{ID: "mock", Path: fixturePath})
	if err != nil {
		t.Fatal(err)
	}
	policy := scheduler.DefaultPolicy(30 * time.Millisecond)
	policy.JitterFraction = 0
	policy.MinBackoff = 10 * time.Millisecond

	sched := scheduler.New([]scheduler.Task{
		{Connector: conn, Policy: policy, Timeout: time.Second, Enabled: true},
	}, scheduler.Options{Store: store, NodeLabel: nodeLabel, Logger: quiet()})
	if err := sched.ValidateAll(); err != nil {
		t.Fatalf("config validation: %v", err)
	}

	srv, err := nodeapi.New(nodeapi.Options{
		Store:        store,
		Devices:      []nodeapi.Device{{ID: deviceID, Token: deviceToken}},
		Logger:       quiet(),
		PollInterval: 10 * time.Millisecond,
		Heartbeat:    100 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	tracked := &trackingListener{Listener: ln}
	httpSrv := &http.Server{Handler: srv.Handler()}
	go func() { _ = httpSrv.Serve(tracked) }()
	baseURL := "http://" + ln.Addr().String()

	// --- display side ---
	dataDir := filepath.Join(dir, "display")
	binding := protocol.SourceBinding{NodeID: nodeID}
	sstore, err := snapstore.New(dataDir, binding)
	if err != nil {
		t.Fatal(err)
	}
	client, err := syncclient.New(syncclient.Options{
		BaseURL: baseURL, DeviceID: deviceID, Token: deviceToken,
		Binding: binding, Store: sstore,
		ClientVersion: "test", Logger: quiet(),
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	r := &rig{
		fixturePath: fixturePath, dataDir: dataDir, store: store,
		baseURL: baseURL, client: client, cancel: cancel,
		httpSrv: httpSrv, conns: tracked,
	}
	r.wg.Add(2)
	go func() {
		defer r.wg.Done()
		sched.Run(ctx)
	}()
	go func() {
		defer r.wg.Done()
		client.Run(ctx)
	}()
	t.Cleanup(r.stop)
	return r
}

// waitFor polls cond until it holds or the deadline passes.
func waitFor(t *testing.T, what string, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func (r *rig) screen(now time.Time) []string {
	return ui.Render(ui.Build(r.client.Snapshot(), ui.BuildOptions{
		Page:              "CODING",
		Now:               now,
		Connected:         r.client.Connected(),
		Thresholds:        protocol.DefaultThresholds(),
		Pi:                ui.PiHealth{TempC: 52, CPUPercent: 3, MemPercent: 18, LANStatus: protocol.DisplayOK, Known: true},
		Version:           "V0.1.0",
		FallbackNodeLabel: nodeLabel,
	}))
}

func (r *rig) liveSnapshot() bool {
	snap := r.client.Snapshot()
	return snap != nil && len(snap.Metrics) > 0 && r.client.Connected()
}

func contains(lines []string, want string) bool {
	for _, l := range lines {
		if strings.Contains(l, want) {
			return true
		}
	}
	return false
}

// E-E001 (Development-Plan P1-03): mock data reaches the screen end to end,
// and editing the fixture changes the screen.
func TestMockDataFlowsToScreen(t *testing.T) {
	r := newRig(t, fixtureJSON("42"))

	waitFor(t, "first live snapshot", 5*time.Second, func() bool {
		return r.liveSnapshot() && contains(r.screen(time.Now()), "42% LEFT")
	})

	screen := r.screen(time.Now())
	for _, want := range []string{"HOMEPI", "DEV-MAC", "CODING", "LIVE",
		"Codex", "MiniMax", "Kimi Code", "API BALANCE", "CNY 8.20", "CNY 49.58"} {
		if !contains(screen, want) {
			t.Errorf("screen missing %q:\n%s", want, strings.Join(screen, "\n"))
		}
	}

	// The operator edits the mock file; the change must appear without a
	// restart of either process.
	beforeVersion := r.client.Snapshot().SnapshotVersion
	if err := os.WriteFile(r.fixturePath, []byte(fixtureJSON("7")), 0o600); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "edited value on screen", 5*time.Second, func() bool {
		snap := r.client.Snapshot()
		return snap != nil && snap.SnapshotVersion > beforeVersion && r.client.Connected() &&
			contains(r.screen(time.Now()), "] 7% LEFT")
	})

	screen = r.screen(time.Now())
	if !contains(screen, "CRIT") {
		t.Errorf("7%% remaining must raise CRIT:\n%s", strings.Join(screen, "\n"))
	}
	if !contains(screen, "CODING       CRIT") {
		t.Errorf("header must escalate to CRIT:\n%s", strings.Join(screen, "\n"))
	}
}

// E-E002 (Development-Plan P1-03, Test-Module-002 E002/E003): losing the
// daemon keeps the last values on screen, marks them stale and shows OFFLINE.
func TestDaemonOfflineKeepsLastKnownGood(t *testing.T) {
	r := newRig(t, fixtureJSON("42"))
	waitFor(t, "first live snapshot", 5*time.Second, func() bool {
		return r.liveSnapshot() && contains(r.screen(time.Now()), "42% LEFT")
	})

	r.stopDaemon()
	waitFor(t, "client to notice the outage", 5*time.Second, func() bool {
		return !r.client.Connected()
	})

	screen := r.screen(time.Now())
	if !contains(screen, "OFFLINE") {
		t.Errorf("header must show OFFLINE:\n%s", strings.Join(screen, "\n"))
	}
	if !contains(screen, "42% LEFT") {
		t.Errorf("last known values must stay on screen:\n%s", strings.Join(screen, "\n"))
	}
	if contains(screen, "0% LEFT") {
		t.Error("offline must not display fabricated zeros")
	}
	if !contains(screen, "DATA STALE") {
		t.Errorf("footer must state staleness:\n%s", strings.Join(screen, "\n"))
	}

	// Metrics also age into STALE once past their freshness budget.
	future := time.Now().Add(10 * time.Minute)
	if !contains(r.screen(future), "STALE") {
		t.Errorf("cards must age into STALE:\n%s", strings.Join(r.screen(future), "\n"))
	}
}

// E-E003 (Development-Plan P1-03, Test-Module-002 E010): a restarted display
// renders from disk before the network is up, and the data directory never
// holds more than one snapshot.
func TestRestartRendersFromDiskWithNoHistory(t *testing.T) {
	r := newRig(t, fixtureJSON("42"))
	waitFor(t, "snapshot persisted", 3*time.Second, func() bool {
		_, err := os.Stat(filepath.Join(r.dataDir, snapstore.FileName))
		return err == nil
	})
	// Let several more collections land so any history would accumulate.
	time.Sleep(200 * time.Millisecond)
	r.stop()

	entries, err := os.ReadDir(r.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != snapstore.FileName {
		names := []string{}
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("data dir holds %v, want exactly one %s", names, snapstore.FileName)
	}

	// Restart the display against an unreachable daemon.
	binding := protocol.SourceBinding{NodeID: nodeID}
	sstore, err := snapstore.New(r.dataDir, binding)
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := syncclient.New(syncclient.Options{
		BaseURL: "http://127.0.0.1:1", DeviceID: deviceID, Token: deviceToken,
		Binding: binding, Store: sstore, ClientVersion: "test", Logger: quiet(),
	})
	if err != nil {
		t.Fatal(err)
	}
	snap := restarted.Snapshot()
	if snap == nil {
		t.Fatal("restarted display did not load the last-known-good snapshot")
	}

	screen := ui.Render(ui.Build(snap, ui.BuildOptions{
		Page: "CODING", Now: time.Now(), Connected: false,
		Thresholds:        protocol.DefaultThresholds(),
		Pi:                ui.PiHealth{Known: false, LANStatus: protocol.DisplayError},
		Version:           "V0.1.0",
		FallbackNodeLabel: nodeLabel,
	}))
	if !contains(screen, "42% LEFT") {
		t.Errorf("offline cold start did not render cached data:\n%s", strings.Join(screen, "\n"))
	}
	if !contains(screen, "OFFLINE") {
		t.Errorf("offline cold start must show OFFLINE:\n%s", strings.Join(screen, "\n"))
	}
}

// E-E004 (Development-Plan P1-03, UI-001 5.2): a connector in blocked_auth
// renders the AUTH card with the official-CLI action and no stale value.
func TestAuthStateRendersOfficialCLIAction(t *testing.T) {
	fixture := strings.Replace(fixtureJSON("42"),
		`"window":"rolling_5h","resets_in":"2h","precision":"verified","source_kind":"compatibility_api"},
    {"id":"codex.week"`,
		`"window":"rolling_5h","resets_in":"2h","precision":"verified","source_kind":"compatibility_api",
     "error_class":"auth"},
    {"id":"codex.week"`, 1)
	if fixture == fixtureJSON("42") {
		t.Fatal("fixture patch did not apply")
	}

	r := newRig(t, fixture)
	waitFor(t, "live auth card", 5*time.Second, func() bool {
		return r.liveSnapshot() && contains(r.screen(time.Now()), "AUTH")
	})

	screen := r.screen(time.Now())
	if !contains(screen, "OFFICIAL CLI") {
		t.Errorf("AUTH card must name the official CLI:\n%s", strings.Join(screen, "\n"))
	}
	if !contains(screen, "-- LEFT") {
		t.Errorf("AUTH card must not show a value as if it were live:\n%s", strings.Join(screen, "\n"))
	}
	// The other providers keep updating (MOD-001 13).
	if !contains(screen, "68% LEFT") {
		t.Errorf("unaffected connectors must keep updating:\n%s", strings.Join(screen, "\n"))
	}
}

// E-E005 (Test-Module-002 U016): a snapshot from an unbound node is rejected
// rather than merged.
func TestForeignSnapshotIsRejected(t *testing.T) {
	r := newRig(t, fixtureJSON("42"))
	waitFor(t, "first live snapshot", 5*time.Second, func() bool {
		return r.liveSnapshot()
	})

	binding := protocol.SourceBinding{NodeID: nodeID}
	foreign := r.client.Snapshot().Clone()
	foreign.SourceNode = "other-pc"
	if err := binding.Accept(foreign); err == nil {
		t.Fatal("binding accepted a foreign node")
	}

	sstore, err := snapstore.New(filepath.Join(t.TempDir(), "foreign"), binding)
	if err != nil {
		t.Fatal(err)
	}
	if err := sstore.Save(foreign); err == nil {
		t.Fatal("store accepted a foreign-node snapshot")
	}
}

// S-E001 (Test-Module-001 S001/E012): nothing that crosses the LAN or lands on
// the display's disk contains a credential-shaped field.
func TestNoSecretsCrossTheWireOrHitDisk(t *testing.T) {
	r := newRig(t, fixtureJSON("42"))
	waitFor(t, "snapshot persisted", 3*time.Second, func() bool {
		_, err := os.Stat(filepath.Join(r.dataDir, snapstore.FileName))
		return err == nil
	})

	onDisk, err := os.ReadFile(filepath.Join(r.dataDir, snapstore.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if hits := protocol.AuditJSON(onDisk, []string{deviceToken}); len(hits) != 0 {
		t.Errorf("persisted snapshot leaked: %v", hits)
	}

	overWire, err := json.Marshal(r.store.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if hits := protocol.AuditJSON(overWire, []string{deviceToken}); len(hits) != 0 {
		t.Errorf("published snapshot leaked: %v", hits)
	}
}

// E-E007 (UI-001 5.1 vs 5.4): when the daemon is gone and the cached data is
// critical, the header must still read OFFLINE.
//
// This is the case that hides an outage: if CRIT won, the footer would print
// the alert count instead of DATA STALE / RETRYING and the operator would
// chase a quota that is merely a stale reading.
func TestOfflineOutranksCriticalInHeader(t *testing.T) {
	r := newRig(t, fixtureJSON("4"))
	waitFor(t, "live critical snapshot", 5*time.Second, func() bool {
		return r.liveSnapshot() && contains(r.screen(time.Now()), "CODING       CRIT")
	})
	if !contains(r.screen(time.Now()), "CODING       CRIT") {
		t.Fatal("test premise broken: header should be CRIT while connected")
	}

	r.stopDaemon()
	waitFor(t, "client to notice the outage", 5*time.Second, func() bool {
		return !r.client.Connected()
	})

	screen := r.screen(time.Now())
	if !contains(screen, "CODING       OFFLINE") {
		t.Errorf("header must read OFFLINE, not CRIT:\n%s", strings.Join(screen, "\n"))
	}
	if !contains(screen, "DATA STALE") {
		t.Errorf("footer must report staleness:\n%s", strings.Join(screen, "\n"))
	}
	if !contains(screen, "RETRYING") {
		t.Errorf("footer must show the reconnect state:\n%s", strings.Join(screen, "\n"))
	}
	// The per-card severity is still visible, so nothing is hidden.
	if !contains(screen, "CRIT") {
		t.Errorf("card severity must remain visible:\n%s", strings.Join(screen, "\n"))
	}
}

// E-E006 (Test-Module-001 E007): a daemon restart yields a new epoch whose
// version counter starts over, and the display accepts it without treating it
// as a regression.
func TestDaemonRestartIsAcceptedAsNewEpoch(t *testing.T) {
	r := newRig(t, fixtureJSON("42"))
	waitFor(t, "first live snapshot", 5*time.Second, func() bool {
		return r.liveSnapshot()
	})
	before := r.client.Snapshot()

	restarted, err := state.New(state.Options{NodeID: nodeID, NodeLabel: nodeLabel, Epoch: "epoch-e2e-2"})
	if err != nil {
		t.Fatal(err)
	}
	after := restarted.Snapshot()

	if after.SnapshotVersion > before.SnapshotVersion {
		t.Fatalf("test premise broken: restarted version %d should be lower", after.SnapshotVersion)
	}
	ok, err := protocol.Supersedes(before, after)
	if !ok || err != nil {
		t.Fatalf("restarted daemon rejected: ok=%v err=%v", ok, err)
	}
}

// E-E008 (Test-Module-002 E013): an empty new daemon may establish the stream
// but must not replace a display's persisted last-known-good snapshot. The new
// epoch takes over only after it has real metric data.
func TestEmptyRestartDoesNotOverwriteLastKnownGood(t *testing.T) {
	dir := t.TempDir()
	binding := protocol.SourceBinding{NodeID: nodeID}
	sstore, err := snapstore.New(filepath.Join(dir, "display"), binding)
	if err != nil {
		t.Fatal(err)
	}

	oldState, err := state.New(state.Options{NodeID: nodeID, NodeLabel: nodeLabel, Epoch: "epoch-old"})
	if err != nil {
		t.Fatal(err)
	}
	oldMetricFixture := fixtureJSON("42")
	fixturePath := filepath.Join(dir, "old.json")
	if err := os.WriteFile(fixturePath, []byte(oldMetricFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	oldConnector, err := mock.New(mock.Options{ID: "mock", Path: fixturePath})
	if err != nil {
		t.Fatal(err)
	}
	oldMetrics, err := oldConnector.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := oldState.ApplyMetrics(1, oldMetrics); err != nil {
		t.Fatal(err)
	}
	if err := sstore.Save(oldState.Snapshot()); err != nil {
		t.Fatal(err)
	}

	newState, err := state.New(state.Options{NodeID: nodeID, NodeLabel: nodeLabel, Epoch: "epoch-new"})
	if err != nil {
		t.Fatal(err)
	}
	srv, err := nodeapi.New(nodeapi.Options{
		Store: newState, Devices: []nodeapi.Device{{ID: deviceID, Token: deviceToken}},
		Logger: quiet(), PollInterval: 5 * time.Millisecond, Heartbeat: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	httpSrv := &http.Server{Handler: srv.Handler()}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = httpSrv.Serve(ln) }()
	defer httpSrv.Close()

	client, err := syncclient.New(syncclient.Options{
		BaseURL: "http://" + ln.Addr().String(), DeviceID: deviceID, Token: deviceToken,
		Binding: binding, Store: sstore, ClientVersion: "test", Logger: quiet(),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		client.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		_ = httpSrv.Close()
		<-done
	})

	time.Sleep(75 * time.Millisecond)
	if client.Connected() {
		t.Fatal("empty restarted daemon must not mark cached data as live")
	}
	if got := client.Snapshot(); got == nil || got.SourceEpoch != "epoch-old" || len(got.Metrics) == 0 {
		t.Fatalf("empty restart replaced in-memory snapshot: %+v", got)
	}
	onDisk, err := sstore.Load()
	if err != nil {
		t.Fatal(err)
	}
	if onDisk.SourceEpoch != "epoch-old" || len(onDisk.Metrics) == 0 {
		t.Fatalf("empty restart replaced on-disk snapshot: %+v", onDisk)
	}

	if _, err := newState.ApplyMetrics(1, oldMetrics); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "first real snapshot from restarted daemon", time.Second, func() bool {
		got := client.Snapshot()
		return got != nil && got.SourceEpoch == "epoch-new" && len(got.Metrics) > 0
	})
}

// E016 (Test-Module-002): if a reconnect attempt fails while the daemon is
// down, the same display process keeps retrying and accepts the recovered
// daemon's new epoch without discarding its last-known-good snapshot.
func TestDisplayReconnectsAfterDialFailureAndDaemonRecovery(t *testing.T) {
	dir := t.TempDir()
	fixturePath := filepath.Join(dir, "recovery.json")
	if err := os.WriteFile(fixturePath, []byte(fixtureJSON("42")), 0o600); err != nil {
		t.Fatal(err)
	}
	conn, err := mock.New(mock.Options{ID: "mock", Path: fixturePath})
	if err != nil {
		t.Fatal(err)
	}
	metrics, err := conn.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	recoveredStore, err := state.New(state.Options{NodeID: nodeID, NodeLabel: nodeLabel, Epoch: "epoch-recovered"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recoveredStore.ApplyMetrics(1, metrics); err != nil {
		t.Fatal(err)
	}

	// Reserve an address and close it so the client's first dial receives a
	// real connection error before the daemon starts on the same address.
	reserved, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := reserved.Addr().String()
	if err := reserved.Close(); err != nil {
		t.Fatal(err)
	}

	binding := protocol.SourceBinding{NodeID: nodeID}
	sstore, err := snapstore.New(filepath.Join(dir, "display"), binding)
	if err != nil {
		t.Fatal(err)
	}
	oldStore, err := state.New(state.Options{NodeID: nodeID, NodeLabel: nodeLabel, Epoch: "epoch-before-outage"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := oldStore.ApplyMetrics(1, metrics); err != nil {
		t.Fatal(err)
	}
	if err := sstore.Save(oldStore.Snapshot()); err != nil {
		t.Fatal(err)
	}
	client, err := syncclient.New(syncclient.Options{
		BaseURL: "http://" + addr, DeviceID: deviceID, Token: deviceToken,
		Binding: binding, Store: sstore, ClientVersion: "test", Logger: quiet(),
		Rand: func() float64 { return 0.5 },
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		client.Run(ctx)
	}()

	waitFor(t, "initial failed dial", time.Second, func() bool {
		return client.RetryIn() > 0
	})
	if snap := client.Snapshot(); snap == nil || snap.SourceEpoch != "epoch-before-outage" {
		t.Fatalf("failed dial replaced last-known-good snapshot: %+v", snap)
	}
	onDiskBeforeRecovery, err := sstore.Load()
	if err != nil {
		t.Fatal(err)
	}
	if onDiskBeforeRecovery.SourceEpoch != "epoch-before-outage" {
		t.Fatalf("failed dial replaced persisted epoch: %q", onDiskBeforeRecovery.SourceEpoch)
	}

	srv, err := nodeapi.New(nodeapi.Options{
		Store: recoveredStore, Devices: []nodeapi.Device{{ID: deviceID, Token: deviceToken}},
		Logger: quiet(), PollInterval: 5 * time.Millisecond, Heartbeat: 20 * time.Millisecond,
	})
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp4", addr)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	httpSrv := &http.Server{Handler: srv.Handler()}
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		_ = httpSrv.Serve(listener)
	}()
	t.Cleanup(func() {
		cancel()
		_ = httpSrv.Close()
		<-clientDone
		<-serverDone
	})

	waitFor(t, "display to reconnect to recovered daemon", 5*time.Second, func() bool {
		snap := client.Snapshot()
		return client.Connected() && snap != nil && snap.SourceEpoch == "epoch-recovered"
	})
	onDisk, err := sstore.Load()
	if err != nil {
		t.Fatal(err)
	}
	if onDisk.SourceEpoch != "epoch-recovered" {
		t.Fatalf("persisted epoch = %q, want epoch-recovered", onDisk.SourceEpoch)
	}
}
