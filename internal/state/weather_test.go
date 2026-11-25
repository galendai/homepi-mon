package state_test

import (
	"testing"
	"time"

	"github.com/galendai/homepi-mon/internal/protocol"
	"github.com/galendai/homepi-mon/internal/state"
)

func weatherSummary(temperature int) protocol.WeatherSummary {
	return protocol.WeatherSummary{
		Location: "BEIJING", TemperatureC: new(temperature), HighTemperatureC: new(18), LowTemperatureC: new(2),
		HumidityPercent: new(50), WindSpeedKPH: new(8), Condition: "CLEAR",
		ObservedAt: time.Date(2026, 11, 24, 0, 30, 0, 0, time.UTC), Status: protocol.StatusOK,
	}
}

func TestWeatherStatePublishesHasDataAndDoesNotSharePointers(t *testing.T) {
	store, err := state.New(state.Options{NodeID: "node", Epoch: "epoch"})
	if err != nil {
		t.Fatal(err)
	}
	if store.HasData() {
		t.Fatal("empty store has data")
	}
	weather := weatherSummary(12)
	if err := store.ApplyConnectorWeather("weather", 1, weather); err != nil {
		t.Fatal(err)
	}
	*weather.TemperatureC = 99
	snapshot := store.Snapshot()
	if !store.HasData() || snapshot.Weather == nil || *snapshot.Weather.TemperatureC != 12 {
		t.Fatalf("weather snapshot = %+v, hasData=%v", snapshot.Weather, store.HasData())
	}
	*snapshot.Weather.TemperatureC = 77
	if *store.Snapshot().Weather.TemperatureC != 12 {
		t.Fatal("snapshot weather aliases current state")
	}
	if err := snapshot.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestWeatherFailurePreservesValueAndSuccessClearsError(t *testing.T) {
	store, _ := state.New(state.Options{NodeID: "node", Epoch: "epoch"})
	if err := store.ApplyConnectorWeather("weather", 1, weatherSummary(12)); err != nil {
		t.Fatal(err)
	}
	changed, err := store.ApplyConnectorError("weather", protocol.ErrNetwork, "provider request failed")
	if err != nil || changed != 1 {
		t.Fatalf("changed=%d err=%v", changed, err)
	}
	stale := store.Snapshot().Weather
	if *stale.TemperatureC != 12 || stale.ErrorClass != protocol.ErrNetwork || stale.Message != "provider request failed" {
		t.Fatalf("stale weather = %+v", stale)
	}
	if err := store.ApplyConnectorWeather("weather", 2, weatherSummary(14)); err != nil {
		t.Fatal(err)
	}
	fresh := store.Snapshot().Weather
	if *fresh.TemperatureC != 14 || fresh.ErrorClass != protocol.ErrNone || fresh.Message != "" {
		t.Fatalf("refreshed weather = %+v", fresh)
	}
}

func TestWeatherRejectsLateResultsAndUnrelatedErrors(t *testing.T) {
	store, _ := state.New(state.Options{NodeID: "node", Epoch: "epoch"})
	if err := store.ApplyConnectorWeather("weather", 2, weatherSummary(12)); err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyConnectorWeather("weather", 1, weatherSummary(1)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApplyConnectorError("other", protocol.ErrTimeout, "timeout"); err != nil {
		t.Fatal(err)
	}
	weather := store.Snapshot().Weather
	if *weather.TemperatureC != 12 || weather.ErrorClass != protocol.ErrNone {
		t.Fatalf("weather changed by late result or unrelated error: %+v", weather)
	}
}
