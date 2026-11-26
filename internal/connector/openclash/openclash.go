// Package openclash collects a bounded current view from an OpenClash controller.
package openclash

import (
	"context"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/galendai/homepi-mon/internal/config"
	"github.com/galendai/homepi-mon/internal/connector"
	"github.com/galendai/homepi-mon/internal/connector/providerutil"
	"github.com/galendai/homepi-mon/internal/protocol"
	"github.com/galendai/homepi-mon/internal/providermeta"
	"github.com/galendai/homepi-mon/internal/secretstore"
)

const (
	typeID              = "openclash"
	defaultSelector     = "🚀 PROXY"
	pollInterval        = 5 * time.Second
	delayRequestTimeout = 5 * time.Second
	delayTestURL        = "http://www.gstatic.com/generate_204"
)

var builtInProxies = map[string]bool{
	"DIRECT": true, "REJECT": true, "REJECT-DROP": true,
	"PASS": true, "PASS-RULE": true, "COMPATIBLE": true,
}

func init() {
	connector.Register(typeID, factory)
	providermeta.Register(providermeta.TypeMeta{
		TypeID: typeID, Label: "OpenClash", Provider: "openclash",
		RequiresSecret: true, SecretFieldLabel: "OpenClash API Secret",
		MinInterval: pollInterval, MinStaleAfter: pollInterval,
		SupportedRegions: []string{"custom"}, DefaultRegion: "custom",
		DefaultBaseURL: "http://127.0.0.1:9090",
		Description:    "Current OpenClash exit, proxy delays and active connections.",
	})
}

// Connector retains only the latest complete report and each node's latest
// daemon-initiated delay check. Scheduler serialization prevents overlapping
// collections; delay checks are bounded to two goroutines per collection.
type Connector struct {
	spec    config.ProviderConfig
	secrets secretstore.Store
	runtime providerutil.Runtime

	mu     sync.RWMutex
	report *protocol.OpenClashReport
}

type settings struct {
	baseURL  string
	selector string
	interval time.Duration
}

// New builds a connector with injectable HTTP and clock dependencies.
func New(spec config.ProviderConfig, secrets secretstore.Store, runtime providerutil.Runtime) *Connector {
	return &Connector{spec: spec, secrets: secrets, runtime: providerutil.NormalizeRuntime(runtime)}
}

func factory(spec config.ProviderConfig, secrets secretstore.Store) (connector.Connector, error) {
	return New(spec, secrets, providerutil.Runtime{}), nil
}

func (c *Connector) ID() string       { return c.spec.ID }
func (c *Connector) Provider() string { return typeID }

func (c *Connector) ValidateConfig() error {
	_, err := c.settings()
	return err
}

func (c *Connector) settings() (settings, error) {
	for key := range c.spec.Options {
		if key != "selector" {
			return settings{}, connector.Errorf(protocol.ErrInvalidConfig, "OpenClash option %q is not allowed", key)
		}
	}
	if c.spec.Region != "custom" || c.spec.SecretRef == "" || c.spec.AuthFile != "" {
		return settings{}, connector.Errorf(protocol.ErrInvalidConfig, "OpenClash requires region=custom and secret_ref")
	}
	u, err := url.Parse(strings.TrimSpace(c.spec.BaseURL))
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		(u.Scheme != "http" && u.Scheme != "https") || (u.Path != "" && u.Path != "/") {
		return settings{}, connector.Errorf(protocol.ErrInvalidConfig, "OpenClash base URL must be an HTTP(S) origin")
	}
	ip := net.ParseIP(u.Hostname())
	if ip != nil && (ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast()) {
		return settings{}, connector.Errorf(protocol.ErrInvalidConfig, "OpenClash base URL must not use a link-local address")
	}
	if u.Scheme == "http" && (ip == nil || (!ip.IsPrivate() && !ip.IsLoopback())) {
		return settings{}, connector.Errorf(protocol.ErrInvalidConfig, "OpenClash HTTP base URL requires a literal private or loopback IP")
	}
	interval := pollInterval
	if raw := strings.TrimSpace(c.spec.Interval); raw != "" {
		parsed, parseErr := time.ParseDuration(raw)
		if parseErr != nil || parsed < pollInterval {
			return settings{}, connector.Errorf(protocol.ErrInvalidConfig, "OpenClash interval must be at least 5s")
		}
		interval = parsed
	}
	selector := defaultSelector
	if raw, ok := c.spec.Options["selector"]; ok {
		selector = strings.TrimSpace(raw)
	}
	if selector == "" || len(selector) > 128 || strings.ContainsFunc(selector, func(r rune) bool {
		return r < 0x20 || r == 0x7f
	}) {
		return settings{}, connector.Errorf(protocol.ErrInvalidConfig, "OpenClash selector is invalid")
	}
	return settings{baseURL: u.Scheme + "://" + u.Host, selector: selector, interval: interval}, nil
}

type proxy struct {
	Type string   `json:"type"`
	Now  string   `json:"now"`
	All  []string `json:"all"`
}

type proxiesResponse struct {
	Proxies map[string]proxy `json:"proxies"`
}

type connectionsResponse struct {
	Connections []struct {
		Chains []string `json:"chains"`
	} `json:"connections"`
}

// openClashAuthError replaces the generic provider HTTP auth wording with the
// action that actually repairs an OpenClash collection: update the node keyring.
func openClashAuthError(err error) error {
	if connector.Classify(err) != protocol.ErrAuth {
		return err
	}
	return connector.Errorf(protocol.ErrAuth, "update the OpenClash API secret in the node keyring")
}

// Collect fetches both controller endpoints and checks the two stalest nodes.
// It performs no retries and starts no work that outlives this call.
func (c *Connector) Collect(ctx context.Context) ([]protocol.ProviderMetric, error) {
	cfg, err := c.settings()
	if err != nil {
		return nil, err
	}
	token, err := providerutil.Secret(ctx, c.spec, c.secrets)
	if err != nil {
		return nil, err
	}
	var proxies proxiesResponse
	if err := connector.GetJSON(ctx, connector.JSONRequest{
		Client: c.runtime.HTTPClient, URL: cfg.baseURL + "/proxies", BearerToken: token,
	}, &proxies); err != nil {
		return nil, openClashAuthError(err)
	}
	if proxies.Proxies == nil {
		return nil, connector.Errorf(protocol.ErrSchemaChanged, "OpenClash proxies response is missing")
	}
	var connections connectionsResponse
	if err := connector.GetJSON(ctx, connector.JSONRequest{
		Client: c.runtime.HTTPClient, URL: cfg.baseURL + "/connections", BearerToken: token,
	}, &connections); err != nil {
		return nil, openClashAuthError(err)
	}

	now := c.runtime.Now().UTC()
	c.mu.RLock()
	var previous protocol.OpenClashReport
	if c.report != nil {
		previous = c.report.Clone()
	}
	c.mu.RUnlock()
	report, err := buildReport(cfg, proxies, connections, previous, now)
	if err != nil {
		return nil, err
	}
	if err := c.checkDelays(ctx, cfg.baseURL, token, report.Nodes); err != nil {
		return nil, openClashAuthError(err)
	}
	report.ObservedAt = c.runtime.Now().UTC()
	if err := report.Validate(); err != nil {
		return nil, connector.Errorf(protocol.ErrSchemaChanged, "OpenClash response schema changed")
	}
	stored := report.Clone()
	c.mu.Lock()
	c.report = &stored
	c.mu.Unlock()
	return []protocol.ProviderMetric{}, nil
}

// OpenClash returns the latest complete report without sharing pointers.
func (c *Connector) OpenClash() (protocol.OpenClashReport, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.report == nil {
		return protocol.OpenClashReport{}, connector.Errorf(protocol.ErrSchemaChanged, "OpenClash report is unavailable")
	}
	return c.report.Clone(), nil
}

func buildReport(cfg settings, proxies proxiesResponse, connections connectionsResponse,
	previous protocol.OpenClashReport, now time.Time) (protocol.OpenClashReport, error) {
	names := make([]string, 0, len(proxies.Proxies))
	realNodes := make(map[string]bool, len(proxies.Proxies))
	for name, item := range proxies.Proxies {
		if len(item.All) != 0 || builtInProxies[strings.ToUpper(name)] ||
			builtInProxies[strings.ToUpper(item.Type)] {
			continue
		}
		if name == "" || len(name) > 128 || strings.ContainsAny(name, "\x00\r\n") {
			return protocol.OpenClashReport{}, connector.Errorf(protocol.ErrSchemaChanged, "OpenClash proxy name is invalid")
		}
		if !realNodes[name] {
			realNodes[name] = true
			names = append(names, name)
		}
	}
	sort.Strings(names)
	selectedNode := resolveSelection(proxies.Proxies, cfg.selector)
	delayStaleAfter := time.Duration((len(names)+1)/2) * 2 * cfg.interval
	if delayStaleAfter == 0 {
		delayStaleAfter = 2 * cfg.interval
	}
	previousNodes := make(map[string]protocol.OpenClashNode, len(previous.Nodes))
	for _, node := range previous.Nodes {
		previousNodes[node.Name] = node
	}
	if len(names) > protocol.MaxOpenClashNodes {
		return protocol.OpenClashReport{}, connector.Errorf(protocol.ErrSchemaChanged, "OpenClash proxy count exceeds protocol limit")
	}
	active := make(map[string]int, len(realNodes))
	for _, connection := range connections.Connections {
		if len(connection.Chains) > 0 && realNodes[connection.Chains[0]] {
			active[connection.Chains[0]]++
		}
	}
	report := protocol.OpenClashReport{
		ObservedAt: now, StaleAfter: protocol.Duration(3 * cfg.interval),
		DelayStaleAfter: protocol.Duration(delayStaleAfter), SelectedGroup: cfg.selector,
		SelectedNode: selectedNode, TotalConnections: len(connections.Connections),
		Nodes: make([]protocol.OpenClashNode, 0, len(names)), Status: protocol.StatusOK,
	}
	for _, name := range names {
		node := protocol.OpenClashNode{Name: name, ActiveConnections: active[name]}
		if old, ok := previousNodes[name]; ok {
			node.CheckedAt = cloneTime(old.CheckedAt)
			if old.CheckedAt != nil && now.Sub(*old.CheckedAt) <= delayStaleAfter {
				node.Alive = cloneBool(old.Alive)
				if old.DelayMS != nil {
					delay := *old.DelayMS
					node.DelayMS = &delay
				}
			}
		}
		report.Nodes = append(report.Nodes, node)
	}
	return report, nil
}

func resolveSelection(proxies map[string]proxy, selector string) string {
	group, ok := proxies[selector]
	if !ok || len(group.All) == 0 || group.Now == "" {
		return ""
	}
	selected := group.Now
	seen := map[string]bool{selector: true}
	for {
		if seen[selected] {
			return ""
		}
		seen[selected] = true
		next, ok := proxies[selected]
		if !ok || len(next.All) == 0 {
			return selected
		}
		if next.Now == "" {
			return ""
		}
		selected = next.Now
	}
}

func (c *Connector) checkDelays(ctx context.Context, baseURL, token string, nodes []protocol.OpenClashNode) error {
	count := len(nodes)
	if count > 2 {
		count = 2
	}
	if count == 0 {
		return nil
	}
	order := make([]int, len(nodes))
	for i := range nodes {
		order[i] = i
	}
	sort.Slice(order, func(i, j int) bool {
		left, right := nodes[order[i]].CheckedAt, nodes[order[j]].CheckedAt
		if (left == nil) != (right == nil) {
			return left == nil
		}
		if left != nil && !left.Equal(*right) {
			return left.Before(*right)
		}
		return nodes[order[i]].Name < nodes[order[j]].Name
	})
	var wg sync.WaitGroup
	fatal := make(chan error, count)
	for _, index := range order[:count] {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			delay, err := c.measureDelay(ctx, baseURL, token, nodes[index].Name)
			checkedAt := c.runtime.Now().UTC()
			nodes[index].CheckedAt = &checkedAt
			falseValue := false
			if err != nil {
				nodes[index].Alive = &falseValue
				nodes[index].DelayMS = nil
				class := connector.Classify(err)
				if class == protocol.ErrAuth || class == protocol.ErrRateLimited {
					fatal <- err
				}
				return
			}
			trueValue := true
			nodes[index].Alive = &trueValue
			nodes[index].DelayMS = &delay
		}(index)
	}
	wg.Wait()
	close(fatal)
	for err := range fatal {
		return err
	}
	return nil
}

func (c *Connector) measureDelay(ctx context.Context, baseURL, token, name string) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, delayRequestTimeout)
	defer cancel()
	query := url.Values{}
	query.Set("timeout", strconv.FormatInt(delayRequestTimeout.Milliseconds(), 10))
	query.Set("url", delayTestURL)
	endpoint := baseURL + "/proxies/" + url.PathEscape(name) + "/delay?" + query.Encode()
	var payload struct {
		Delay int `json:"delay"`
	}
	if err := connector.GetJSON(ctx, connector.JSONRequest{
		Client: c.runtime.HTTPClient, URL: endpoint, BearerToken: token,
	}, &payload); err != nil {
		return 0, err
	}
	if payload.Delay <= 0 || payload.Delay > 120000 {
		return 0, connector.Errorf(protocol.ErrSchemaChanged, "OpenClash delay response is invalid")
	}
	return payload.Delay, nil
}

func cloneBool(value *bool) *bool {
	if value == nil {
		return nil
	}
	out := *value
	return &out
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	out := *value
	return &out
}

var _ connector.Connector = (*Connector)(nil)
var _ connector.OpenClashReporter = (*Connector)(nil)
