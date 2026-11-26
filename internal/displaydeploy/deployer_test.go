package displaydeploy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/galendai/homepi-mon/internal/config"
	"github.com/galendai/homepi-mon/internal/displayconfig"
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
	scripts       []string
	inputs        [][]byte
	failApply     bool
	version       uint64
	unhealthy     bool
	rollbackFails bool
	probeFails    bool
	capability    bool
	extraProbe    string
	env           displayconfig.Environment
	previous      displayconfig.Environment
	file          []byte
	previousFile  []byte
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
		if r.probeFails {
			return nil, errors.New("SSH unavailable")
		}
		var details strings.Builder
		fmt.Fprintf(&details, "file_digest=%x\n", sha256.Sum256(r.file))
		if r.capability {
			details.WriteString("rotation_env=yes\nrenderer=gui\n")
		}
		for _, prefix := range []string{"file_", "process_"} {
			fmt.Fprintf(&details, "%sstyle=%s\n", prefix, r.env.Style)
			for name, v := range map[string]string{"pages": r.env.PageSubset, "page-only": r.env.PageLock, "home-only": r.env.HomeOnly, "page-order": r.env.PageOrder, "page-dwell-seconds": r.env.PageDwellSeconds} {
				if v != "" {
					fmt.Fprintf(&details, "%s%s=%s\n", prefix, name, v)
				}
			}
		}
		return []byte(fmt.Sprintf("active=active\nrestarts=0\ndevice_id=pi-kiosk\nnode_url_hash=780826007f8e25dc3aec7ee2d18ad5d231826c756712214def4c9155cf29b015\nbinary=homepi-display_0.1.0\nepoch=e1\nversion=%d\ngenerated=2026-08-12T00:00:00Z\nprocess_verified=yes\n", version) + details.String() + r.extraProbe), nil
	case validateScript:
		return []byte("validated\n"), nil
	case applyScript:
		if r.failApply {
			return nil, errors.New("apply failed")
		}
		expected, raw, ok := bytes.Cut(input, []byte("\n"))
		if !ok || string(expected) != fmt.Sprintf("%x", sha256.Sum256(r.file)) {
			return []byte("environment_changed\n"), nil
		}
		env, err := displayconfig.Parse(bytes.NewReader(raw))
		if err != nil {
			return nil, err
		}
		r.previous, r.env = r.env, env
		r.previousFile, r.file = r.file, raw
		return []byte("applied\n"), nil
	case rollbackScript:
		if r.rollbackFails {
			return nil, errors.New("synthetic recovery failure")
		}
		r.env, r.unhealthy = r.previous, false
		r.file = r.previousFile
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
	if !slices.Contains(runner.scripts, rollbackScript) || runner.scripts[len(runner.scripts)-1] != probeScript {
		t.Fatal("rollback script was not executed")
	}
}

func TestParseProbeIsRedactedAndStrict(t *testing.T) {
	digest := strings.Repeat("a1", 32)
	s, err := parseProbe([]byte("file_digest=" + digest + "\nactive=active\nrestarts=0\nfile_style=rich\ndevice_id=pi-kiosk\nnode_url_hash=780826007f8e25dc3aec7ee2d18ad5d231826c756712214def4c9155cf29b015\nbinary=homepi-display_0.1.0\nepoch=e\nversion=3\ngenerated=t\n"))
	if err != nil || !s.Connected || s.SnapshotVersion != 3 {
		t.Fatalf("probe = %+v, %v", s, err)
	}
	if _, err := parseProbe([]byte("active=active\n")); err == nil {
		t.Fatal("accepted incomplete probe")
	}
	wire, err := json.Marshal(s)
	if err != nil || bytes.Contains(wire, []byte(digest)) {
		t.Fatal("the private file digest was exposed")
	}
}

func TestApplyRejectsEnvironmentEditedAfterTest(t *testing.T) {
	m, r, _ := newTestManager(t)
	if _, err := m.Test(t.Context()); err != nil {
		t.Fatal(err)
	}
	r.file = []byte("# operator revision\n")
	state, err := m.State(t.Context())
	if err != nil || state.Tested {
		t.Fatalf("unexpected test status after file change: tested=%v err=%v", state.Tested, err)
	}
	if _, err := m.Apply(t.Context()); !errors.Is(err, ErrEnvironmentChanged) || slices.Contains(r.scripts, applyScript) {
		t.Fatalf("apply after external edit: %v", err)
	}
}

func TestApplyRequiresRetestAfterCredentialRotation(t *testing.T) {
	m, r, _ := newTestManager(t)
	if _, err := m.Test(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := m.store.Set(t.Context(), "keyring:device:pi-kiosk", "rotated-fixture"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Apply(t.Context()); err == nil || !strings.Contains(err.Error(), "must pass test") || slices.Contains(r.scripts, applyScript) {
		t.Fatalf("apply after credential change: %v", err)
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
	runner := &testRunner{env: displayconfig.Environment{Style: "ascii"}}
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

func TestProbeSeparatesFileFromProcessAndUsesDefaults(t *testing.T) {
	m, r, _ := newTestManager(t)
	p := Edit{SSHHost: "dietpi", DeviceID: "pi-kiosk", NodeURL: "https://192.168.31.107:8443", Style: "ascii", DataDir: displayconfig.DefaultDataDir}
	r.extraProbe = "process_home-only=false\nflag_pages=HOME,CODING,API,OPENCLASH\nflag_style=rich\ndrop_in=split-card-pages.conf\n"
	state, err := m.Edit(t.Context(), p)
	if err != nil || !state.Remote.Verified || len(state.Differences) != 0 {
		t.Fatalf("legacy file diff=%v err=%v", state.Differences, err)
	}
	remote := state.Remote
	if remote.HomeOnly != "false" || remote.Style != "ascii" || remote.PageSubset != "" || remote.Startup == nil || remote.Startup.PageSubset != "HOME,CODING,API,OPENCLASH" || remote.Startup.Style != "rich" || remote.startupStep() != "startup_overridden" {
		t.Fatalf("remote=%+v", remote)
	}
	r.extraProbe = "process_verified=no\n"
	state, err = m.State(t.Context())
	if err != nil || !state.Remote.Verified || state.Remote.Startup != nil {
		t.Fatal("unreadable process should preserve file evidence")
	}
	r.extraProbe = "process_pages=HOME,API\n"
	state, err = m.State(t.Context())
	if err != nil || state.Remote.StartupState != "startup_unverified" {
		t.Fatal("an unidentified running binary cannot confirm support for the new environment mode")
	}
	r.probeFails = true
	state, err = m.State(t.Context())
	if err != nil || state.Remote.Verified || state.Remote.Note == "" || len(state.Differences) > 0 || state.Tested {
		t.Fatalf("SSH failure state=%+v error=%v", state, err)
	}
}

func TestApplyCustomModeAcrossDisplayVersions(t *testing.T) {
	m, r, dir := newTestManager(t)
	desired := Edit{SSHHost: "dietpi", DeviceID: "pi-kiosk", NodeURL: "https://192.168.31.107:8443", Style: "ascii", DataDir: displayconfig.DefaultDataDir, PageSubset: "api,home,openclash"}
	state, err := m.Edit(t.Context(), desired)
	if err != nil || !slices.Contains(state.Differences, "page_subset") {
		t.Fatalf("edit=%+v err=%v", state, err)
	}
	if _, err = m.Test(t.Context()); err == nil || !strings.Contains(err.Error(), "update homepi-display") || slices.Contains(r.scripts, validateScript) {
		t.Fatalf("old target: %v", err)
	}
	r.capability = true
	test, err := m.Test(t.Context())
	if err != nil || test.Revision != state.Revision {
		t.Fatalf("candidate test: %v", err)
	}
	result, err := m.Apply(t.Context())
	if err != nil || result.Status != "applied" || !slices.Contains(result.Steps, "startup_confirmed") || result.Remote.Startup.PageSubset != "API,HOME,OPENCLASH" {
		t.Fatalf("apply=%+v err=%v", result, err)
	}
	restored, err := New(m.configPath, dir, m.store, r)
	if err != nil {
		t.Fatal(err)
	}
	state, err = restored.State(t.Context())
	if err != nil || state.Profile.PageSubset != "API,HOME,OPENCLASH" || state.Revision != result.Revision || state.TargetDeviceID != "pi-kiosk" || len(state.Differences) != 0 {
		t.Fatalf("reopen=%+v err=%v", state, err)
	}
	desired.PageSubset, desired.PageLock = "", "API"
	r.extraProbe = "flag_pages=HOME,CODING,API,OPENCLASH\n"
	if _, err = restored.Edit(t.Context(), desired); err != nil {
		t.Fatal(err)
	}
	if _, err = restored.Test(t.Context()); err != nil {
		t.Fatal(err)
	}
	result, err = restored.Apply(t.Context())
	if err != nil || result.Remote.PageLock != "API" || result.Remote.Startup.PageLock != "" || !slices.Contains(result.Steps, "startup_overridden") {
		t.Fatalf("override=%+v err=%v", result, err)
	}
}

func TestUnconfirmedRollbackRequiresRecovery(t *testing.T) {
	m, r, _ := newTestManager(t)
	_, err := m.Edit(t.Context(), Edit{SSHHost: "dietpi", DeviceID: "pi-kiosk", NodeURL: "https://192.168.31.107:8443", Style: "ascii", DataDir: displayconfig.DefaultDataDir})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Test(t.Context()); err != nil {
		t.Fatal(err)
	}
	r.unhealthy, r.rollbackFails = true, true
	clock := time.Now()
	m.now = func() time.Time { clock = clock.Add(30 * time.Second); return clock }
	result, err := m.Apply(t.Context())
	if err == nil || result.RolledBack || result.Status != "manual_recovery_required" {
		t.Fatalf("recovery=%+v err=%v", result, err)
	}
	if _, err = m.Apply(t.Context()); err == nil {
		t.Fatal("retry accepted without retesting")
	}
}
