package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/galendai/homepi-mon/internal/protocol"
)

// HomeModel contains presentation-ready, secret-free values for the pixel
// landing page. Numeric weather readings remain typed in protocol.WeatherSummary.
type HomeModel struct {
	TitleText          string
	TimeText           string
	DateText           string
	TemperatureText    string
	ConditionText      string
	HighText           string
	LowText            string
	HumidityText       string
	WindText           string
	WeatherDetail      string
	Monitor            HomeMonitor
	WeatherUpdatedText string
	HeaderStatus       string
	FooterStatus       string
	PageText           string
}

// BuildHomeModel derives the landing-page presentation from the current snapshot.
func BuildHomeModel(vm ViewModel) HomeModel {
	model := HomeModel{
		TitleText:       "HOME",
		TimeText:        vm.Now.Format("15:04"),
		DateText:        strings.ToUpper(vm.Now.Format("Mon, Jan 02")),
		TemperatureText: "--",
		ConditionText:   "UNAVAILABLE",
		HighText:        "H --",
		LowText:         "L --",
		HumidityText:    "HUMIDITY --",
		WindText:        "WIND --",
		Monitor:         homeMonitor(vm),
		HeaderStatus:    string(vm.Link),
		FooterStatus:    "SNAPSHOT --",
		PageText:        fmt.Sprintf("%d/%d", vm.PageNumber, vm.PageCount),
	}
	if vm.SyncedAgo != nil {
		model.FooterStatus = "SNAPSHOT " + shortDuration(*vm.SyncedAgo) + " AGO"
	}
	weather := vm.Weather
	if weather == nil {
		model.WeatherUpdatedText = "NO WEATHER DATA"
		return model
	}
	if weather.TemperatureC != nil {
		model.TemperatureText = fmt.Sprintf("%d°C", *weather.TemperatureC)
	}
	if weather.HighTemperatureC != nil {
		model.HighText = fmt.Sprintf("H %d°C", *weather.HighTemperatureC)
	}
	if weather.LowTemperatureC != nil {
		model.LowText = fmt.Sprintf("L %d°C", *weather.LowTemperatureC)
	}
	if weather.HumidityPercent != nil {
		model.HumidityText = fmt.Sprintf("HUMIDITY %d%%", *weather.HumidityPercent)
	}
	if weather.WindSpeedKPH != nil {
		model.WindText = fmt.Sprintf("WIND %d KM/H", *weather.WindSpeedKPH)
	}
	if weather.Condition != "" {
		model.ConditionText = strings.ToUpper(weather.Condition)
	}
	age := vm.Now.Sub(weather.ObservedAt)
	if age < 0 {
		age = 0
	}
	displayAge := age.Truncate(time.Minute)
	model.WeatherUpdatedText = "UPDATED " + shortDuration(displayAge) + " AGO"
	stale := weather.Status == protocol.StatusStale || weather.Status == protocol.StatusError
	if weather.StaleAfter.D() > 0 && age > weather.StaleAfter.D() {
		stale = true
	}
	if stale {
		model.ConditionText = "STALE - " + model.ConditionText
		model.WeatherUpdatedText = "WEATHER " + shortDuration(displayAge) + " OLD"
	} else if weather.ErrorClass == protocol.ErrNone && weather.Status != protocol.StatusUnknown {
		var detail []string
		if weather.HumidityPercent != nil {
			detail = append(detail, fmt.Sprintf("RH %d%%", *weather.HumidityPercent))
		}
		if weather.WindSpeedKPH != nil {
			detail = append(detail, fmt.Sprintf("WIND %d km/h", *weather.WindSpeedKPH))
		}
		model.WeatherDetail = strings.Join(detail, "    ")
	}
	return model
}

func (vm ViewModel) homeBody() []string {
	home := BuildHomeModel(vm)
	location := "  WEATHER"
	if vm.Weather != nil && vm.Weather.Location != "" {
		location = "  " + strings.ToUpper(vm.Weather.Location)
	}
	left := []string{
		"       .          .",
		"           ##",
		"          ####",
		"         ######",
		"        ########",
		"       ###  ##  ###",
		"       ##   ##   ##",
		"       ###########",
		"  PIXEL HOME",
		location,
		"",
	}
	right := []string{
		"",
		home.TimeText,
		home.DateText,
		"",
		home.TemperatureText + "  " + home.ConditionText,
		home.HighText + "   " + home.LowText,
		home.HumidityText,
		home.WindText,
		home.WeatherUpdatedText,
		home.HeaderStatus,
		home.PageText,
	}
	body := make([]string, len(left))
	for i := range left {
		body[i] = " " + padTo(left[i], 25) + right[i]
	}
	return body
}
