package displaydeploy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/galendai/homepi-mon/internal/config"
	"github.com/galendai/homepi-mon/internal/secretstore"
	"github.com/galendai/homepi-mon/internal/tlsconfig"
)

type testStore struct{ values map[string]string }

func (s *testStore) Get(_ context.Context, ref string) (string, error) {
	v, ok := s.values[ref]
	if !ok {
		return "", secretstore.ErrNotFound
	}
	return v, nil
}
func (s *testStore) Set(_ context.Context, ref, value string) error {
	s.values[ref] = value
	return nil
}
func (s *testStore) Delete(_ context.Context, ref string) error { delete(s.values, ref); return nil }
func (s *testStore) Backend() string                            { return "test" }

type testRunner struct {
	scripts   []string
	inputs    [][]byte
	failApply bool
	version   uint64
	unhealthy bool
}

func (r *testRunner) Run(_ context.Context, host, script string, input []byte) ([]byte, error) {
	if host != "dietpi" {
		return nil, errors.New("unexpected host")
	}
	r.scripts = append(r.scripts, script)
	r.inputs = append(r.inputs, append([]byte(nil), input...))
	switch script {
	case probeScript:
		r.version++
		version := r.version
		if r.unhealthy {
			version = 1
		}
		return []byte(fmt.Sprintf("active=active\nrestarts=0\nstyle=ascii\ndevice_id=pi-kiosk\nnode_url_hash=780826007f8e25dc3aec7ee2d18ad5d231826c756712214def4c9155cf29b015\nbinary=homepi-display_0.1.0\nepoch=e1\nversion=%d\ngenerated=2026-08-12T00:00:00Z\n", version)), nil
	case validateScript:
		return []byte("validated\n"), nil
	case applyScript:
		if r.failApply {
			return nil, errors.New("apply failed")
		}
		return []byte("applied\n"), nil
	case rollbackScript:
		return []byte("rolled_back\n"), nil
	default:
		return nil, errors.New("unknown script")
	}
}

func TestManagerTestApplyPersistsOnlyReference(t *testing.T) {
	m, runner, dataDir := newTestManager(t)
	ctx := context.Background()
	state, err := m.Edit(ctx, Edit{SSHHost: "dietpi", DeviceID: "pi-kiosk", NodeURL: "https://192.168.31.107:8443", Style: "ascii", DataDir: "/var/lib/homepi-display"})
	if err != nil {
		t.Fatal(err)
	}
	if !state.TokenPresent || state.Tested {
		t.Fatalf("edit state = %+v", state)
	}
	if _, err := m.Apply(ctx); err == nil {
		t.Fatal("apply accepted untested candidate")
	}
	if _, err := m.Test(ctx); err != nil {
		t.Fatal(err)
	}
	result, err := m.Apply(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "applied" || result.Remote.Style != "ascii" {
		t.Fatalf("result = %+v", result)
	}
	raw, err := os.ReadFile(filepath.Join(dataDir, "display-profiles.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "device-secret") || !strings.Contains(string(raw), "keyring:device:pi-kiosk") {
		t.Fatalf("profile secret contract violated: %s", raw)
	}
	info, _ := os.Stat(filepath.Join(dataDir, "display-profiles.json"))
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("profile mode = %o", info.Mode().Perm())
	}
	if len(runner.inputs) < 3 || !bytesContain(runner.inputs, []byte("HOMEPI_DISPLAY_STYLE=ascii")) ||
		!bytesContain(runner.inputs, []byte("HOMEPI_PAGE_ORDER=CODING,API,HOMELAB,SERVICES,SYSTEM")) ||
		!bytesContain(runner.inputs, []byte("HOMEPI_PAGE_DWELL_SECONDS=CODING:15,API:15,HOMELAB:15,SERVICES:15,SYSTEM:15")) {
		t.Fatal("candidate environment was not sent via stdin")
	}
}

func TestManagerRejectsUnsafeHostAndInvalidStyle(t *testing.T) {
	m, _, _ := newTestManager(t)
	base := Edit{SSHHost: "dietpi", DeviceID: "pi-kiosk", NodeURL: "https://192.168.31.107:8443", Style: "rich", DataDir: "/var/lib/homepi-display"}
	for _, mutate := range []func(*Edit){
		func(e *Edit) { e.SSHHost = "-oProxyCommand=id" },
		func(e *Edit) { e.SSHHost = "dietpi;id" },
		func(e *Edit) { e.Style = "neon" },
		func(e *Edit) { e.DataDir = "/tmp" },
		func(e *Edit) { e.PageOrder = "CODING,API,HOMELAB,SERVICES,CODING" },
		func(e *Edit) { e.PageDwellSeconds = "CODING:4,API:15,HOMELAB:15,SERVICES:15,SYSTEM:15" },
	} {
		e := base
		mutate(&e)
		if _, err := m.Edit(context.Background(), e); err == nil {
			t.Fatalf("accepted invalid edit %+v", e)
		}
	}
}

func TestManagerRollsBackWhenSnapshotDoesNotAdvance(t *testing.T) {
	m, runner, _ := newTestManager(t)
	ctx := context.Background()
	edit := Edit{SSHHost: "dietpi", DeviceID: "pi-kiosk", NodeURL: "https://192.168.31.107:8443", Style: "ascii", DataDir: "/var/lib/homepi-display"}
	if _, err := m.Edit(ctx, edit); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Test(ctx); err != nil {
		t.Fatal(err)
	}
	runner.unhealthy = true
	clock := time.Now()
	m.now = func() time.Time {
		clock = clock.Add(30 * time.Second)
		return clock
	}
	result, err := m.Apply(ctx)
	if err == nil || result.Status != "rolled_back" || !result.RolledBack {
		t.Fatalf("result = %+v, err=%v", result, err)
	}
	if len(runner.scripts) == 0 || runner.scripts[len(runner.scripts)-1] != rollbackScript {
		t.Fatal("rollback script was not executed")
	}
}

func TestParseProbeIsRedactedAndStrict(t *testing.T) {
	s, err := parseProbe([]byte("active=active\nrestarts=0\nstyle=rich\nbinary=homepi-display_0.1.0\nepoch=e\nversion=3\ngenerated=t\n"))
	if err != nil || !s.Connected || s.SnapshotVersion != 3 {
		t.Fatalf("probe = %+v, %v", s, err)
	}
	if _, err := parseProbe([]byte("active=active\n")); err == nil {
		t.Fatal("accepted incomplete probe")
	}
}

func newTestManager(t *testing.T) (*Manager, *testRunner, string) {
	t.Helper()
	dataDir := t.TempDir()
	if _, _, err := tlsconfig.EnsureCert(dataDir, "dev-mac", 0); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{SchemaVersion: 1, SourceNode: config.SourceNodeConfig{ID: "dev-mac"}, Listen: config.ListenConfig{Addr: "192.168.31.107:8443"}, Devices: []config.DeviceConfig{{ID: "pi-kiosk", TokenRef: "keyring:device:pi-kiosk"}}}
	raw, _ := json.Marshal(cfg)
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &testRunner{}
	m, err := New(configPath, dataDir, &testStore{values: map[string]string{"keyring:device:pi-kiosk": "device-secret"}}, runner)
	if err != nil {
		t.Fatal(err)
	}
	return m, runner, dataDir
}

func bytesContain(values [][]byte, needle []byte) bool {
	for _, value := range values {
		if strings.Contains(string(value), string(needle)) {
			return true
		}
	}
	return false
}
