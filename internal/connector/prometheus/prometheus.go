package prometheus

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"net/url"
	"regexp"
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

const typeID = "prometheus"

var labelName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

var queryOrder = []string{"up", "cpu", "memory", "disk", "network_rx", "network_tx", "alerts"}

var defaultQueries = map[string]string{
	"up":         `max by (instance) (up and on(instance) node_uname_info)`,
	"cpu":        `100 - (avg by (instance) (rate(node_cpu_seconds_total{mode="idle"}[5m])) * 100)`,
	"memory":     `max by (instance) ((1 - node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes) * 100)`,
	"disk":       `max by (instance) ((1 - node_filesystem_avail_bytes{fstype!~"tmpfs|devtmpfs|overlay|squashfs"} / node_filesystem_size_bytes{fstype!~"tmpfs|devtmpfs|overlay|squashfs"}) * 100)`,
	"network_rx": `sum by (instance) (rate(node_network_receive_bytes_total{device!~"lo|veth.*|docker.*|br-.*"}[5m]))`,
	"network_tx": `sum by (instance) (rate(node_network_transmit_bytes_total{device!~"lo|veth.*|docker.*|br-.*"}[5m]))`,
	"alerts":     `sum(ALERTS{alertstate="firing"}) or vector(0)`,
}

var allowedOptions = map[string]bool{
	"entity_label": true, "max_series": true, "query_timeout": true,
	"up_query": true, "cpu_query": true, "memory_query": true, "disk_query": true,
	"network_rx_query": true, "network_tx_query": true, "alerts_query": true,
}

func init() {
	connector.Register(typeID, factory)
	providermeta.Register(providermeta.TypeMeta{
		TypeID: typeID, Label: "Prometheus", Provider: "prometheus",
		RequiresSecret: false, MinInterval: 15 * time.Second, MinStaleAfter: 30 * time.Second,
		SupportedRegions: []string{"custom"}, DefaultRegion: "custom",
		DefaultBaseURL: "https://prometheus.local",
		Description:    "Read-only bounded instant PromQL summaries for node_exporter and alerts.",
	})
}

type Connector struct {
	spec    config.ProviderConfig
	secrets secretstore.Store
	runtime providerutil.Runtime
	mu      sync.RWMutex
	report  protocol.HomeLabReport
}

type settings struct {
	base        string
	entityLabel string
	maxSeries   int
	timeout     time.Duration
	queries     map[string]string
}

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
		if !allowedOptions[key] {
			return settings{}, connector.Errorf(protocol.ErrInvalidConfig, "Prometheus option %q is not allowed", key)
		}
	}
	base, err := providerutil.BaseURL(c.spec, "", "")
	if err != nil {
		return settings{}, err
	}
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return settings{}, connector.Errorf(protocol.ErrInvalidConfig, "Prometheus base URL must be an HTTPS origin")
	}
	if u.Path != "" && u.Path != "/" {
		return settings{}, connector.Errorf(protocol.ErrInvalidConfig, "Prometheus base URL must not contain a path")
	}
	entity := option(c.spec.Options, "entity_label", "instance")
	if !labelName.MatchString(entity) {
		return settings{}, connector.Errorf(protocol.ErrInvalidConfig, "Prometheus entity_label is invalid")
	}
	maxSeries, err := intOption(c.spec.Options, "max_series", 64, 1, 1000)
	if err != nil {
		return settings{}, err
	}
	timeout, err := time.ParseDuration(option(c.spec.Options, "query_timeout", "10s"))
	if err != nil || timeout < time.Second || timeout > 30*time.Second {
		return settings{}, connector.Errorf(protocol.ErrInvalidConfig, "Prometheus query_timeout must be 1s..30s")
	}
	queries := make(map[string]string, len(queryOrder))
	for _, key := range queryOrder {
		query := option(c.spec.Options, key+"_query", defaultQueries[key])
		if strings.TrimSpace(query) == "" || len(query) > 4096 || strings.ContainsAny(query, "\x00\r\n") {
			return settings{}, connector.Errorf(protocol.ErrInvalidConfig, "Prometheus %s query is invalid", key)
		}
		queries[key] = query
	}
	if len(queries) > 10 {
		return settings{}, connector.Errorf(protocol.ErrInvalidConfig, "Prometheus query budget exceeded")
	}
	return settings{base: strings.TrimRight(base, "/"), entityLabel: entity, maxSeries: maxSeries, timeout: timeout, queries: queries}, nil
}

func (c *Connector) Collect(ctx context.Context) ([]protocol.ProviderMetric, error) {
	settings, err := c.settings()
	if err != nil {
		return nil, err
	}
	token := ""
	if c.spec.SecretRef != "" {
		token, err = providerutil.Secret(ctx, c.spec, c.secrets)
		if err != nil {
			return nil, err
		}
	}
	results := make(map[string][]sample, len(queryOrder))
	for _, key := range queryOrder {
		values, queryErr := c.query(ctx, settings, key, token)
		if queryErr != nil {
			return nil, queryErr
		}
		results[key] = values
	}
	version, err := c.buildVersion(ctx, settings.base, token)
	if err != nil {
		return nil, err
	}
	report, err := buildReport(c.spec, settings, results, version, c.runtime.Now().UTC())
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.report = report.Clone()
	c.mu.Unlock()
	return nil, nil
}

func (c *Connector) HomeLab() (protocol.HomeLabReport, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.report.Clone(), nil
}

type sample struct {
	labels map[string]string
	value  float64
}

func (c *Connector) query(ctx context.Context, settings settings, key, token string) ([]sample, error) {
	values := url.Values{}
	values.Set("query", settings.queries[key])
	values.Set("timeout", settings.timeout.String())
	// Ask for one more than the accepted maximum so a server-side result limit
	// cannot silently turn an over-limit response into an apparently valid one.
	values.Set("limit", strconv.Itoa(settings.maxSeries+1))
	var payload struct {
		Status string `json:"status"`
		Data   struct {
			ResultType string `json:"resultType"`
			Result     []struct {
				Metric map[string]string `json:"metric"`
				Value  []any             `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := connector.GetJSON(ctx, connector.JSONRequest{
		Client: c.runtime.HTTPClient, URL: settings.base + "/api/v1/query?" + values.Encode(), BearerToken: token,
	}, &payload); err != nil {
		return nil, err
	}
	if payload.Status != "success" || payload.Data.ResultType != "vector" {
		return nil, connector.Errorf(protocol.ErrSchemaChanged, "Prometheus query response is not an instant vector")
	}
	if len(payload.Data.Result) > settings.maxSeries {
		return nil, connector.Errorf(protocol.ErrInvalidConfig, "Prometheus %s query exceeded series limit", key)
	}
	out := make([]sample, 0, len(payload.Data.Result))
	for _, item := range payload.Data.Result {
		if len(item.Value) != 2 {
			return nil, connector.Errorf(protocol.ErrSchemaChanged, "Prometheus sample value is invalid")
		}
		text, ok := item.Value[1].(string)
		if !ok {
			text = fmt.Sprint(item.Value[1])
		}
		value, parseErr := strconv.ParseFloat(text, 64)
		if parseErr != nil || math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, connector.Errorf(protocol.ErrSchemaChanged, "Prometheus sample value is not finite")
		}
		out = append(out, sample{labels: item.Metric, value: value})
	}
	return out, nil
}

func (c *Connector) buildVersion(ctx context.Context, base, token string) (string, error) {
	var payload struct {
		Status string `json:"status"`
		Data   struct {
			Version string `json:"version"`
		} `json:"data"`
	}
	if err := connector.GetJSON(ctx, connector.JSONRequest{Client: c.runtime.HTTPClient, URL: base + "/api/v1/status/buildinfo", BearerToken: token}, &payload); err != nil {
		return "", err
	}
	if payload.Status != "success" || payload.Data.Version == "" || len(payload.Data.Version) > 64 {
		return "", connector.Errorf(protocol.ErrSchemaChanged, "Prometheus build information is invalid")
	}
	return payload.Data.Version, nil
}

type nodeValues struct {
	name              string
	online            *bool
	cpu, memory, disk *int
	rx, tx            *int64
}

func buildReport(spec config.ProviderConfig, settings settings, results map[string][]sample, version string, now time.Time) (protocol.HomeLabReport, error) {
	nodes := map[string]*nodeValues{}
	for _, key := range queryOrder[:6] {
		seen := map[string]bool{}
		for _, item := range results[key] {
			name := item.labels[settings.entityLabel]
			if name == "" || len(name) > 64 || strings.ContainsAny(name, "\x00\r\n") {
				return protocol.HomeLabReport{}, connector.Errorf(protocol.ErrSchemaChanged, "Prometheus result is missing a safe entity label")
			}
			if seen[name] {
				return protocol.HomeLabReport{}, connector.Errorf(protocol.ErrSchemaChanged, "Prometheus query returned a duplicate entity")
			}
			seen[name] = true
			node := nodes[name]
			if node == nil {
				node = &nodeValues{name: name}
				nodes[name] = node
			}
			switch key {
			case "up":
				value := item.value >= 1
				node.online = &value
			case "cpu":
				value, err := percent(item.value)
				if err != nil {
					return protocol.HomeLabReport{}, err
				}
				node.cpu = value
			case "memory":
				value, err := percent(item.value)
				if err != nil {
					return protocol.HomeLabReport{}, err
				}
				node.memory = value
			case "disk":
				value, err := percent(item.value)
				if err != nil {
					return protocol.HomeLabReport{}, err
				}
				node.disk = value
			case "network_rx":
				value, err := rate(item.value)
				if err != nil {
					return protocol.HomeLabReport{}, err
				}
				node.rx = value
			case "network_tx":
				value, err := rate(item.value)
				if err != nil {
					return protocol.HomeLabReport{}, err
				}
				node.tx = value
			}
		}
	}
	if len(nodes) > settings.maxSeries {
		return protocol.HomeLabReport{}, connector.Errorf(protocol.ErrInvalidConfig, "Prometheus node count exceeded series limit")
	}
	names := make([]string, 0, len(nodes))
	for name := range nodes {
		names = append(names, name)
	}
	sort.Strings(names)
	report := protocol.HomeLabReport{}
	for order, name := range names {
		values := nodes[name]
		status := protocol.StatusOK
		if values.online != nil && !*values.online {
			status = protocol.StatusCritical
		} else if values.disk != nil && *values.disk >= 95 {
			status = protocol.StatusCritical
		} else if values.disk != nil && *values.disk >= 85 {
			status = protocol.StatusWarning
		}
		report.Nodes = append(report.Nodes, protocol.HomeLabNode{
			ID: entityID(spec.ID, name), Name: name, Online: values.online,
			CPUPercent: values.cpu, MemoryPercent: values.memory, DiskPercent: values.disk,
			NetworkReceiveBPS: values.rx, NetworkTransmitBPS: values.tx,
			ObservedAt: now, Status: status, Order: order,
		})
	}
	alertCount := 0
	for _, item := range results["alerts"] {
		if item.value < 0 || item.value > float64(settings.maxSeries) {
			return protocol.HomeLabReport{}, connector.Errorf(protocol.ErrSchemaChanged, "Prometheus alert count is out of range")
		}
		alertCount += int(math.Round(item.value))
	}
	healthy := true
	report.Services = []protocol.HomeLabService{{
		ID: spec.ID + ".service", Name: spec.AccountLabel, Kind: typeID, Version: version,
		APIGeneration: "v1", Healthy: &healthy, FiringAlerts: &alertCount,
		ObservedAt: now, Status: statusForAlerts(alertCount), Order: 10,
	}}
	for i := range report.Nodes {
		if err := report.Nodes[i].Validate(); err != nil {
			return protocol.HomeLabReport{}, connector.Errorf(protocol.ErrSchemaChanged, "Prometheus node summary is invalid")
		}
	}
	if err := report.Services[0].Validate(); err != nil {
		return protocol.HomeLabReport{}, connector.Errorf(protocol.ErrSchemaChanged, "Prometheus service summary is invalid")
	}
	return report, nil
}

func percent(value float64) (*int, error) {
	rounded := int(math.Round(value))
	if rounded < 0 || rounded > 100 {
		return nil, connector.Errorf(protocol.ErrSchemaChanged, "Prometheus percentage is outside 0..100")
	}
	return &rounded, nil
}
func rate(value float64) (*int64, error) {
	if value < 0 || value > math.MaxInt64 {
		return nil, connector.Errorf(protocol.ErrSchemaChanged, "Prometheus network rate is out of range")
	}
	rounded := int64(math.Round(value))
	return &rounded, nil
}
func statusForAlerts(count int) protocol.MetricStatus {
	if count > 0 {
		return protocol.StatusWarning
	}
	return protocol.StatusOK
}
func entityID(prefix, name string) string {
	sum := sha256.Sum256([]byte(name))
	return prefix + ".node." + hex.EncodeToString(sum[:6])
}
func option(values map[string]string, key, fallback string) string {
	if value, ok := values[key]; ok {
		return value
	}
	return fallback
}
func intOption(values map[string]string, key string, fallback, min, max int) (int, error) {
	raw, ok := values[key]
	if !ok {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < min || value > max {
		return 0, connector.Errorf(protocol.ErrInvalidConfig, "Prometheus %s must be %d..%d", key, min, max)
	}
	return value, nil
}

var _ connector.Connector = (*Connector)(nil)
var _ connector.HomeLabReporter = (*Connector)(nil)
