// Package openmeteo collects current weather from the public Open-Meteo API.
package openmeteo

import (
	"context"
	"math"
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

const (
	typeID         = "openmeteo"
	defaultBaseURL = "https://api.open-meteo.com/v1/forecast"
)

var allowedOptions = map[string]bool{
	"latitude": true, "longitude": true, "location": true, "timezone": true,
}

var weatherConditions = map[int]string{
	0: "CLEAR", 1: "MAINLY CLEAR", 2: "PARTLY CLOUDY", 3: "OVERCAST",
	45: "FOG", 48: "RIME FOG",
	51: "LIGHT DRIZZLE", 53: "DRIZZLE", 55: "HEAVY DRIZZLE",
	56: "FREEZING DRIZZLE", 57: "HEAVY FRZ DRIZZLE",
	61: "LIGHT RAIN", 63: "RAIN", 65: "HEAVY RAIN",
	66: "FREEZING RAIN", 67: "HEAVY FRZ RAIN",
	71: "LIGHT SNOW", 73: "SNOW", 75: "HEAVY SNOW", 77: "SNOW GRAINS",
	80: "LIGHT SHOWERS", 81: "SHOWERS", 82: "HEAVY SHOWERS",
	85: "SNOW SHOWERS", 86: "HEAVY SNOW SHOWERS",
	95: "THUNDERSTORM", 96: "TSTORM HAIL", 99: "SEVERE TSTORM HAIL",
}

func init() {
	connector.Register(typeID, factory)
	providermeta.Register(providermeta.TypeMeta{
		TypeID: typeID, Label: "Open-Meteo Weather", Provider: "openmeteo",
		RequiresSecret: false, MinInterval: time.Minute, MinStaleAfter: time.Minute,
		SupportedRegions: []string{"global", "custom"}, DefaultRegion: "global",
		DefaultBaseURL: defaultBaseURL,
		Description:    "Current Open-Meteo weather; Beijing by default, with no API key required.",
	})
}

// Connector fetches and retains only the latest successfully decoded summary.
type Connector struct {
	spec    config.ProviderConfig
	runtime providerutil.Runtime
	mu      sync.RWMutex
	weather *protocol.WeatherSummary
}

type settings struct {
	baseURL   string
	latitude  float64
	longitude float64
	location  string
	timezone  string
	zone      *time.Location
}

// New builds a connector with injectable HTTP and clock dependencies for tests.
func New(spec config.ProviderConfig, runtime providerutil.Runtime) *Connector {
	return &Connector{spec: spec, runtime: providerutil.NormalizeRuntime(runtime)}
}

func factory(spec config.ProviderConfig, _ secretstore.Store) (connector.Connector, error) {
	return New(spec, providerutil.Runtime{}), nil
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
			return settings{}, connector.Errorf(protocol.ErrInvalidConfig, "Open-Meteo option %q is not allowed", key)
		}
	}
	if c.spec.SecretRef != "" || c.spec.AuthFile != "" {
		return settings{}, connector.Errorf(protocol.ErrInvalidConfig, "Open-Meteo does not use credentials")
	}
	base := defaultBaseURL
	switch c.spec.Region {
	case "", "global":
	case "custom":
		base = strings.TrimSpace(c.spec.BaseURL)
		parsed, err := url.Parse(base)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
			return settings{}, connector.Errorf(protocol.ErrInvalidConfig, "Open-Meteo custom base URL is invalid")
		}
	default:
		return settings{}, connector.Errorf(protocol.ErrInvalidConfig, "Open-Meteo region %q is not supported", c.spec.Region)
	}
	latitude, err := coordinate(c.spec.Options, "latitude", 39.9042, -90, 90)
	if err != nil {
		return settings{}, err
	}
	longitude, err := coordinate(c.spec.Options, "longitude", 116.4074, -180, 180)
	if err != nil {
		return settings{}, err
	}
	location := option(c.spec.Options, "location", "BEIJING")
	if location == "" || len(location) > 64 || strings.ContainsAny(location, "\r\n\x00") {
		return settings{}, connector.Errorf(protocol.ErrInvalidConfig, "Open-Meteo location is invalid")
	}
	timezone := option(c.spec.Options, "timezone", "Asia/Shanghai")
	zone, zoneErr := time.LoadLocation(timezone)
	if zoneErr != nil {
		return settings{}, connector.Errorf(protocol.ErrInvalidConfig, "Open-Meteo timezone is invalid")
	}
	return settings{
		baseURL: base, latitude: latitude, longitude: longitude,
		location: location, timezone: timezone, zone: zone,
	}, nil
}

func option(options map[string]string, key, fallback string) string {
	if value, ok := options[key]; ok {
		return strings.TrimSpace(value)
	}
	return fallback
}

func coordinate(options map[string]string, key string, fallback, min, max float64) (float64, error) {
	raw, ok := options[key]
	if !ok {
		return fallback, nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < min || value > max {
		return 0, connector.Errorf(protocol.ErrInvalidConfig, "Open-Meteo %s is outside its valid range", key)
	}
	return value, nil
}

type forecastResponse struct {
	Current struct {
		Time             *string  `json:"time"`
		Temperature      *float64 `json:"temperature_2m"`
		RelativeHumidity *float64 `json:"relative_humidity_2m"`
		WeatherCode      *int     `json:"weather_code"`
		WindSpeed        *float64 `json:"wind_speed_10m"`
	} `json:"current"`
	Daily struct {
		High []*float64 `json:"temperature_2m_max"`
		Low  []*float64 `json:"temperature_2m_min"`
	} `json:"daily"`
}

// Collect performs one request and publishes no partial summary on failure.
func (c *Connector) Collect(ctx context.Context) ([]protocol.ProviderMetric, error) {
	cfg, err := c.settings()
	if err != nil {
		return nil, err
	}
	endpoint, parseErr := url.Parse(cfg.baseURL)
	if parseErr != nil {
		return nil, connector.Errorf(protocol.ErrInvalidConfig, "Open-Meteo base URL is invalid")
	}
	query := endpoint.Query()
	query.Set("latitude", strconv.FormatFloat(cfg.latitude, 'f', -1, 64))
	query.Set("longitude", strconv.FormatFloat(cfg.longitude, 'f', -1, 64))
	query.Set("current", "temperature_2m,relative_humidity_2m,weather_code,wind_speed_10m")
	query.Set("daily", "temperature_2m_max,temperature_2m_min")
	query.Set("forecast_days", "1")
	query.Set("timezone", cfg.timezone)
	endpoint.RawQuery = query.Encode()

	var response forecastResponse
	if err := connector.GetJSON(ctx, connector.JSONRequest{
		Client: c.runtime.HTTPClient, URL: endpoint.String(),
	}, &response); err != nil {
		return nil, err
	}
	weather, err := buildWeather(cfg, response)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	stored := weather.Clone()
	c.weather = &stored
	c.mu.Unlock()
	return []protocol.ProviderMetric{}, nil
}

// Weather returns the latest complete successful response.
func (c *Connector) Weather() (protocol.WeatherSummary, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.weather == nil {
		return protocol.WeatherSummary{}, connector.Errorf(protocol.ErrSchemaChanged, "Open-Meteo weather is unavailable")
	}
	return c.weather.Clone(), nil
}

func buildWeather(cfg settings, response forecastResponse) (protocol.WeatherSummary, error) {
	current := response.Current
	if current.Time == nil || current.Temperature == nil || current.RelativeHumidity == nil ||
		current.WeatherCode == nil || current.WindSpeed == nil ||
		len(response.Daily.High) != 1 || len(response.Daily.Low) != 1 ||
		response.Daily.High[0] == nil || response.Daily.Low[0] == nil {
		return protocol.WeatherSummary{}, connector.Errorf(protocol.ErrSchemaChanged, "Open-Meteo response is missing current weather fields")
	}
	observedAt, err := parseObservedAt(*current.Time, cfg.zone)
	if err != nil {
		return protocol.WeatherSummary{}, connector.Errorf(protocol.ErrSchemaChanged, "Open-Meteo observation time is invalid")
	}
	temperature, err := roundedMeasurement("temperature", *current.Temperature, -100, 100)
	if err != nil {
		return protocol.WeatherSummary{}, err
	}
	high, err := roundedMeasurement("daily high", *response.Daily.High[0], -100, 100)
	if err != nil {
		return protocol.WeatherSummary{}, err
	}
	low, err := roundedMeasurement("daily low", *response.Daily.Low[0], -100, 100)
	if err != nil {
		return protocol.WeatherSummary{}, err
	}
	if high < low {
		return protocol.WeatherSummary{}, connector.Errorf(protocol.ErrSchemaChanged, "Open-Meteo daily high is below daily low")
	}
	humidity, err := roundedMeasurement("relative humidity", *current.RelativeHumidity, 0, 100)
	if err != nil {
		return protocol.WeatherSummary{}, err
	}
	wind, err := roundedMeasurement("wind speed", *current.WindSpeed, 0, 1000)
	if err != nil {
		return protocol.WeatherSummary{}, err
	}
	condition, ok := weatherCondition(*current.WeatherCode)
	if !ok {
		return protocol.WeatherSummary{}, connector.Errorf(protocol.ErrSchemaChanged, "Open-Meteo returned an unknown weather code")
	}
	weather := protocol.WeatherSummary{
		Location: cfg.location, TemperatureC: new(temperature),
		HighTemperatureC: new(high), LowTemperatureC: new(low),
		HumidityPercent: new(humidity), WindSpeedKPH: new(wind),
		Condition: condition, ObservedAt: observedAt, Status: protocol.StatusOK,
	}
	if err := weather.Validate(); err != nil {
		return protocol.WeatherSummary{}, connector.Errorf(protocol.ErrSchemaChanged, "Open-Meteo response failed weather validation")
	}
	return weather, nil
}

func roundedMeasurement(name string, value, min, max float64) (int, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < min || value > max {
		return 0, connector.Errorf(protocol.ErrSchemaChanged, "Open-Meteo %s is outside its valid range", name)
	}
	return int(math.Round(value)), nil
}

func parseObservedAt(raw string, zone *time.Location) (time.Time, error) {
	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		return parsed.UTC(), nil
	}
	parsed, err := time.ParseInLocation("2006-01-02T15:04", raw, zone)
	if err != nil {
		return time.Time{}, err
	}
	return parsed.UTC(), nil
}

func weatherCondition(code int) (string, bool) {
	condition, ok := weatherConditions[code]
	return condition, ok
}

var _ connector.Connector = (*Connector)(nil)
var _ connector.WeatherReporter = (*Connector)(nil)
