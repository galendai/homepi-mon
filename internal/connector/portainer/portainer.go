package portainer

import (
	"context"
	"encoding/json"
	"net/url"
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

const typeID = "portainer"

func init() {
	connector.Register(typeID, factory)
	providermeta.Register(providermeta.TypeMeta{
		TypeID: typeID, Label: "Portainer", Provider: "portainer",
		RequiresSecret: true, SecretFieldLabel: "Portainer access token",
		MinInterval: 30 * time.Second, MinStaleAfter: time.Minute,
		SupportedRegions: []string{"custom"}, DefaultRegion: "custom",
		DefaultBaseURL: "https://portainer.local:9443",
		Description:    "Read-only version, environment, stack and container summary.",
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
	base            string
	maxEnvironments int
	maxContainers   int
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
		if key != "max_environments" && key != "max_containers" {
			return settings{}, connector.Errorf(protocol.ErrInvalidConfig, "Portainer option %q is not allowed", key)
		}
	}
	if c.spec.SecretRef == "" {
		return settings{}, connector.Errorf(protocol.ErrInvalidConfig, "Portainer secret_ref is required")
	}
	base, err := providerutil.BaseURL(c.spec, "", "")
	if err != nil {
		return settings{}, err
	}
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return settings{}, connector.Errorf(protocol.ErrInvalidConfig, "Portainer base URL must be an HTTPS origin")
	}
	maxEnvironments, err := intOption(c.spec.Options, "max_environments", 4, 1, 7)
	if err != nil {
		return settings{}, err
	}
	maxContainers, err := intOption(c.spec.Options, "max_containers", 1000, 1, 5000)
	if err != nil {
		return settings{}, err
	}
	return settings{base: strings.TrimRight(base, "/"), maxEnvironments: maxEnvironments, maxContainers: maxContainers}, nil
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
	version, generation, err := c.status(ctx, settings.base, token)
	if err != nil {
		return nil, err
	}
	var environments []struct {
		ID     int    `json:"Id"`
		Name   string `json:"Name"`
		Status int    `json:"Status"`
	}
	if err := c.get(ctx, settings.base+"/api/endpoints", token, &environments); err != nil {
		return nil, err
	}
	if len(environments) > settings.maxEnvironments {
		return nil, connector.Errorf(protocol.ErrInvalidConfig, "Portainer environment count exceeded limit")
	}
	var stacks []json.RawMessage
	if err := c.get(ctx, settings.base+"/api/stacks", token, &stacks); err != nil {
		return nil, err
	}
	online, running, stopped, failed := 0, 0, 0, 0
	for _, environment := range environments {
		if environment.ID <= 0 || environment.Name == "" || len(environment.Name) > 64 {
			return nil, connector.Errorf(protocol.ErrSchemaChanged, "Portainer environment response is invalid")
		}
		if environment.Status == 1 {
			online++
		} else {
			continue
		}
		var containers []struct {
			State string `json:"State"`
		}
		target := settings.base + "/api/endpoints/" + strconv.Itoa(environment.ID) + "/docker/containers/json?all=true"
		if err := c.get(ctx, target, token, &containers); err != nil {
			return nil, err
		}
		if len(containers) > settings.maxContainers {
			return nil, connector.Errorf(protocol.ErrInvalidConfig, "Portainer container count exceeded limit")
		}
		for _, container := range containers {
			switch strings.ToLower(container.State) {
			case "running":
				running++
			case "dead", "removing":
				failed++
			default:
				stopped++
			}
		}
	}
	healthy := true
	status := protocol.StatusOK
	if failed > 0 {
		status = protocol.StatusCritical
	} else if online < len(environments) || stopped > 0 {
		status = protocol.StatusWarning
	}
	total := len(environments)
	stackCount := len(stacks)
	report := protocol.HomeLabReport{Services: []protocol.HomeLabService{{
		ID: c.spec.ID + ".service", Name: c.spec.AccountLabel, Kind: typeID,
		Version: version, APIGeneration: generation, Healthy: &healthy,
		EnvironmentsTotal: &total, EnvironmentsOnline: &online,
		ContainersRunning: &running, ContainersStopped: &stopped,
		ContainersFailed: &failed, Stacks: &stackCount,
		ObservedAt: c.runtime.Now().UTC(), Status: status, Order: 30,
	}}}
	if err := report.Services[0].Validate(); err != nil {
		return nil, connector.Errorf(protocol.ErrSchemaChanged, "Portainer service summary is invalid")
	}
	c.mu.Lock()
	c.report = report.Clone()
	c.mu.Unlock()
	return nil, nil
}

func (c *Connector) status(ctx context.Context, base, token string) (string, string, error) {
	var modern struct {
		Version string `json:"Version"`
	}
	err := c.get(ctx, base+"/api/system/status", token, &modern)
	if err == nil {
		if modern.Version == "" || len(modern.Version) > 64 {
			return "", "", connector.Errorf(protocol.ErrSchemaChanged, "Portainer system status is invalid")
		}
		return modern.Version, "system-status", nil
	}
	if connector.StatusCode(err) != 404 {
		return "", "", err
	}
	var legacy struct {
		Version string `json:"Version"`
	}
	if err := c.get(ctx, base+"/api/status", token, &legacy); err != nil {
		return "", "", err
	}
	if legacy.Version == "" || len(legacy.Version) > 64 {
		return "", "", connector.Errorf(protocol.ErrSchemaChanged, "Portainer legacy status is invalid")
	}
	return legacy.Version, "legacy-status", nil
}

func (c *Connector) get(ctx context.Context, target, token string, dst any) error {
	return connector.GetJSON(ctx, connector.JSONRequest{
		Client: c.runtime.HTTPClient, URL: target, Headers: map[string]string{"X-API-Key": token},
	}, dst)
}

func (c *Connector) HomeLab() (protocol.HomeLabReport, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.report.Clone(), nil
}
func intOption(values map[string]string, key string, fallback, min, max int) (int, error) {
	raw, ok := values[key]
	if !ok {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < min || value > max {
		return 0, connector.Errorf(protocol.ErrInvalidConfig, "Portainer %s must be %d..%d", key, min, max)
	}
	return value, nil
}

var _ connector.Connector = (*Connector)(nil)
var _ connector.HomeLabReporter = (*Connector)(nil)
