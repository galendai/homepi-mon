package openclash

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/galendai/homepi-mon/internal/config"
	"github.com/galendai/homepi-mon/internal/connector"
	"github.com/galendai/homepi-mon/internal/connector/providerutil"
	"github.com/galendai/homepi-mon/internal/protocol"
	"github.com/galendai/homepi-mon/internal/secretstore"
)

const testSecret = "openclash-secret-that-must-not-leak"

type memorySecrets struct{ value string }

func (s memorySecrets) Get(context.Context, string) (string, error) { return s.value, nil }
func (s memorySecrets) Set(context.Context, string, string) error   { return nil }
func (s memorySecrets) Delete(context.Context, string) error        { return nil }
func (s memorySecrets) Backend() string                             { return "memory" }

var _ secretstore.Store = memorySecrets{}

type controllerFixture struct {
	t *testing.T

	mu            sync.Mutex
	proxies       string
	connections   string
	delayStatus   int
	delayRequests []string
	delayHook     http.HandlerFunc
}

func newControllerFixture(t *testing.T) *controllerFixture {
	return &controllerFixture{
		t: t, connections: `{"connections":[]}`, delayStatus: http.StatusOK,
		proxies: `{"proxies":{"🚀 PROXY":{"type":"Selector","now":"node-a","all":["node-a"]},"node-a":{"type":"Shadowsocks","alive":true}}}`,
	}
}

func (f *controllerFixture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer "+testSecret {
		f.t.Errorf("unexpected method or credential: %s %q", r.Method, r.Header.Get("Authorization"))
	}
	f.mu.Lock()
	proxies, connections, delayStatus, delayHook := f.proxies, f.connections, f.delayStatus, f.delayHook
	if strings.HasPrefix(r.URL.Path, "/proxies/") {
		f.delayRequests = append(f.delayRequests, r.URL.EscapedPath())
	}
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.URL.Path == "/proxies":
		_, _ = w.Write([]byte(proxies))
	case r.URL.Path == "/connections":
		_, _ = w.Write([]byte(connections))
	case strings.HasPrefix(r.URL.Path, "/proxies/") && strings.HasSuffix(r.URL.Path, "/delay"):
		if delayHook != nil {
			delayHook(w, r)
			return
		}
		w.WriteHeader(delayStatus)
		if delayStatus == http.StatusOK {
			if r.URL.Query().Get("timeout") != "5000" || r.URL.Query().Get("url") != delayTestURL {
				f.t.Errorf("delay query = %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"delay":123}`))
		} else {
			_, _ = w.Write([]byte(`{"message":"` + testSecret + `"}`))
		}
	default:
		http.NotFound(w, r)
	}
}

func (f *controllerFixture) setProxies(body string) {
	f.mu.Lock()
	f.proxies = body
	f.mu.Unlock()
}

func (f *controllerFixture) delays() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.delayRequests...)
}

func openClashTestConnector(serverURL string, now *time.Time, client *http.Client) *Connector {
	return New(config.ProviderConfig{
		ID: "clash", Type: typeID, AccountLabel: "router", Region: "custom",
		BaseURL: serverURL, SecretRef: "keyring:openclash",
	}, memorySecrets{value: testSecret}, providerutil.Runtime{
		HTTPClient: client, Now: func() time.Time { return *now },
	})
}

func collectReport(t *testing.T, c *Connector) protocol.OpenClashReport {
	t.Helper()
	metrics, err := c.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(metrics) != 0 {
		t.Fatalf("unexpected Provider metrics: %+v", metrics)
	}
	report, err := c.OpenClash()
	if err != nil {
		t.Fatal(err)
	}
	if err := report.Validate(); err != nil {
		t.Fatal(err)
	}
	return report
}

func TestCollectFiltersBuiltinsResolvesChainAndEscapesUnicode(t *testing.T) {
	fixture := newControllerFixture(t)
	fixture.proxies = `{"proxies":{
		"🚀 PROXY":{"type":"Selector","now":"🛰 自动","all":["🛰 自动","香港 🇭🇰"]},
		"🛰 自动":{"type":"URLTest","now":"香港 🇭🇰","all":["香港 🇭🇰","美国 01"]},
		"香港 🇭🇰":{"type":"Shadowsocks","alive":true,"history":[{"delay":9999}]},
		"美国 01":{"type":"VMess","alive":true,"history":[{"delay":8888}]},
		"DIRECT":{"type":"Direct","alive":true},"REJECT":{"type":"Reject"},
		"REJECT-DROP":{"type":"RejectDrop"},"PASS":{"type":"Pass"},
		"PASS-RULE":{"type":"Pass"},"COMPATIBLE":{"type":"Compatible"}
	}}`
	fixture.connections = `{"connections":[
		{"chains":["香港 🇭🇰","🛰 自动"],"metadata":{"secret":"discard"}},
		{"chains":["香港 🇭🇰","🚀 PROXY"]},{"chains":["美国 01","🚀 PROXY"]},
		{"chains":["DIRECT"]}
	]}`
	server := httptest.NewServer(fixture)
	defer server.Close()
	now := time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)
	report := collectReport(t, openClashTestConnector(server.URL, &now, server.Client()))
	if report.SelectedGroup != defaultSelector || report.SelectedNode != "香港 🇭🇰" ||
		report.TotalConnections != 4 || len(report.Nodes) != 2 {
		t.Fatalf("report = %+v", report)
	}
	for _, node := range report.Nodes {
		if node.DelayMS == nil || *node.DelayMS != 123 {
			t.Fatalf("node used upstream history or missed live delay: %+v", node)
		}
	}
	if node := nodeByName(report.Nodes, "香港 🇭🇰"); node == nil || node.ActiveConnections != 2 {
		t.Fatalf("Hong Kong connection aggregation = %+v", node)
	}
	if node := nodeByName(report.Nodes, "美国 01"); node == nil || node.ActiveConnections != 1 {
		t.Fatalf("US connection aggregation = %+v", node)
	}
	delays := fixture.delays()
	unicodePath := "/proxies/" + url.PathEscape("香港 🇭🇰") + "/delay"
	if len(delays) != 2 || (delays[0] != unicodePath && delays[1] != unicodePath) {
		t.Fatalf("escaped delay paths = %q", delays)
	}
}

func TestSelectionChainDetectsCycle(t *testing.T) {
	fixture := newControllerFixture(t)
	fixture.proxies = `{"proxies":{
		"🚀 PROXY":{"type":"Selector","now":"loop","all":["loop"]},
		"loop":{"type":"Selector","now":"🚀 PROXY","all":["🚀 PROXY"]},
		"node-a":{"type":"Shadowsocks"}
	}}`
	server := httptest.NewServer(fixture)
	defer server.Close()
	now := time.Now().UTC()
	report := collectReport(t, openClashTestConnector(server.URL, &now, server.Client()))
	if report.SelectedNode != "" {
		t.Fatalf("cyclic selection resolved to %q", report.SelectedNode)
	}
}

func TestDelayRotationPrioritizesNewAndOldestAndHandlesMembershipChanges(t *testing.T) {
	fixture := newControllerFixture(t)
	fixture.proxies = `{"proxies":{"🚀 PROXY":{"type":"Selector","now":"node-a","all":["node-a","node-b","node-c"]},
		"node-a":{"type":"Shadowsocks"},"node-b":{"type":"Shadowsocks"},"node-c":{"type":"Shadowsocks","alive":false}}}`
	server := httptest.NewServer(fixture)
	defer server.Close()
	now := time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)
	c := openClashTestConnector(server.URL, &now, server.Client())
	first := collectReport(t, c)
	if unmeasured := nodeByName(first.Nodes, "node-c"); unmeasured.Alive != nil || unmeasured.CheckedAt != nil {
		t.Fatalf("unmeasured node inherited upstream alive state: %+v", unmeasured)
	}
	now = now.Add(5 * time.Second)
	second := collectReport(t, c)
	if nodeByName(second.Nodes, "node-c").CheckedAt == nil {
		t.Fatal("new unmeasured node was not checked in the second round")
	}
	now = now.Add(5 * time.Second)
	fixture.setProxies(`{"proxies":{"🚀 PROXY":{"type":"Selector","now":"node-b","all":["node-b","node-c","node-d"]},
		"node-b":{"type":"Shadowsocks"},"node-c":{"type":"Shadowsocks"},"node-d":{"type":"Shadowsocks"}}}`)
	third := collectReport(t, c)
	if len(third.Nodes) != 3 || nodeByName(third.Nodes, "node-a") != nil {
		t.Fatalf("removed node remains: %+v", third.Nodes)
	}
	if nodeByName(third.Nodes, "node-d").CheckedAt == nil {
		t.Fatal("added node was not prioritized")
	}
	delays := fixture.delays()
	want := [][]string{{"node-a", "node-b"}, {"node-c", "node-a"}, {"node-d", "node-b"}}
	if len(delays) != 6 {
		t.Fatalf("delay requests = %q", delays)
	}
	for round, names := range want {
		got := []string{delays[round*2], delays[round*2+1]}
		sort.Strings(got)
		sort.Strings(names)
		for i, name := range names {
			if !strings.Contains(got[i], url.PathEscape(name)) {
				t.Errorf("delay round %d = %q, want nodes %q", round, got, names)
			}
		}
	}
}

func TestOldMeasuredDelayExpiresWithoutUsingUpstreamHistory(t *testing.T) {
	fixture := newControllerFixture(t)
	fixture.proxies = `{"proxies":{"🚀 PROXY":{"type":"Selector","now":"node-a","all":["node-a","node-b","node-c"]},
		"node-a":{"type":"Shadowsocks","history":[{"delay":1}]},"node-b":{"type":"Shadowsocks"},"node-c":{"type":"Shadowsocks"}}}`
	server := httptest.NewServer(fixture)
	defer server.Close()
	now := time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)
	c := openClashTestConnector(server.URL, &now, server.Client())
	collectReport(t, c)
	now = now.Add(5 * time.Second)
	collectReport(t, c)
	now = now.Add(100 * time.Second)
	previous, err := c.OpenClash()
	if err != nil {
		t.Fatal(err)
	}
	var current proxiesResponse
	if err := json.Unmarshal([]byte(fixture.proxies), &current); err != nil {
		t.Fatal(err)
	}
	report, err := buildReport(settings{selector: defaultSelector, interval: 5 * time.Second}, current, connectionsResponse{}, previous, now)
	if err != nil {
		t.Fatal(err)
	}
	nodeA := nodeByName(report.Nodes, "node-a")
	if nodeA.DelayMS != nil || nodeA.Alive != nil || nodeA.CheckedAt == nil {
		t.Fatalf("expired measurement was retained or replaced from history: %+v", nodeA)
	}
}

func TestDelayFreshnessUsesConfiguredInterval(t *testing.T) {
	fixture := newControllerFixture(t)
	fixture.proxies = `{"proxies":{"🚀 PROXY":{"type":"Selector","now":"node-a","all":["node-a","node-b","node-c"]},
		"node-a":{"type":"Shadowsocks"},"node-b":{"type":"Shadowsocks"},"node-c":{"type":"Shadowsocks"}}}`
	server := httptest.NewServer(fixture)
	defer server.Close()
	now := time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)
	c := openClashTestConnector(server.URL, &now, server.Client())
	c.spec.Interval = "10s"
	report := collectReport(t, c)
	if got, want := report.DelayStaleAfter.D(), 40*time.Second; got != want {
		t.Fatalf("DelayStaleAfter = %s, want %s", got, want)
	}
	if got, want := report.StaleAfter.D(), 30*time.Second; got != want {
		t.Fatalf("StaleAfter = %s, want %s", got, want)
	}
}

func TestCollectRejectsMoreThanProtocolNodeLimit(t *testing.T) {
	items := make(map[string]proxy, protocol.MaxOpenClashNodes+2)
	names := make([]string, 0, protocol.MaxOpenClashNodes+1)
	for index := range protocol.MaxOpenClashNodes + 1 {
		name := fmt.Sprintf("node-%03d", index)
		names = append(names, name)
		items[name] = proxy{Type: "Shadowsocks"}
	}
	items[defaultSelector] = proxy{Type: "Selector", Now: names[0], All: names}
	body, err := json.Marshal(proxiesResponse{Proxies: items})
	if err != nil {
		t.Fatal(err)
	}
	fixture := newControllerFixture(t)
	fixture.proxies = string(body)
	server := httptest.NewServer(fixture)
	defer server.Close()
	now := time.Now().UTC()
	_, err = openClashTestConnector(server.URL, &now, server.Client()).Collect(context.Background())
	if connector.Classify(err) != protocol.ErrSchemaChanged {
		t.Fatalf("oversized proxy set error = %v", err)
	}
}

func TestCollectRejectsMissingProxiesObject(t *testing.T) {
	fixture := newControllerFixture(t)
	fixture.proxies = `{}`
	server := httptest.NewServer(fixture)
	defer server.Close()
	now := time.Now().UTC()
	_, err := openClashTestConnector(server.URL, &now, server.Client()).Collect(context.Background())
	if connector.Classify(err) != protocol.ErrSchemaChanged {
		t.Fatalf("missing proxies object error = %v", err)
	}
}

func nodeByName(nodes []protocol.OpenClashNode, name string) *protocol.OpenClashNode {
	for i := range nodes {
		if nodes[i].Name == name {
			return &nodes[i]
		}
	}
	return nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDelayFailureIsolationAndFatalClasses(t *testing.T) {
	t.Run("ordinary failure only fails node", func(t *testing.T) {
		fixture := newControllerFixture(t)
		fixture.delayStatus = http.StatusInternalServerError
		server := httptest.NewServer(fixture)
		defer server.Close()
		now := time.Now().UTC()
		report := collectReport(t, openClashTestConnector(server.URL, &now, server.Client()))
		node := report.Nodes[0]
		if node.Alive == nil || *node.Alive || node.DelayMS != nil || node.CheckedAt == nil ||
			report.Status != protocol.StatusOK {
			t.Fatalf("ordinary delay failure = %+v, report status=%s", node, report.Status)
		}
	})
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			fixture := newControllerFixture(t)
			fixture.delayStatus = status
			server := httptest.NewServer(fixture)
			defer server.Close()
			now := time.Now().UTC()
			_, err := openClashTestConnector(server.URL, &now, server.Client()).Collect(context.Background())
			want := protocol.ErrAuth
			if status == http.StatusTooManyRequests {
				want = protocol.ErrRateLimited
			}
			if connector.Classify(err) != want {
				t.Fatalf("class = %s, want %s (err=%v)", connector.Classify(err), want, err)
			}
			if strings.Contains(connector.Message(err), testSecret) || strings.Contains(err.Error(), testSecret) {
				t.Fatal("credential leaked into delay error")
			}
			if status != http.StatusTooManyRequests {
				message := connector.Message(err)
				if !strings.Contains(message, "OpenClash API secret") || strings.Contains(message, "official provider") {
					t.Fatalf("OpenClash auth guidance = %q", message)
				}
			}
		})
	}
	t.Run("delay timeout only fails node", func(t *testing.T) {
		fixture := newControllerFixture(t)
		server := httptest.NewServer(fixture)
		defer server.Close()
		client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if strings.HasSuffix(r.URL.Path, "/delay") {
				return nil, context.DeadlineExceeded
			}
			return http.DefaultTransport.RoundTrip(r)
		})}
		now := time.Now().UTC()
		report := collectReport(t, openClashTestConnector(server.URL, &now, client))
		if report.Nodes[0].Alive == nil || *report.Nodes[0].Alive || report.Nodes[0].DelayMS != nil {
			t.Fatalf("timeout node = %+v", report.Nodes[0])
		}
	})
}

func TestZeroDelayIsTreatedAsNodeFailure(t *testing.T) {
	fixture := newControllerFixture(t)
	fixture.delayHook = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"delay":0}`))
	}
	server := httptest.NewServer(fixture)
	defer server.Close()
	now := time.Now().UTC()
	report := collectReport(t, openClashTestConnector(server.URL, &now, server.Client()))
	node := report.Nodes[0]
	if node.Alive == nil || *node.Alive || node.DelayMS != nil || node.CheckedAt == nil {
		t.Fatalf("zero delay became a live measurement: %+v", node)
	}
}

func TestControllerTimeoutAndSecretRedaction(t *testing.T) {
	t.Run("controller timeout fails collection", func(t *testing.T) {
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, context.DeadlineExceeded
		})}
		now := time.Now().UTC()
		_, err := openClashTestConnector("http://127.0.0.1:9090", &now, client).Collect(context.Background())
		if connector.Classify(err) != protocol.ErrTimeout {
			t.Fatalf("class = %s, err=%v", connector.Classify(err), err)
		}
	})
	t.Run("authentication response body and token are redacted", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"token":"` + testSecret + `"}`))
		}))
		defer server.Close()
		now := time.Now().UTC()
		_, err := openClashTestConnector(server.URL, &now, server.Client()).Collect(context.Background())
		if err == nil || connector.Classify(err) != protocol.ErrAuth {
			t.Fatalf("auth error = %v", err)
		}
		if strings.Contains(err.Error(), testSecret) || strings.Contains(connector.Message(err), server.URL) {
			t.Fatalf("unsafe auth error: %v", err)
		}
		if message := connector.Message(err); !strings.Contains(message, "OpenClash API secret") ||
			strings.Contains(message, "official provider") {
			t.Fatalf("controller auth guidance = %q", message)
		}
	})
}

func TestOpenClashValidateConfig(t *testing.T) {
	now := time.Now().UTC()
	for _, baseURL := range []string{"http://127.0.0.1:9090", "http://192.168.1.1:9090", "https://router.example.com"} {
		c := openClashTestConnector(baseURL, &now, nil)
		if err := c.ValidateConfig(); err != nil {
			t.Errorf("valid config %q rejected: %v", baseURL, err)
		}
	}
	for _, baseURL := range []string{"http://localhost:9090", "http://router.lan:9090", "https://router.example.com/api"} {
		c := openClashTestConnector(baseURL, &now, nil)
		if err := c.ValidateConfig(); err == nil {
			t.Errorf("invalid config %q accepted", baseURL)
		}
	}
	controlSelector := openClashTestConnector("http://127.0.0.1:9090", &now, nil)
	controlSelector.spec.Options = map[string]string{"selector": "bad\tselector"}
	if err := controlSelector.ValidateConfig(); err == nil {
		t.Fatal("OpenClash selector with a control character accepted")
	}
}

func TestDelayChecksNeverExceedTwoConcurrentRequests(t *testing.T) {
	fixture := newControllerFixture(t)
	fixture.proxies = `{"proxies":{"🚀 PROXY":{"type":"Selector","now":"a","all":["a","b","c","d"]},
		"a":{"type":"Shadowsocks"},"b":{"type":"Shadowsocks"},"c":{"type":"Shadowsocks"},"d":{"type":"Shadowsocks"}}}`
	var mu sync.Mutex
	active, maximum, calls := 0, 0, 0
	fixture.delayHook = func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		active++
		calls++
		if active > maximum {
			maximum = active
		}
		mu.Unlock()
		time.Sleep(10 * time.Millisecond)
		mu.Lock()
		active--
		mu.Unlock()
		_, _ = w.Write([]byte(`{"delay":10}`))
	}
	server := httptest.NewServer(fixture)
	defer server.Close()
	now := time.Now().UTC()
	collectReport(t, openClashTestConnector(server.URL, &now, server.Client()))
	mu.Lock()
	defer mu.Unlock()
	if calls != 2 || maximum > 2 || active != 0 {
		t.Fatalf("calls=%d max=%d active=%d", calls, maximum, active)
	}
}

func TestSecretStoreFailureDoesNotExposeSecret(t *testing.T) {
	now := time.Now().UTC()
	c := New(config.ProviderConfig{
		ID: "clash", Type: typeID, Region: "custom", BaseURL: "http://127.0.0.1:9090", SecretRef: "keyring:clash",
	}, failingSecrets{}, providerutil.Runtime{Now: func() time.Time { return now }})
	_, err := c.Collect(context.Background())
	if err == nil || strings.Contains(err.Error(), testSecret) {
		t.Fatalf("secret store error = %v", err)
	}
}

type failingSecrets struct{}

func (failingSecrets) Get(context.Context, string) (string, error) {
	return "", errors.New(testSecret)
}
func (failingSecrets) Set(context.Context, string, string) error { return nil }
func (failingSecrets) Delete(context.Context, string) error      { return nil }
func (failingSecrets) Backend() string                           { return "failing" }
