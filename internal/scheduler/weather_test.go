package scheduler_test

import (
	"context"
	"testing"
	"time"

	"github.com/galendai/homepi-mon/internal/connector"
	"github.com/galendai/homepi-mon/internal/protocol"
	"github.com/galendai/homepi-mon/internal/scheduler"
)

type weatherStub struct {
	*stubConnector
	weather protocol.WeatherSummary
}

func (w *weatherStub) Weather() (protocol.WeatherSummary, error) {
	return w.weather.Clone(), nil
}

func schedulerWeather(temperature int) protocol.WeatherSummary {
	return protocol.WeatherSummary{
		Location: "BEIJING", TemperatureC: new(temperature), HighTemperatureC: new(16), LowTemperatureC: new(4),
		HumidityPercent: new(48), WindSpeedKPH: new(10), Condition: "CLEAR",
		ObservedAt: time.Date(2026, 11, 24, 0, 30, 0, 0, time.UTC), Status: protocol.StatusOK,
	}
}

func TestManualRefreshCollectsWeatherAndMarksFailureStale(t *testing.T) {
	store := newStore(t)
	base := &stubConnector{id: "beijing-weather"}
	weatherConnector := &weatherStub{stubConnector: base, weather: schedulerWeather(13)}
	base.set(nil, nil)
	s := scheduler.New([]scheduler.Task{{
		Connector: weatherConnector, Enabled: true, StaleAfter: 30 * time.Minute,
	}}, scheduler.Options{Store: store, Logger: quietLogger()})

	if err := s.Refresh(context.Background(), []string{"beijing-weather"}); err != nil {
		t.Fatal(err)
	}
	weather := store.Snapshot().Weather
	if weather == nil || *weather.TemperatureC != 13 || weather.StaleAfter.D() != 30*time.Minute {
		t.Fatalf("weather = %+v", weather)
	}
	if base.calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", base.calls.Load())
	}

	base.set(nil, connector.Errorf(protocol.ErrTimeout, "provider request timed out"))
	if err := s.Refresh(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	weather = store.Snapshot().Weather
	if weather == nil || *weather.TemperatureC != 13 || weather.ErrorClass != protocol.ErrTimeout ||
		weather.Message != "provider request timed out" {
		t.Fatalf("stale weather = %+v", weather)
	}

	weatherConnector.weather = schedulerWeather(15)
	base.set(nil, nil)
	if err := s.Refresh(context.Background(), []string{"beijing-weather"}); err != nil {
		t.Fatal(err)
	}
	weather = store.Snapshot().Weather
	if weather == nil || *weather.TemperatureC != 15 || weather.ErrorClass != protocol.ErrNone || weather.Message != "" {
		t.Fatalf("recovered weather = %+v", weather)
	}
}
