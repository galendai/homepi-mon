package webadmin_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/galendai/homepi-mon/internal/configtx"
	"github.com/galendai/homepi-mon/internal/webadmin"

	// Pull every connector so init() registers the factory + meta
	// that the Service relies on.
	_ "github.com/galendai/homepi-mon/internal/connector/codexusage"
	_ "github.com/galendai/homepi-mon/internal/connector/deepseek"
	_ "github.com/galendai/homepi-mon/internal/connector/kimiapi"
	_ "github.com/galendai/homepi-mon/internal/connector/kimicoding"
	_ "github.com/galendai/homepi-mon/internal/connector/minimax"
	_ "github.com/galendai/homepi-mon/internal/connector/mock"
)

// newTestServer brings up a Server bound to a random loopback port
// plus a per-test temp dir for the config and secret backend. The
// returned teardown closes the listener. The URL ends in a slash so
// callers can append path segments without double-slash noise.
func newTestServer(t *testing.T) (*webadmin.Server, string) {
	return newTestServerWithConfig(t, nil)
}

func newTestServerWithConfig(t *testing.T, mutate func(*webadmin.Config)) (*webadmin.Server, string) {
	return newTestServerWithConfigJSON(t, `{
		"schema_version": 1,
		"source_node": {"id": "test-node", "label": "test-node"},
		"listen": {"addr": "127.0.0.1:0", "tls": {"cert": "auto", "key": "auto"}},
		"devices": [],
		"providers": []
	}`, mutate)
}

func newTestServerWithConfigJSON(t *testing.T, configJSON string, mutate func(*webadmin.Config)) (*webadmin.Server, string) {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	secretDir := filepath.Join(dir, "secrets")
	if err := os.MkdirAll(secretDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte(configJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	svc, err := configtx.New(context.Background(), configtx.Options{
		ConfigPath: cfgPath, SecretDir: secretDir,
		DataDir: dir, ServiceLabel: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	webCfg := webadmin.Config{
		Service:     svc,
		Addr:        "127.0.0.1:0",
		IdleTimeout: 30 * time.Second,
		Restart:     func(context.Context) error { return nil },
		HealthCheck: func(context.Context) error { return nil },
	}
	if mutate != nil {
		mutate(&webCfg)
	}
	srv, err := webadmin.New(webCfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Listen(); err != nil {
		t.Fatal(err)
	}
	// Run in a goroutine; the Server exits when Shutdown is called.
	go func() { _ = srv.Run(context.Background()) }()
	// Allow the listener to accept connections before tests fire
	// requests. 50 ms is well below the Server's ReadTimeout.
	time.Sleep(50 * time.Millisecond)
	t.Cleanup(func() { _ = srv.Shutdown(context.Background()) })
	return srv, srv.URL()
}

func doJSON(t *testing.T, method, url, csrf, origin string, body any) (*http.Response, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, raw
}

func TestNewRejectsNonLoopbackBind(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	secretDir := filepath.Join(dir, "secrets")
	if err := os.WriteFile(cfgPath, []byte(`{"schema_version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	svc, _ := configtx.New(context.Background(), configtx.Options{
		ConfigPath: cfgPath, SecretDir: secretDir,
	})
	srv, err := webadmin.New(webadmin.Config{Service: svc, Addr: "0.0.0.0:0"})
	if err != nil {
		// New itself may reject the address; either path is
		// acceptable, but the listener must never bind to a
		// non-loopback interface.
		if !strings.Contains(err.Error(), "loopback") {
			t.Errorf("err = %q, want loopback-related message", err.Error())
		}
		return
	}
	if _, err := srv.Listen(); err == nil {
		t.Fatal("Listen accepted 0.0.0.0 bind")
	} else if !strings.Contains(err.Error(), "loopback") {
		t.Errorf("Listen err = %q, want loopback-related message", err.Error())
	}
}

func TestSecurityHeaders(t *testing.T) {
	_, url := newTestServer(t)
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	want := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"Referrer-Policy":        "no-referrer",
	}
	for k, v := range want {
		if got := resp.Header.Get(k); got != v {
			t.Errorf("header %s = %q, want %q", k, got, v)
		}
	}
	csp := resp.Header.Get("Content-Security-Policy")
	if !strings.Contains(csp, "default-src 'self'") ||
		!strings.Contains(csp, "frame-ancestors 'none'") {
		t.Errorf("CSP = %q, want default-src 'self' and frame-ancestors 'none'", csp)
	}
}

func TestCSRFBlocksStateChange(t *testing.T) {
	srv, url := newTestServer(t)
	_ = srv
	base := strings.TrimSuffix(url, "/")
	resp, body := doJSON(t, http.MethodPost, base+"/api/draft", "", base, map[string]any{
		"id": "p1", "new_type": "mock", "account_label": "a",
		"region": "global", "interval": "60s", "stale_after": "5m",
	})
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403; body=%s", resp.StatusCode, body)
	}
	resp, body = doJSON(t, http.MethodPost, base+"/api/draft", "wrong-token", base, map[string]any{
		"id": "p1", "new_type": "mock", "account_label": "a",
		"region": "global", "interval": "60s", "stale_after": "5m",
	})
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("wrong token: status = %d, want 403; body=%s", resp.StatusCode, body)
	}
}

func TestOriginEnforced(t *testing.T) {
	srv, url := newTestServer(t)
	_ = srv
	base := strings.TrimSuffix(url, "/")
	resp, body := doJSON(t, http.MethodPost, base+"/api/draft", "any-token", "http://attacker.example", map[string]any{
		"id": "p1", "new_type": "mock", "account_label": "a",
		"region": "global", "interval": "60s", "stale_after": "5m",
	})
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("foreign origin: status = %d, want 403; body=%s", resp.StatusCode, body)
	}
	resp, body = doJSON(t, http.MethodPost, base+"/api/draft", srv.CSRFToken(), "http://127.0.0.1:1", map[string]any{
		"id": "p1", "new_type": "mock", "account_label": "a",
		"region": "global", "interval": "60s", "stale_after": "5m",
	})
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("other loopback origin: status = %d, want 403; body=%s", resp.StatusCode, body)
	}
}

func TestDraftLifecycle(t *testing.T) {
	srv, url := newTestServer(t)
	base := strings.TrimSuffix(url, "/")
	// 1. Create a draft.
	resp, body := doJSON(t, http.MethodPost, base+"/api/draft", srv.CSRFToken(), base, map[string]any{
		"id":            "mock-1",
		"new_type":      "mock",
		"account_label": "demo",
		"region":        "global",
		"interval":      "60s",
		"stale_after":   "5m",
		"mock_fixture":  "examples/mock-fixture.json",
		"enabled":       true,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create draft: status = %d, body=%s", resp.StatusCode, body)
	}
	// 2. Get the draft: must contain mock-1.
	resp, body = doJSON(t, http.MethodGet, base+"/api/draft", "", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get draft: status = %d", resp.StatusCode)
	}
	if !bytes.Contains(body, []byte("mock-1")) {
		t.Errorf("draft body missing mock-1: %s", body)
	}
	// 3. Delete the draft.
	resp, body = doJSON(t, http.MethodDelete, base+"/api/draft", srv.CSRFToken(), base, map[string]any{
		"id": "mock-1",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete draft: status = %d, body=%s", resp.StatusCode, body)
	}
}

func TestExistingProviderCanBeEditedDisabledAndEnabled(t *testing.T) {
	srv, url := newTestServerWithConfigJSON(t, `{
		"schema_version": 1,
		"source_node": {"id": "test-node", "label": "test-node"},
		"listen": {"addr": "127.0.0.1:0", "tls": {"cert": "auto", "key": "auto"}},
		"devices": [],
		"providers": [{
			"id": "deepseek-main", "type": "deepseek_api", "account_label": "old label",
			"region": "global", "interval": "60s", "stale_after": "5m", "enabled": false,
			"secret_ref": "keyring:provider-key:deepseek-main@1"
		}]
	}`, nil)
	base := strings.TrimSuffix(url, "/")
	resp, body := doJSON(t, http.MethodPut, base+"/api/draft", srv.CSRFToken(), base, map[string]any{
		"id": "deepseek-main", "new_type": "deepseek_api", "account_label": "edited label",
		"region": "cn", "interval": "90s", "stale_after": "6m", "enabled": false,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("edit draft: status=%d body=%s", resp.StatusCode, body)
	}
	var draft struct {
		Providers []struct {
			ID           string `json:"id"`
			AccountLabel string `json:"account_label"`
			Region       string `json:"region"`
			Interval     string `json:"interval"`
			SecretRef    string `json:"secret_ref"`
			Enabled      bool   `json:"enabled"`
		} `json:"providers"`
		Diff []struct {
			ID string `json:"ID"`
			Op string `json:"Op"`
		} `json:"diff"`
	}
	if err := json.Unmarshal(body, &draft); err != nil {
		t.Fatal(err)
	}
	if len(draft.Providers) != 1 || draft.Providers[0].AccountLabel != "edited label" ||
		draft.Providers[0].Region != "cn" || draft.Providers[0].Interval != "90s" ||
		draft.Providers[0].SecretRef == "" || len(draft.Diff) != 1 ||
		draft.Diff[0].ID != "deepseek-main" || draft.Diff[0].Op != "modified" {
		t.Fatalf("edited provider = %+v", draft.Providers)
	}

	for _, enabled := range []bool{true, false} {
		resp, body = doJSON(t, http.MethodPut, base+"/api/draft", srv.CSRFToken(), base, map[string]any{
			"id": "deepseek-main", "enabled": enabled,
		})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("toggle enabled=%v: status=%d body=%s", enabled, resp.StatusCode, body)
		}
		if err := json.Unmarshal(body, &draft); err != nil {
			t.Fatal(err)
		}
		if len(draft.Providers) != 1 || draft.Providers[0].Enabled != enabled ||
			draft.Providers[0].AccountLabel != "edited label" || draft.Providers[0].SecretRef == "" {
			t.Fatalf("toggled provider enabled=%v: %+v", enabled, draft.Providers)
		}
	}
}

func TestDraftRejectsInvalidInterval(t *testing.T) {
	srv, url := newTestServer(t)
	base := strings.TrimSuffix(url, "/")
	resp, body := doJSON(t, http.MethodPost, base+"/api/draft", srv.CSRFToken(), base, map[string]any{
		"id":            "p1",
		"new_type":      "mock",
		"account_label": "a",
		"region":        "global",
		"interval":      "1s",
		"stale_after":   "5m",
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body=%s", resp.StatusCode, body)
	}
}

func TestConcurrentDraftRequestsAreSerialised(t *testing.T) {
	srv, serverURL := newTestServer(t)
	base := strings.TrimSuffix(serverURL, "/")
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, id := range []string{"concurrent-a", "concurrent-b"} {
		go func(id string) {
			<-start
			body, _ := json.Marshal(map[string]any{
				"id": id, "new_type": "mock", "account_label": id,
				"region": "global", "interval": "60s", "stale_after": "5m",
				"mock_fixture": "missing.json",
			})
			req, reqErr := http.NewRequest(http.MethodPost, base+"/api/draft", bytes.NewReader(body))
			if reqErr != nil {
				results <- reqErr
				return
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Origin", base)
			req.Header.Set("X-CSRF-Token", srv.CSRFToken())
			resp, callErr := http.DefaultClient.Do(req)
			if callErr != nil {
				results <- callErr
				return
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				results <- fmt.Errorf("provider %s status %d", id, resp.StatusCode)
				return
			}
			results <- nil
		}(id)
	}
	close(start)
	for i := 0; i < 2; i++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	resp, body := doJSON(t, http.MethodGet, base+"/api/draft", "", "", nil)
	if resp.StatusCode != http.StatusOK || !bytes.Contains(body, []byte("concurrent-a")) ||
		!bytes.Contains(body, []byte("concurrent-b")) {
		t.Fatalf("concurrent draft lost an update: %s", body)
	}
}

func TestStaticIndex(t *testing.T) {
	_, url := newTestServer(t)
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !bytes.Contains(body, []byte("HomePi Node Admin")) {
		t.Errorf("index page missing title: %s", body)
	}
	for _, marker := range []string{
		`content="#f8fcfb"`,
		`href="#main-content"`,
		`aria-label="Primary navigation"`,
		`id="main-content"`,
		`id="global-feedback"`,
		`id="runtime-signal"`,
		`id="apply-runtime-hint"`,
		`aria-live="polite"`,
	} {
		if !bytes.Contains(body, []byte(marker)) {
			t.Errorf("index page missing accessible shell marker %q", marker)
		}
	}
}

func TestStaticAssetsAreServedFromPublicPaths(t *testing.T) {
	_, serverURL := newTestServer(t)
	base := strings.TrimSuffix(serverURL, "/")
	for _, asset := range []struct {
		path, marker string
	}{
		{"/static/app.css", ".runtime-signal"},
		{"/static/app.js", "Runtime versions aligned"},
	} {
		resp, err := http.Get(base + asset.path)
		if err != nil {
			t.Fatal(err)
		}
		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil || resp.StatusCode != http.StatusOK ||
			!bytes.Contains(body, []byte(asset.marker)) {
			t.Fatalf("GET %s: status=%d body=%q err=%v",
				asset.path, resp.StatusCode, body, readErr)
		}
	}
}

func TestProviderClientExposesEditToggleAndCancelActions(t *testing.T) {
	_, serverURL := newTestServer(t)
	base := strings.TrimSuffix(serverURL, "/")
	resp, err := http.Get(base + "/static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	body, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("app.js: status=%d err=%v", resp.StatusCode, readErr)
	}
	for _, marker := range []string{
		"beginProviderEdit(provider)",
		"toggleProvider(provider, toggleButton)",
		"cancel-edit-btn",
		"Leave the secret blank to keep the existing credential",
	} {
		if !bytes.Contains(body, []byte(marker)) {
			t.Errorf("app.js missing provider management marker %q", marker)
		}
	}
}

func TestBootstrapTokenAuthorizesSameOriginMutation(t *testing.T) {
	_, serverURL := newTestServer(t)
	base := strings.TrimSuffix(serverURL, "/")
	resp, body := doJSON(t, http.MethodGet, base+"/api/bootstrap", "", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("bootstrap status=%d body=%s", resp.StatusCode, body)
	}
	var bootstrap map[string]string
	if err := json.Unmarshal(body, &bootstrap); err != nil {
		t.Fatal(err)
	}
	token := bootstrap["csrf_token"]
	if token == "" {
		t.Fatal("bootstrap returned empty CSRF token")
	}
	resp, body = doJSON(t, http.MethodPost, base+"/api/draft", token, base, map[string]any{
		"id": "p1", "new_type": "mock", "account_label": "a",
		"region": "global", "interval": "60s", "stale_after": "5m",
		"mock_fixture": "missing.json",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("same-origin mutation status=%d body=%s", resp.StatusCode, body)
	}
}

func TestDraftResponseMasksSecretReferences(t *testing.T) {
	srv, serverURL := newTestServer(t)
	base := strings.TrimSuffix(serverURL, "/")
	fullRef := "keyring:provider-key:account-production-deepseek"
	secret := "candidate-super-secret"
	resp, body := doJSON(t, http.MethodPost, base+"/api/draft", srv.CSRFToken(), base, map[string]any{
		"id": "deepseek-main", "new_type": "deepseek_api", "account_label": "main",
		"region": "global", "interval": "60s", "stale_after": "5m",
		"secret_ref": fullRef, "candidate_secret": secret,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("draft status=%d body=%s", resp.StatusCode, body)
	}
	if bytes.Contains(body, []byte(fullRef)) || bytes.Contains(body, []byte(secret)) {
		t.Fatalf("draft leaked credential data: %s", body)
	}
	if !bytes.Contains(body, []byte("keyring:...")) {
		t.Fatalf("draft omitted masked reference: %s", body)
	}
}

func TestStatusAllowsMissingDisplayCallback(t *testing.T) {
	_, serverURL := newTestServerWithConfig(t, func(cfg *webadmin.Config) {
		cfg.DisplayStatus = nil
	})
	resp, body := doJSON(t, http.MethodGet,
		strings.TrimSuffix(serverURL, "/")+"/api/status", "", "", nil)
	if resp.StatusCode != http.StatusOK || !bytes.Contains(body, []byte("Display status source not registered")) {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
}

func TestStatusReportsRedactedRuntimeAlignment(t *testing.T) {
	_, serverURL := newTestServerWithConfig(t, func(cfg *webadmin.Config) {
		cfg.RuntimeStatus = func(context.Context) webadmin.RuntimeSnapshot {
			return webadmin.RuntimeSnapshot{
				State:   "version_mismatch",
				Message: "The installed service uses a different build.",
				Admin: webadmin.BuildSnapshot{
					Version: "0.1.0-dev", Commit: "unknown", Platform: "darwin/arm64",
				},
				Service: webadmin.BuildSnapshot{
					Version: "0.1.0", Commit: "07740bf", Built: "2026-08-11T05:03:26Z",
				},
				ServiceInstalled: true,
				ServiceRunning:   true,
			}
		}
	})
	resp, body := doJSON(t, http.MethodGet,
		strings.TrimSuffix(serverURL, "/")+"/api/status", "", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	for _, want := range []string{
		`"state":"version_mismatch"`,
		`"version":"0.1.0-dev"`,
		`"commit":"07740bf"`,
		`"service_installed":true`,
		`"service_running":true`,
	} {
		if !bytes.Contains(body, []byte(want)) {
			t.Errorf("runtime response missing %s: %s", want, body)
		}
	}
	for _, forbidden := range []string{
		"/Users/operator/.local/bin/homepi-node",
		"launchctl",
		"ProgramArguments",
	} {
		if bytes.Contains(body, []byte(forbidden)) {
			t.Errorf("runtime response leaked service detail %q: %s", forbidden, body)
		}
	}
}

func TestStatusRuntimeDetectionFailureIsNonBlocking(t *testing.T) {
	_, serverURL := newTestServerWithConfig(t, func(cfg *webadmin.Config) {
		cfg.RuntimeStatus = func(context.Context) webadmin.RuntimeSnapshot {
			return webadmin.RuntimeSnapshot{
				State:   "unavailable",
				Message: "The installed service version could not be checked.",
				Admin:   webadmin.BuildSnapshot{Version: "0.1.0-dev", Commit: "unknown"},
			}
		}
	})
	base := strings.TrimSuffix(serverURL, "/")
	resp, body := doJSON(t, http.MethodGet, base+"/api/status", "", "", nil)
	if resp.StatusCode != http.StatusOK || !bytes.Contains(body, []byte(`"state":"unavailable"`)) {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	resp, body = doJSON(t, http.MethodGet, base+"/api/draft", "", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("draft was blocked by runtime detection failure: status=%d body=%s", resp.StatusCode, body)
	}
}

func TestApplyRequiresTestAndRunsConfiguredServiceCallbacks(t *testing.T) {
	var restarts, checks int
	srv, serverURL := newTestServerWithConfig(t, func(cfg *webadmin.Config) {
		cfg.Restart = func(context.Context) error { restarts++; return nil }
		cfg.HealthCheck = func(context.Context) error { checks++; return nil }
	})
	base := strings.TrimSuffix(serverURL, "/")
	fixture, err := filepath.Abs("../../examples/mock-fixture.json")
	if err != nil {
		t.Fatal(err)
	}
	resp, body := doJSON(t, http.MethodPost, base+"/api/draft", srv.CSRFToken(), base, map[string]any{
		"id": "mock-apply", "new_type": "mock", "account_label": "main",
		"region": "global", "interval": "60s", "stale_after": "5m",
		"mock_fixture": fixture,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("draft status=%d body=%s", resp.StatusCode, body)
	}
	resp, body = doJSON(t, http.MethodPost, base+"/api/draft/apply", srv.CSRFToken(), base, map[string]any{})
	if resp.StatusCode != http.StatusConflict || restarts != 0 || checks != 0 {
		t.Fatalf("untested apply status=%d restart/check=%d/%d body=%s",
			resp.StatusCode, restarts, checks, body)
	}
	resp, body = doJSON(t, http.MethodPost, base+"/api/draft/test", srv.CSRFToken(), base,
		map[string]any{"id": "mock-apply"})
	if resp.StatusCode != http.StatusOK || !bytes.Contains(body, []byte(`"class":""`)) {
		t.Fatalf("test status=%d body=%s", resp.StatusCode, body)
	}
	resp, body = doJSON(t, http.MethodPut, base+"/api/draft", srv.CSRFToken(), base,
		map[string]any{"id": "mock-apply", "interval": "61s"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("edit after test status=%d body=%s", resp.StatusCode, body)
	}
	resp, body = doJSON(t, http.MethodPost, base+"/api/draft/apply", srv.CSRFToken(), base, map[string]any{})
	if resp.StatusCode != http.StatusConflict || restarts != 0 || checks != 0 {
		t.Fatalf("stale test apply status=%d restart/check=%d/%d body=%s",
			resp.StatusCode, restarts, checks, body)
	}
	resp, body = doJSON(t, http.MethodPost, base+"/api/draft/test", srv.CSRFToken(), base,
		map[string]any{"id": "mock-apply"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("retest status=%d body=%s", resp.StatusCode, body)
	}
	resp, body = doJSON(t, http.MethodPost, base+"/api/draft/apply", srv.CSRFToken(), base, map[string]any{})
	if resp.StatusCode != http.StatusOK || restarts != 1 || checks != 1 {
		t.Fatalf("tested apply status=%d restart/check=%d/%d body=%s",
			resp.StatusCode, restarts, checks, body)
	}
}

func TestIdleTimeoutResetsOnRequestActivity(t *testing.T) {
	_, serverURL := newTestServerWithConfig(t, func(cfg *webadmin.Config) {
		cfg.IdleTimeout = 300 * time.Millisecond
	})
	time.Sleep(200 * time.Millisecond)
	resp, err := http.Get(serverURL)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	time.Sleep(120 * time.Millisecond)
	resp, err = http.Get(serverURL)
	if err != nil {
		t.Fatalf("server closed despite recent request activity: %v", err)
	}
	_ = resp.Body.Close()
}

func TestPathTraversalRejected(t *testing.T) {
	_, url := newTestServer(t)
	base := strings.TrimSuffix(url, "/")
	resp, err := http.Get(base + "/../etc/passwd")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Errorf("path traversal returned 200")
	}
}
