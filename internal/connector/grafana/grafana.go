package grafana

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
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

const typeID = "grafana"

var namespacePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

func init() {
	connector.Register(typeID, factory)
	providermeta.Register(providermeta.TypeMeta{
		TypeID: typeID, Label: "Grafana", Provider: "grafana",
		RequiresSecret: true, SecretFieldLabel: "Grafana service account token",
		MinInterval: 30 * time.Second, MinStaleAfter: time.Minute,
		SupportedRegions: []string{"custom"}, DefaultRegion: "custom",
		DefaultBaseURL: "https://grafana.local",
		Description:    "Read-only health, version, alert summary and API generation probe.",
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
	base      string
	namespace string
	maxAlerts int
}

func New(spec config.ProviderConfig, secrets secretstore.Store, runtime providerutil.Runtime) *Connector {
	return &Connector{spec: spec, secrets: secrets, runtime: providerutil.NormalizeRuntime(runtime)}
}

func factory(spec config.ProviderConfig, secrets secretstore.Store) (connector.Connector, error) {
	return New(spec, secrets, providerutil.Runtime{}), nil
}

func (c *Connector) ID() string            { return c.spec.ID }
func (c *Connector) Provider() string      { return typeID }
func (c *Connector) ValidateConfig() error { _, err := c.settings(); return err }

func (c *Connector) settings() (settings, error) {
	for key := range c.spec.Options {
		if key != "namespace" && key != "max_alerts" {
			return settings{}, connector.Errorf(protocol.ErrInvalidConfig, "Grafana option %q is not allowed", key)
		}
	}
	if c.spec.SecretRef == "" {
		return settings{}, connector.Errorf(protocol.ErrInvalidConfig, "Grafana secret_ref is required")
	}
	base, err := providerutil.BaseURL(c.spec, "", "")
	if err != nil {
		return settings{}, err
	}
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return settings{}, connector.Errorf(protocol.ErrInvalidConfig, "Grafana base URL must be an HTTPS origin")
	}
	namespace := option(c.spec.Options, "namespace", "default")
	if !namespacePattern.MatchString(namespace) {
		return settings{}, connector.Errorf(protocol.ErrInvalidConfig, "Grafana namespace is invalid")
	}
	maxAlerts, err := integerOption(c.spec.Options, "max_alerts", 256, 1, 1000)
	if err != nil {
		return settings{}, err
	}
	return settings{base: strings.TrimRight(base, "/"), namespace: namespace, maxAlerts: maxAlerts}, nil
}

func (c *Connector) Collect(ctx context.Context) ([]protocol.ProviderMetric, error) {
	settings, err := c.settings()
	if err != nil {
		return nil, err
	}
	token, err := providerutil.Secret(ctx, c.spec, c.secrets)
	if err != nil {
		return nil, err
	}
	var health struct {
		Commit   string `json:"commit"`
		Database string `json:"database"`
		Version  string `json:"version"`
	}
	if err := c.get(ctx, settings.base+"/api/health", token, &health); err != nil {
		return nil, err
	}
	if health.Version == "" || len(health.Version) > 64 || len(health.Commit) > 128 {
		return nil, connector.Errorf(protocol.ErrSchemaChanged, "Grafana health response is invalid")
	}
	var alerts []struct {
		Labels map[string]string `json:"labels"`
		Status struct {
			State string `json:"state"`
		} `json:"status"`
	}
	if err := c.get(ctx, settings.base+"/api/alertmanager/grafana/api/v2/alerts", token, &alerts); err != nil {
		return nil, err
	}
	if len(alerts) > settings.maxAlerts {
		return nil, connector.Errorf(protocol.ErrInvalidConfig, "Grafana alert count exceeded limit")
	}
	firing, critical := 0, false
	for _, alert := range alerts {
		if alert.Status.State == "" || strings.EqualFold(alert.Status.State, "active") || strings.EqualFold(alert.Status.State, "firing") {
			firing++
			if strings.EqualFold(alert.Labels["severity"], "critical") {
				critical = true
			}
		}
	}
	generation, rules, err := c.probeRules(ctx, settings, token)
	if err != nil {
		return nil, err
	}
	healthy := strings.EqualFold(health.Database, "ok")
	status := protocol.StatusOK
	message := "rules=" + strconv.Itoa(rules)
	if !healthy || critical {
		status = protocol.StatusCritical
	} else if firing > 0 {
		status = protocol.StatusWarning
	}
	report := protocol.HomeLabReport{Services: []protocol.HomeLabService{{
		ID: c.spec.ID + ".service", Name: c.spec.AccountLabel, Kind: typeID,
		Version: health.Version, APIGeneration: generation, Healthy: &healthy,
		FiringAlerts: &firing, ObservedAt: c.runtime.Now().UTC(), Status: status,
		Message: message, Order: 20,
	}}}
	if err := report.Services[0].Validate(); err != nil {
		return nil, connector.Errorf(protocol.ErrSchemaChanged, "Grafana service summary is invalid")
	}
	c.mu.Lock()
	c.report = report.Clone()
	c.mu.Unlock()
	return nil, nil
}

func (c *Connector) probeRules(ctx context.Context, settings settings, token string) (string, int, error) {
	var modern struct {
		Items []json.RawMessage `json:"items"`
	}
	modernURL := settings.base + "/apis/rules.alerting.grafana.app/v0alpha1/namespaces/" + url.PathEscape(settings.namespace) + "/alertrules"
	err := c.get(ctx, modernURL, token, &modern)
	if err == nil {
		if len(modern.Items) > settings.maxAlerts {
			return "", 0, connector.Errorf(protocol.ErrInvalidConfig, "Grafana rule count exceeded limit")
		}
		return "apis-v0alpha1", len(modern.Items), nil
	}
	if connector.StatusCode(err) != 404 {
		return "", 0, err
	}
	var legacy []json.RawMessage
	if err := c.get(ctx, settings.base+"/api/v1/provisioning/alert-rules", token, &legacy); err != nil {
		return "", 0, err
	}
	if len(legacy) > settings.maxAlerts {
		return "", 0, connector.Errorf(protocol.ErrInvalidConfig, "Grafana rule count exceeded limit")
	}
	return "api-legacy", len(legacy), nil
}

func (c *Connector) get(ctx context.Context, target, token string, dst any) error {
	return connector.GetJSON(ctx, connector.JSONRequest{Client: c.runtime.HTTPClient, URL: target, BearerToken: token}, dst)
}

func (c *Connector) HomeLab() (protocol.HomeLabReport, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.report.Clone(), nil
}

func option(values map[string]string, key, fallback string) string {
	if value, ok := values[key]; ok {
		return value
	}
	return fallback
}
func integerOption(values map[string]string, key string, fallback, min, max int) (int, error) {
	raw, ok := values[key]
	if !ok {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < min || value > max {
		return 0, connector.Errorf(protocol.ErrInvalidConfig, "Grafana %s must be %d..%d", key, min, max)
	}
	return value, nil
}

var _ connector.Connector = (*Connector)(nil)
var _ connector.HomeLabReporter = (*Connector)(nil)
