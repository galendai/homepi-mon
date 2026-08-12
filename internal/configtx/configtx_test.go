package configtx_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/galendai/homepi-mon/internal/config"
	"github.com/galendai/homepi-mon/internal/configtx"

	// Pull every connector so their init() registers the
	// connector.Factory and the providermeta.TypeMeta that this
	// package's tests depend on.
	_ "github.com/galendai/homepi-mon/internal/connector/codexusage"
	_ "github.com/galendai/homepi-mon/internal/connector/deepseek"
	_ "github.com/galendai/homepi-mon/internal/connector/kimiapi"
	_ "github.com/galendai/homepi-mon/internal/connector/kimicoding"
	_ "github.com/galendai/homepi-mon/internal/connector/minimax"
	_ "github.com/galendai/homepi-mon/internal/connector/mock"

	"github.com/galendai/homepi-mon/internal/secretstore"
)

// testEnv provides a fully-isolated Service instance bound to a
// per-test temporary directory. Every test gets a fresh keyring/file
// backend so secret mutations never leak between runs.
type testEnv struct {
	t       *testing.T
	dir     string
	secret  *stubStore
	service *configtx.Service
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	secretDir := filepath.Join(dir, "secrets")
	if err := os.MkdirAll(secretDir, 0o700); err != nil {
		t.Fatalf("mkdir secret dir: %v", err)
	}
	// Pre-create a minimally valid config so loadOrEmpty hits the
	// file path without Validate rejecting source_node.id == "".
	if err := os.WriteFile(cfgPath, []byte(`{
		"schema_version": 1,
		"source_node": {"id": "test-node", "label": "test-node"},
		"listen": {"addr": "127.0.0.1:0", "tls": {"cert": "auto", "key": "auto"}},
		"devices": [],
		"providers": []
	}`), 0o600); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	stub := newStubStore()
	svc, err := configtx.New(context.Background(), configtx.Options{
		ConfigPath:   cfgPath,
		SecretDir:    secretDir,
		ServiceLabel: "test",
		Now:          func() time.Time { return time.Unix(1700000000, 0) },
		SecretStore:  stub,
	})
	if err != nil {
		t.Fatalf("configtx.New: %v", err)
	}
	return &testEnv{t: t, dir: dir, secret: stub, service: svc}
}

// stubStore is a secretstore.Store replacement so Apply can run without
// touching the OS keyring. The test wires the stub into the Service
// by writing the candidate secrets to a file-based store the
// Service's secretstore.Open discovers; the file backend lives under
// the temp dir.
func newStubStore() *stubStore { return &stubStore{v: map[string]string{}} }

type stubStore struct {
	mu    sync.Mutex
	v     map[string]string
	onSet func(ref string)
}

func (s *stubStore) Get(_ context.Context, ref string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.v[ref]
	if !ok {
		return "", secretstore.ErrNotFound
	}
	return v, nil
}

func (s *stubStore) Set(_ context.Context, ref, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.onSet != nil {
		s.onSet(ref)
	}
	s.v[ref] = value
	return nil
}

func (s *stubStore) Delete(_ context.Context, ref string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.v, ref)
	return nil
}

func (s *stubStore) Backend() string { return "stub" }

func TestServiceOpenDraft(t *testing.T) {
	env := newTestEnv(t)
	d, err := env.service.OpenDraft(context.Background())
	if err != nil {
		t.Fatalf("OpenDraft: %v", err)
	}
	if d.HasPending() {
		t.Error("fresh draft reports HasPending = true")
	}
	// SourceNode.ID carries the seed value so the operator can see
	// the active node on Overview.
	if d.Pending().SourceNode.ID != "test-node" {
		t.Errorf("SourceNode.ID = %q, want test-node", d.Pending().SourceNode.ID)
	}
}

func TestEditProviderNewAndExisting(t *testing.T) {
	env := newTestEnv(t)
	d, err := env.service.OpenDraft(context.Background())
	if err != nil {
		t.Fatalf("OpenDraft: %v", err)
	}
	region := "global"
	interval := "60s"
	stale := "5m"
	if err := d.EditProvider(configtx.ProviderEdit{
		ID:           "codex-main",
		NewType:      "codex_usage",
		AccountLabel: strPtr("work"),
		Region:       &region,
		Interval:     &interval,
		StaleAfter:   &stale,
		AuthFile:     strPtr("/tmp/auth.json"),
	}); err != nil {
		t.Fatalf("EditProvider new: %v", err)
	}
	if !d.HasPending() {
		t.Error("HasPending = false after edit, want true")
	}
	diff := d.Diff()
	if len(diff) != 1 || diff[0].Op != configtx.DiffAdded {
		t.Errorf("Diff = %+v, want one added entry", diff)
	}
	// Editing an existing provider must apply changes in place.
	enabled := true
	if err := d.EditProvider(configtx.ProviderEdit{
		ID:      "codex-main",
		Enabled: &enabled,
	}); err != nil {
		t.Fatalf("EditProvider existing: %v", err)
	}
	if d.Pending().Providers[0].AccountLabel != "work" {
		t.Errorf("AccountLabel = %q, want work", d.Pending().Providers[0].AccountLabel)
	}
}

func TestEditProviderCandidateSecret(t *testing.T) {
	env := newTestEnv(t)
	d, err := env.service.OpenDraft(context.Background())
	if err != nil {
		t.Fatalf("OpenDraft: %v", err)
	}
	region := "global"
	interval := "60s"
	stale := "5m"
	if err := d.EditProvider(configtx.ProviderEdit{
		ID:              "deepseek-main",
		NewType:         "deepseek_api",
		AccountLabel:    strPtr("personal"),
		Region:          &region,
		Interval:        &interval,
		StaleAfter:      &stale,
		CandidateSecret: "sk-test-1234",
	}); err != nil {
		t.Fatalf("EditProvider with secret: %v", err)
	}
	p := d.Pending().Providers[0]
	if p.SecretRef == "" {
		t.Error("SecretRef = empty, want auto-generated")
	}
	if !strings.HasPrefix(p.SecretRef, "keyring:provider-key:deepseek-main") {
		t.Errorf("SecretRef = %q, want default keyring:provider-key:deepseek-main...", p.SecretRef)
	}
}

func TestEditProviderRejectsInvalidRegion(t *testing.T) {
	env := newTestEnv(t)
	d, err := env.service.OpenDraft(context.Background())
	if err != nil {
		t.Fatalf("OpenDraft: %v", err)
	}
	region := "mars"
	interval := "60s"
	stale := "5m"
	err = d.EditProvider(configtx.ProviderEdit{
		ID:           "x",
		NewType:      "deepseek_api",
		AccountLabel: strPtr("x"),
		Region:       &region,
		Interval:     &interval,
		StaleAfter:   &stale,
	})
	if !errors.Is(err, configtx.ErrInvalidDraft) {
		t.Errorf("err = %v, want ErrInvalidDraft", err)
	}
}

func TestEditProviderRejectsIntervalBelowMin(t *testing.T) {
	env := newTestEnv(t)
	d, err := env.service.OpenDraft(context.Background())
	if err != nil {
		t.Fatalf("OpenDraft: %v", err)
	}
	region := "global"
	interval := "1s"
	stale := "5m"
	err = d.EditProvider(configtx.ProviderEdit{
		ID:           "x",
		NewType:      "deepseek_api",
		AccountLabel: strPtr("x"),
		Region:       &region,
		Interval:     &interval,
		StaleAfter:   &stale,
	})
	if !errors.Is(err, configtx.ErrInvalidDraft) {
		t.Errorf("err = %v, want ErrInvalidDraft", err)
	}
}

func TestEditProviderDropSecret(t *testing.T) {
	env := newTestEnv(t)
	d, err := env.service.OpenDraft(context.Background())
	if err != nil {
		t.Fatalf("OpenDraft: %v", err)
	}
	region := "global"
	interval := "60s"
	stale := "5m"
	// mock type never required a secret, so DropSecret on a key
	// provider would be rejected by the type's "secret required" rule.
	if err := d.EditProvider(configtx.ProviderEdit{
		ID:           "mock-1",
		NewType:      "mock",
		AccountLabel: strPtr("a"),
		Region:       &region,
		Interval:     &interval,
		StaleAfter:   &stale,
		MockFixture:  strPtr("examples/mock-fixture.json"),
	}); err != nil {
		t.Fatalf("EditProvider: %v", err)
	}
	if d.Pending().Providers[0].SecretRef != "" {
		t.Errorf("SecretRef = %q, want empty (mock has no secret)", d.Pending().Providers[0].SecretRef)
	}
}

func TestEditProviderRotateSecret(t *testing.T) {
	env := newTestEnv(t)
	d, err := env.service.OpenDraft(context.Background())
	if err != nil {
		t.Fatalf("OpenDraft: %v", err)
	}
	region := "global"
	interval := "60s"
	stale := "5m"
	if err := d.EditProvider(configtx.ProviderEdit{
		ID:              "deepseek-main",
		NewType:         "deepseek_api",
		AccountLabel:    strPtr("a"),
		Region:          &region,
		Interval:        &interval,
		StaleAfter:      &stale,
		CandidateSecret: "sk-1",
	}); err != nil {
		t.Fatalf("EditProvider: %v", err)
	}
	original := d.Pending().Providers[0].SecretRef
	if err := d.EditProvider(configtx.ProviderEdit{
		ID:              "deepseek-main",
		CandidateSecret: "sk-2",
		RotateSecret:    true,
	}); err != nil {
		t.Fatalf("EditProvider rotate: %v", err)
	}
	got := d.Pending().Providers[0].SecretRef
	if got == original {
		t.Errorf("SecretRef unchanged after rotation: %q", got)
	}
	if got != "keyring:provider-key:deepseek-main@2" {
		t.Errorf("SecretRef = %q, want second version", got)
	}
}

func TestRejectedEditDoesNotPolluteDraft(t *testing.T) {
	env := newTestEnv(t)
	d, err := env.service.OpenDraft(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	region, interval, stale := "global", "1s", "5m"
	err = d.EditProvider(configtx.ProviderEdit{
		ID: "bad", NewType: "mock", AccountLabel: strPtr("bad"),
		Region: &region, Interval: &interval, StaleAfter: &stale,
		MockFixture: strPtr("missing.json"),
	})
	if !errors.Is(err, configtx.ErrInvalidDraft) {
		t.Fatalf("EditProvider error = %v, want ErrInvalidDraft", err)
	}
	if d.HasPending() || len(d.Pending().Providers) != 0 {
		t.Fatalf("rejected edit remained in draft: %+v", d.Pending().Providers)
	}
	if _, err := env.service.Apply(context.Background(), d, configtx.ApplyOptions{}); err != nil {
		t.Fatalf("Apply clean draft: %v", err)
	}
	loaded, err := config.Load(env.service.ConfigPath())
	if err != nil || len(loaded.Providers) != 0 {
		t.Fatalf("persisted rejected provider: %+v, err=%v", loaded, err)
	}
}

func TestExistingCandidateSecretIsAutomaticallyVersioned(t *testing.T) {
	env := newTestEnv(t)
	cfg := validConfigForTest()
	cfg.Providers = []config.ProviderConfig{{
		ID: "deepseek-main", Type: "deepseek_api", AccountLabel: "main",
		Region: "global", Interval: "60s", StaleAfter: "5m",
		SecretRef: "keyring:provider-key:deepseek-main",
	}}
	if err := cfg.Save(env.service.ConfigPath()); err != nil {
		t.Fatal(err)
	}
	d, err := env.service.OpenDraft(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := d.EditProvider(configtx.ProviderEdit{
		ID: "deepseek-main", CandidateSecret: "replacement",
	}); err != nil {
		t.Fatal(err)
	}
	got := d.Pending().Providers[0].SecretRef
	if got != "keyring:provider-key:deepseek-main@2" {
		t.Fatalf("candidate ref = %q, want @2 version", got)
	}
}

func TestCustomRegionRemainsSupported(t *testing.T) {
	env := newTestEnv(t)
	d, err := env.service.OpenDraft(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	region, baseURL, interval, stale := "custom", "http://127.0.0.1:18080", "60s", "5m"
	if err := d.EditProvider(configtx.ProviderEdit{
		ID: "deepseek-main", NewType: "deepseek_api", AccountLabel: strPtr("main"),
		Region: &region, BaseURL: &baseURL, Interval: &interval, StaleAfter: &stale,
		SecretRef: strPtr("keyring:provider-key:deepseek-main"),
	}); err != nil {
		t.Fatalf("custom region rejected: %v", err)
	}
}

func TestApplyPersistsAndRunsRestart(t *testing.T) {
	env := newTestEnv(t)
	d, err := env.service.OpenDraft(context.Background())
	if err != nil {
		t.Fatalf("OpenDraft: %v", err)
	}
	region := "global"
	interval := "60s"
	stale := "5m"
	if err := d.EditProvider(configtx.ProviderEdit{
		ID:           "mock-1",
		NewType:      "mock",
		AccountLabel: strPtr("a"),
		Region:       &region,
		Interval:     &interval,
		StaleAfter:   &stale,
		MockFixture:  strPtr("examples/mock-fixture.json"),
	}); err != nil {
		t.Fatalf("EditProvider: %v", err)
	}
	var restartCount int32
	var healthCount int32
	res, err := env.service.Apply(context.Background(), d, configtx.ApplyOptions{
		Restart:     func(_ context.Context) error { atomic.AddInt32(&restartCount, 1); return nil },
		HealthCheck: func(_ context.Context) error { atomic.AddInt32(&healthCount, 1); return nil },
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !res.Restarted {
		t.Error("ApplyResult.Restarted = false, want true")
	}
	if !res.Healthy {
		t.Error("ApplyResult.Healthy = false, want true")
	}
	if restartCount != 1 || healthCount != 1 {
		t.Errorf("restart/health count = %d/%d, want 1/1", restartCount, healthCount)
	}
	// The on-disk config must contain the new provider.
	loaded, err := config.Load(env.service.ConfigPath())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded.Providers) != 1 || loaded.Providers[0].ID != "mock-1" {
		t.Errorf("on-disk providers = %+v, want one mock-1", loaded.Providers)
	}
}

func TestApplyCommitsVersionedSecretBeforeConfig(t *testing.T) {
	env := newTestEnv(t)
	d, err := env.service.OpenDraft(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	region, interval, stale := "global", "60s", "5m"
	if err := d.EditProvider(configtx.ProviderEdit{
		ID: "deepseek-main", NewType: "deepseek_api", AccountLabel: strPtr("main"),
		Region: &region, Interval: &interval, StaleAfter: &stale,
		CandidateSecret: "new-secret",
	}); err != nil {
		t.Fatal(err)
	}
	observedOldConfig := false
	env.secret.onSet = func(string) {
		loaded, loadErr := config.Load(env.service.ConfigPath())
		observedOldConfig = loadErr == nil && len(loaded.Providers) == 0
	}
	if _, err := env.service.Apply(context.Background(), d, configtx.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	if !observedOldConfig {
		t.Fatal("config referenced candidate before the secret was committed")
	}
	loaded, err := config.Load(env.service.ConfigPath())
	if err != nil || len(loaded.Providers) != 1 ||
		loaded.Providers[0].SecretRef != "keyring:provider-key:deepseek-main@1" {
		t.Fatalf("persisted config = %+v, err=%v", loaded, err)
	}
}

func TestApplyFailsWhenHealthCheckFails(t *testing.T) {
	env := newTestEnv(t)
	d, err := env.service.OpenDraft(context.Background())
	if err != nil {
		t.Fatalf("OpenDraft: %v", err)
	}
	region := "global"
	interval := "60s"
	stale := "5m"
	if err := d.EditProvider(configtx.ProviderEdit{
		ID:           "mock-1",
		NewType:      "mock",
		AccountLabel: strPtr("a"),
		Region:       &region,
		Interval:     &interval,
		StaleAfter:   &stale,
		MockFixture:  strPtr("examples/mock-fixture.json"),
	}); err != nil {
		t.Fatalf("EditProvider: %v", err)
	}
	_, err = env.service.Apply(context.Background(), d, configtx.ApplyOptions{
		Restart:     func(_ context.Context) error { return nil },
		HealthCheck: func(_ context.Context) error { return errors.New("synthetic failure") },
	})
	if err == nil {
		t.Fatal("Apply = nil, want error")
	}
	var applyErr *configtx.ApplyError
	if !errors.As(err, &applyErr) {
		t.Fatalf("err = %v, want *ApplyError", err)
	}
	if applyErr.Step != configtx.StepVerifying {
		t.Errorf("Step = %q, want verifying", applyErr.Step)
	}
	// The on-disk config must have been rolled back to the original
	// (empty) state.
	loaded, loadErr := config.Load(env.service.ConfigPath())
	if loadErr != nil {
		t.Fatalf("Load after rollback: %v", loadErr)
	}
	if len(loaded.Providers) != 0 {
		t.Errorf("providers after rollback = %+v, want empty", loaded.Providers)
	}
}

func TestApplyRevisionConflict(t *testing.T) {
	env := newTestEnv(t)
	d, err := env.service.OpenDraft(context.Background())
	if err != nil {
		t.Fatalf("OpenDraft: %v", err)
	}
	region := "global"
	interval := "60s"
	stale := "5m"
	if err := d.EditProvider(configtx.ProviderEdit{
		ID:           "mock-1",
		NewType:      "mock",
		AccountLabel: strPtr("a"),
		Region:       &region,
		Interval:     &interval,
		StaleAfter:   &stale,
		MockFixture:  strPtr("examples/mock-fixture.json"),
	}); err != nil {
		t.Fatalf("EditProvider: %v", err)
	}
	// First Apply succeeds and bumps the Service's baseRevision.
	if _, err := env.service.Apply(context.Background(), d, configtx.ApplyOptions{}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	// A second draft loaded after the Apply must conflict with the
	// post-Apply revision.
	_, err = env.service.Apply(context.Background(), d, configtx.ApplyOptions{})
	if !errors.Is(err, configtx.ErrRevisionConflict) {
		t.Errorf("err = %v, want ErrRevisionConflict", err)
	}
}

func TestApplyDetectsExternalDiskEdit(t *testing.T) {
	env := newTestEnv(t)
	d, err := env.service.OpenDraft(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(env.service.ConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	cfg.SourceNode.Label = "external-edit"
	if err := cfg.Save(env.service.ConfigPath()); err != nil {
		t.Fatal(err)
	}
	_, err = env.service.Apply(context.Background(), d, configtx.ApplyOptions{})
	if !errors.Is(err, configtx.ErrRevisionConflict) {
		t.Fatalf("Apply error = %v, want revision conflict", err)
	}
}

func TestRollbackRestartsOldConfigAndDeletesCandidateSecret(t *testing.T) {
	env := newTestEnv(t)
	oldRef := "keyring:provider-key:deepseek-main"
	cfg := validConfigForTest()
	cfg.Providers = []config.ProviderConfig{{
		ID: "deepseek-main", Type: "deepseek_api", AccountLabel: "main",
		Region: "global", Interval: "60s", StaleAfter: "5m", SecretRef: oldRef,
	}}
	if err := cfg.Save(env.service.ConfigPath()); err != nil {
		t.Fatal(err)
	}
	if err := env.secret.Set(context.Background(), oldRef, "old-value"); err != nil {
		t.Fatal(err)
	}
	d, err := env.service.OpenDraft(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := d.EditProvider(configtx.ProviderEdit{
		ID: "deepseek-main", CandidateSecret: "new-value",
	}); err != nil {
		t.Fatal(err)
	}
	newRef := d.Pending().Providers[0].SecretRef
	var restarts int32
	_, err = env.service.Apply(context.Background(), d, configtx.ApplyOptions{
		Restart: func(context.Context) error {
			atomic.AddInt32(&restarts, 1)
			return nil
		},
		HealthCheck: func(context.Context) error { return errors.New("unhealthy") },
	})
	if err == nil {
		t.Fatal("Apply succeeded, want health failure")
	}
	if got := atomic.LoadInt32(&restarts); got != 2 {
		t.Fatalf("restart count = %d, want new + restored old", got)
	}
	loaded, loadErr := config.Load(env.service.ConfigPath())
	if loadErr != nil || loaded.Providers[0].SecretRef != oldRef {
		t.Fatalf("rollback config = %+v, err=%v", loaded, loadErr)
	}
	if value, getErr := env.secret.Get(context.Background(), oldRef); getErr != nil || value != "old-value" {
		t.Fatalf("old secret = %q, err=%v", value, getErr)
	}
	if _, getErr := env.secret.Get(context.Background(), newRef); !errors.Is(getErr, secretstore.ErrNotFound) {
		t.Fatalf("candidate secret still exists: %v", getErr)
	}
}

func TestApplyPreservesRequestedOrphanSecret(t *testing.T) {
	env := newTestEnv(t)
	ref := "keyring:provider-key:deepseek-main"
	cfg := validConfigForTest()
	cfg.Providers = []config.ProviderConfig{{
		ID: "deepseek-main", Type: "deepseek_api", AccountLabel: "main",
		Region: "global", Interval: "60s", StaleAfter: "5m", SecretRef: ref,
	}}
	if err := cfg.Save(env.service.ConfigPath()); err != nil {
		t.Fatal(err)
	}
	_ = env.secret.Set(context.Background(), ref, "keep-me")
	d, err := env.service.OpenDraft(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.DeleteProvider("deepseek-main"); err != nil {
		t.Fatal(err)
	}
	if _, err := env.service.Apply(context.Background(), d, configtx.ApplyOptions{
		KeepSecretRefs: []string{ref},
	}); err != nil {
		t.Fatal(err)
	}
	if value, err := env.secret.Get(context.Background(), ref); err != nil || value != "keep-me" {
		t.Fatalf("preserved secret = %q, err=%v", value, err)
	}
}

func TestMetricShadowUsesActualMetricIDs(t *testing.T) {
	env := newTestEnv(t)
	cfg := validConfigForTest()
	cfg.Providers = []config.ProviderConfig{{
		ID: "codex-main", Type: "codex_usage", AccountLabel: "main",
		Region: "global", Interval: "60s", StaleAfter: "5m",
	}}
	if err := cfg.Save(env.service.ConfigPath()); err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(env.dir, "shadow.json")
	if err := os.WriteFile(fixture, []byte(`{"metrics":[{
		"id":"codex-main.5h","provider":"mock","display_name":"Shadow",
		"metric_kind":"quota","value":"1","limit":"100","unit":"percent",
		"window":"rolling_5h","precision":"verified","source_kind":"mock"
	}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	d, err := env.service.OpenDraft(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	region, interval, stale := "global", "60s", "5m"
	err = d.EditProvider(configtx.ProviderEdit{
		ID: "mock-shadow", NewType: "mock", AccountLabel: strPtr("shadow"),
		Region: &region, Interval: &interval, StaleAfter: &stale,
		MockFixture: &fixture,
	})
	if !errors.Is(err, configtx.ErrShadowedProvider) || !strings.Contains(err.Error(), "codex-main.5h") {
		t.Fatalf("shadow error = %v", err)
	}
}

func TestDeleteProvider(t *testing.T) {
	env := newTestEnv(t)
	// Pre-seed a config with one provider.
	if err := os.WriteFile(env.service.ConfigPath(), []byte(`{
		"schema_version": 1,
		"source_node": {"id": "n", "label": "n"},
		"listen": {"addr": "127.0.0.1:8443", "tls": {"cert": "auto", "key": "auto"}},
		"devices": [],
		"providers": [
			{"id": "mock-1", "type": "mock", "account_label": "a",
			 "region": "global", "interval": "60s", "stale_after": "5m", "enabled": true}
		]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	d, err := env.service.OpenDraft(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ref, err := d.DeleteProvider("mock-1")
	if err != nil {
		t.Fatalf("DeleteProvider: %v", err)
	}
	if ref != "" {
		t.Errorf("ref = %q, want empty (mock has no secret)", ref)
	}
	if len(d.Pending().Providers) != 0 {
		t.Errorf("Providers = %+v, want empty", d.Pending().Providers)
	}
}

func TestStatusRedactsSecret(t *testing.T) {
	env := newTestEnv(t)
	if err := os.WriteFile(env.service.ConfigPath(), []byte(`{
		"schema_version": 1,
		"source_node": {"id": "n", "label": "n"},
		"listen": {"addr": "127.0.0.1:8443", "tls": {"cert": "auto", "key": "auto"}},
		"devices": [],
		"providers": [
			{"id": "deepseek-main", "type": "deepseek_api", "account_label": "personal",
			 "region": "global", "interval": "60s", "stale_after": "5m", "enabled": true,
			 "secret_ref": "keyring:provider-key:deepseek-main"}
		]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	statuses, err := env.service.ProviderStatuses(context.Background(), nil)
	if err != nil {
		t.Fatalf("ProviderStatuses: %v", err)
	}
	if len(statuses) != 1 {
		t.Fatalf("statuses = %d, want 1", len(statuses))
	}
	if statuses[0].SecretRef == "keyring:provider-key:deepseek-main" {
		t.Errorf("SecretRef = %q, want masked form", statuses[0].SecretRef)
	}
	if !strings.Contains(statuses[0].SecretRef, "...") {
		t.Errorf("SecretRef = %q, want ... substring", statuses[0].SecretRef)
	}
}

func TestProviderStatusesOnlyMarkActualChangesAndMergeRows(t *testing.T) {
	env := newTestEnv(t)
	cfg := validConfigForTest()
	cfg.Providers = []config.ProviderConfig{
		{ID: "modified", Type: "codex_usage", AccountLabel: "m", Region: "global", Interval: "60s", StaleAfter: "5m"},
		{ID: "removed", Type: "codex_usage", AccountLabel: "r", Region: "global", Interval: "60s", StaleAfter: "5m"},
		{ID: "unchanged", Type: "codex_usage", AccountLabel: "u", Region: "global", Interval: "60s", StaleAfter: "5m"},
	}
	if err := cfg.Save(env.service.ConfigPath()); err != nil {
		t.Fatal(err)
	}
	d, err := env.service.OpenDraft(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	interval := "90s"
	if err := d.EditProvider(configtx.ProviderEdit{ID: "modified", Interval: &interval}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.DeleteProvider("removed"); err != nil {
		t.Fatal(err)
	}
	region, stale := "global", "5m"
	if err := d.EditProvider(configtx.ProviderEdit{
		ID: "added", NewType: "mock", AccountLabel: strPtr("a"), Region: &region,
		Interval: strPtr("60s"), StaleAfter: &stale, MockFixture: strPtr("missing.json"),
	}); err != nil {
		t.Fatal(err)
	}
	statuses, err := env.service.ProviderStatuses(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	got := make(map[string]configtx.ProviderStatus)
	for _, status := range statuses {
		got[status.ID] = status
	}
	if len(got) != 4 {
		t.Fatalf("statuses = %+v", statuses)
	}
	if !got["modified"].Pending || !got["removed"].Pending || !got["added"].Pending {
		t.Fatalf("changed pending flags = %+v", got)
	}
	if got["unchanged"].Pending {
		t.Fatal("unchanged provider marked pending")
	}
	if got["added"].Configured || !got["removed"].Configured {
		t.Fatalf("configured flags = added:%v removed:%v",
			got["added"].Configured, got["removed"].Configured)
	}
}

func TestConcurrentApplySerialised(t *testing.T) {
	env := newTestEnv(t)
	d1, err := env.service.OpenDraft(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	d2, err := env.service.OpenDraft(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	region := "global"
	interval := "60s"
	stale := "5m"
	if err := d1.EditProvider(configtx.ProviderEdit{
		ID:           "mock-1",
		NewType:      "mock",
		AccountLabel: strPtr("a"),
		Region:       &region,
		Interval:     &interval,
		StaleAfter:   &stale,
		MockFixture:  strPtr("examples/mock-fixture.json"),
	}); err != nil {
		t.Fatal(err)
	}
	if err := d2.EditProvider(configtx.ProviderEdit{
		ID:           "mock-2",
		NewType:      "mock",
		AccountLabel: strPtr("b"),
		Region:       &region,
		Interval:     &interval,
		StaleAfter:   &stale,
		MockFixture:  strPtr("examples/mock-fixture.json"),
	}); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	errs := make(chan error, 2)
	for _, draft := range []*configtx.Draft{d1, d2} {
		go func(draft *configtx.Draft) {
			<-start
			_, applyErr := env.service.Apply(context.Background(), draft, configtx.ApplyOptions{})
			errs <- applyErr
		}(draft)
	}
	close(start)
	var success, conflict int
	for i := 0; i < 2; i++ {
		applyErr := <-errs
		switch {
		case applyErr == nil:
			success++
		case errors.Is(applyErr, configtx.ErrRevisionConflict):
			conflict++
		default:
			t.Fatalf("unexpected Apply error: %v", applyErr)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success/conflict = %d/%d, want 1/1", success, conflict)
	}
}

func strPtr(s string) *string { return &s }

func validConfigForTest() *config.Config {
	cfg := &config.Config{
		SchemaVersion: config.SchemaVersion,
		SourceNode:    config.SourceNodeConfig{ID: "test-node", Label: "test-node"},
		Listen: config.ListenConfig{
			Addr: "127.0.0.1:0",
			TLS:  config.TLSConfig{Cert: "auto", Key: "auto"},
		},
	}
	cfg.ApplyDefaults()
	return cfg
}
