package protocol

import (
	"errors"
	"fmt"
	"time"
)

// WeatherSummary is the optional current weather reading for one configured
// location. Nil measurements represent capabilities or values that were not
// available; they must never be rendered as zero.
type WeatherSummary struct {
	Location         string       `json:"location"`
	TemperatureC     *int         `json:"temperature_c,omitempty"`
	HighTemperatureC *int         `json:"high_temperature_c,omitempty"`
	LowTemperatureC  *int         `json:"low_temperature_c,omitempty"`
	HumidityPercent  *int         `json:"humidity_percent,omitempty"`
	WindSpeedKPH     *int         `json:"wind_speed_kph,omitempty"`
	Condition        string       `json:"condition"`
	ObservedAt       time.Time    `json:"observed_at"`
	StaleAfter       Duration     `json:"stale_after,omitempty"`
	Status           MetricStatus `json:"status"`
	ErrorClass       ErrorClass   `json:"error_class,omitempty"`
	Message          string       `json:"message,omitempty"`
}

// Clone returns a copy that shares no optional measurement pointers.
func (w WeatherSummary) Clone() WeatherSummary {
	out := w
	out.TemperatureC = cloneInt(w.TemperatureC)
	out.HighTemperatureC = cloneInt(w.HighTemperatureC)
	out.LowTemperatureC = cloneInt(w.LowTemperatureC)
	out.HumidityPercent = cloneInt(w.HumidityPercent)
	out.WindSpeedKPH = cloneInt(w.WindSpeedKPH)
	return out
}

// Validate checks bounds and terminal-safe text for a weather summary.
func (w *WeatherSummary) Validate() error {
	if w == nil {
		return errors.New("weather: nil")
	}
	if err := safeText("location", w.Location, 64); err != nil {
		return err
	}
	if w.Location == "" {
		return errors.New("location: must not be empty")
	}
	for field, value := range map[string]*int{
		"temperature_c":      w.TemperatureC,
		"high_temperature_c": w.HighTemperatureC,
		"low_temperature_c":  w.LowTemperatureC,
	} {
		if value != nil && (*value < -100 || *value > 100) {
			return fmt.Errorf("%s: must be -100..100", field)
		}
	}
	if w.HighTemperatureC != nil && w.LowTemperatureC != nil &&
		*w.HighTemperatureC < *w.LowTemperatureC {
		return errors.New("high_temperature_c: must not be below low_temperature_c")
	}
	if w.HumidityPercent != nil && (*w.HumidityPercent < 0 || *w.HumidityPercent > 100) {
		return errors.New("humidity_percent: must be 0..100")
	}
	if w.WindSpeedKPH != nil && (*w.WindSpeedKPH < 0 || *w.WindSpeedKPH > 1000) {
		return errors.New("wind_speed_kph: must be 0..1000")
	}
	if err := safeText("condition", w.Condition, 32); err != nil {
		return err
	}
	if w.Condition == "" {
		return errors.New("condition: must not be empty")
	}
	for _, r := range w.Condition {
		if r > 127 {
			return errors.New("condition: must be ASCII")
		}
	}
	if w.ObservedAt.IsZero() {
		return errors.New("observed_at: must be set")
	}
	if w.StaleAfter.D() < 0 {
		return errors.New("stale_after: must not be negative")
	}
	if !w.Status.Valid() {
		return enumError("status", w.Status)
	}
	if w.ErrorClass != ErrNone && !w.ErrorClass.Valid() {
		return enumError("error_class", w.ErrorClass)
	}
	return safeText("message", w.Message, 160)
}
