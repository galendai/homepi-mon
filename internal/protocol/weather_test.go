package protocol_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/galendai/homepi-mon/internal/protocol"
)

func validWeatherSnapshot() *protocol.MetricSnapshot {
	now := time.Date(2026, 11, 24, 0, 30, 0, 0, time.UTC)
	return &protocol.MetricSnapshot{
		SchemaVersion: protocol.SchemaVersion, SourceEpoch: "epoch", SourceNode: "node", GeneratedAt: now,
		Weather: &protocol.WeatherSummary{
			Location: "BEIJING", TemperatureC: new(13), HighTemperatureC: new(16), LowTemperatureC: new(4),
			HumidityPercent: new(48), WindSpeedKPH: new(10), Condition: "PARTLY CLOUDY",
			ObservedAt: now, StaleAfter: protocol.Duration(5 * time.Minute), Status: protocol.StatusOK,
		},
	}
}

func TestWeatherSnapshotValidatesAndClonesMeasurements(t *testing.T) {
	snapshot := validWeatherSnapshot()
	if snapshot.SchemaVersion != "1.2" {
		t.Fatalf("schema = %q, want 1.2", snapshot.SchemaVersion)
	}
	if err := snapshot.Validate(); err != nil {
		t.Fatal(err)
	}
	clone := snapshot.Clone()
	*clone.Weather.TemperatureC = 99
	*clone.Weather.HumidityPercent = 0
	if *snapshot.Weather.TemperatureC != 13 || *snapshot.Weather.HumidityPercent != 48 {
		t.Fatal("snapshot clone shares weather measurement pointers")
	}
}

func TestWeatherSnapshotRejectsInvalidData(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*protocol.WeatherSummary)
		want   string
	}{
		{"location", func(w *protocol.WeatherSummary) { w.Location = "bad\x1b" }, "control character"},
		{"condition ASCII", func(w *protocol.WeatherSummary) { w.Condition = "晴" }, "ASCII"},
		{"humidity", func(w *protocol.WeatherSummary) { w.HumidityPercent = new(101) }, "0..100"},
		{"wind", func(w *protocol.WeatherSummary) { w.WindSpeedKPH = new(-1) }, "0..1000"},
		{"daily order", func(w *protocol.WeatherSummary) { w.HighTemperatureC = new(1) }, "must not be below"},
		{"observation", func(w *protocol.WeatherSummary) { w.ObservedAt = time.Time{} }, "must be set"},
		{"freshness", func(w *protocol.WeatherSummary) { w.StaleAfter = protocol.Duration(-time.Second) }, "negative"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := validWeatherSnapshot()
			tc.mutate(snapshot.Weather)
			if err := snapshot.Validate(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestOldSnapshotWithoutWeatherRemainsCompatible(t *testing.T) {
	raw := `{"schema_version":"1.1","source_epoch":"epoch","snapshot_version":1,"generated_at":"2026-11-24T00:00:00Z","source_node":"node","metrics":[],"connector_health":[]}`
	var snapshot protocol.MetricSnapshot
	if err := json.Unmarshal([]byte(raw), &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Weather != nil {
		t.Fatal("old snapshot invented weather data")
	}
	if err := snapshot.Validate(); err != nil {
		t.Fatal(err)
	}
}
