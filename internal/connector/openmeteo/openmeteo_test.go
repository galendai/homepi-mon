package openmeteo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/galendai/homepi-mon/internal/config"
	"github.com/galendai/homepi-mon/internal/connector"
	"github.com/galendai/homepi-mon/internal/connector/providerutil"
	"github.com/galendai/homepi-mon/internal/protocol"
)

const validForecast = `{
  "current":{"time":"2026-11-24T08:30","temperature_2m":12.6,"relative_humidity_2m":48.4,"weather_code":2,"wind_speed_10m":9.5},
  "daily":{"temperature_2m_max":[16.4],"temperature_2m_min":[3.6]}
}`

func TestCollectRequestsBeijingDefaultsAndBuildsSummary(t *testing.T) {
	var requestErr string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		want := map[string]string{
			"latitude": "39.9042", "longitude": "116.4074", "timezone": "Asia/Shanghai",
			"current": "temperature_2m,relative_humidity_2m,weather_code,wind_speed_10m",
			"daily":   "temperature_2m_max,temperature_2m_min", "forecast_days": "1",
		}
		for key, value := range want {
			if query.Get(key) != value {
				requestErr = key + "=" + query.Get(key)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(validForecast))
	}))
	defer server.Close()

	c := New(config.ProviderConfig{ID: "weather", Region: "custom", BaseURL: server.URL}, providerutil.Runtime{
		HTTPClient: server.Client(),
	})
	metrics, err := c.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if requestErr != "" {
		t.Fatalf("unexpected request query: %s", requestErr)
	}
	if len(metrics) != 0 {
		t.Fatalf("weather emitted ProviderMetrics: %+v", metrics)
	}
	weather, err := c.Weather()
	if err != nil {
		t.Fatal(err)
	}
	wantObserved := time.Date(2026, 11, 24, 0, 30, 0, 0, time.UTC)
	if weather.Location != "BEIJING" || *weather.TemperatureC != 13 ||
		*weather.HighTemperatureC != 16 || *weather.LowTemperatureC != 4 ||
		*weather.HumidityPercent != 48 || *weather.WindSpeedKPH != 10 ||
		weather.Condition != "PARTLY CLOUDY" || !weather.ObservedAt.Equal(wantObserved) ||
		weather.Status != protocol.StatusOK || weather.ErrorClass != protocol.ErrNone {
		t.Fatalf("weather = %+v", weather)
	}
}

func TestSettingsAllowLocationOverridesAndRejectInvalidValues(t *testing.T) {
	c := New(config.ProviderConfig{Options: map[string]string{
		"latitude": "31.2304", "longitude": "121.4737", "location": "SHANGHAI", "timezone": "Asia/Shanghai",
	}}, providerutil.Runtime{})
	cfg, err := c.settings()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.latitude != 31.2304 || cfg.longitude != 121.4737 || cfg.location != "SHANGHAI" ||
		cfg.baseURL != "https://api.open-meteo.com/v1/forecast" {
		t.Fatalf("settings = %+v", cfg)
	}

	for _, options := range []map[string]string{
		{"latitude": "91"}, {"longitude": "-181"}, {"latitude": "NaN"},
		{"timezone": "not/a-zone"}, {"location": ""}, {"api_key": "secret"},
	} {
		c := New(config.ProviderConfig{Options: options}, providerutil.Runtime{})
		if err := c.ValidateConfig(); err == nil {
			t.Fatalf("invalid options accepted: %#v", options)
		}
	}
}

func TestResponseSchemaAndMeasurementBoundaries(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(string) string
		wantErr string
	}{
		{"missing current", func(raw string) string { return strings.Replace(raw, `"temperature_2m":12.6,`, "", 1) }, "missing"},
		{"missing daily", func(raw string) string { return strings.Replace(raw, `[16.4]`, `[]`, 1) }, "missing"},
		{"null daily", func(raw string) string { return strings.Replace(raw, `[3.6]`, `[null]`, 1) }, "missing"},
		{"bad time", func(raw string) string { return strings.Replace(raw, "2026-11-24T08:30", "yesterday", 1) }, "time"},
		{"humidity", func(raw string) string { return strings.Replace(raw, "48.4", "100.1", 1) }, "humidity"},
		{"negative wind", func(raw string) string { return strings.Replace(raw, "9.5", "-1", 1) }, "wind"},
		{"reversed daily", func(raw string) string { return strings.Replace(raw, "16.4", "2", 1) }, "high"},
		{"unknown WMO", func(raw string) string { return strings.Replace(raw, `"weather_code":2`, `"weather_code":1000`, 1) }, "weather code"},
		{"non-finite", func(raw string) string { return strings.Replace(raw, "12.6", "1e999", 1) }, "schema"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var response forecastResponse
			raw := tc.mutate(validForecast)
			if err := json.Unmarshal([]byte(raw), &response); err == nil {
				_, err = buildWeather(settings{location: "BEIJING", zone: time.FixedZone("CST", 8*3600)}, response)
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want %q", err, tc.wantErr)
				}
			} else if tc.wantErr != "schema" {
				t.Fatalf("test response did not decode: %v", err)
			}
		})
	}
}

func TestFailedRefreshRetainsOnlyLastSuccessfulConnectorValue(t *testing.T) {
	fail := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if fail {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"upstream secret detail"}`))
			return
		}
		_, _ = w.Write([]byte(validForecast))
	}))
	defer server.Close()
	c := New(config.ProviderConfig{Region: "custom", BaseURL: server.URL}, providerutil.Runtime{HTTPClient: server.Client()})
	if _, err := c.Collect(context.Background()); err != nil {
		t.Fatal(err)
	}
	before, _ := c.Weather()
	fail = true
	_, err := c.Collect(context.Background())
	if connector.Classify(err) != protocol.ErrUpstream {
		t.Fatalf("failure class = %q, err=%v", connector.Classify(err), err)
	}
	after, weatherErr := c.Weather()
	if weatherErr != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("failed refresh changed cached weather: before=%+v after=%+v err=%v", before, after, weatherErr)
	}
}

func TestWMOConditionsAreShortASCII(t *testing.T) {
	codes := []int{0, 1, 2, 3, 45, 48, 51, 53, 55, 56, 57, 61, 63, 65, 66, 67, 71, 73, 75, 77, 80, 81, 82, 85, 86, 95, 96, 99}
	for _, code := range codes {
		condition, ok := weatherCondition(code)
		if !ok || condition == "" || len(condition) > 24 {
			t.Fatalf("code %d condition = %q, ok=%v", code, condition, ok)
		}
		for _, r := range condition {
			if r < 32 || r > 126 {
				t.Fatalf("code %d condition is not ASCII: %q", code, condition)
			}
		}
	}
	if _, ok := weatherCondition(-1); ok {
		t.Fatal("unknown WMO code accepted")
	}
}
