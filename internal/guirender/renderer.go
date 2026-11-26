package guirender

import "errors"

// HomeFrame contains display-ready strings for the 480x320 home screen.
type HomeFrame struct {
	TitleText       string
	TimeText        string
	DateText        string
	TemperatureText string
	ConditionText   string
	HighText        string
	LowText         string
	HeaderStatus    string
	FooterStatus    string
	PageText        string
	MonitorSource   string
	MonitorDetail   string
	MonitorTone     TokenPlanTone
	WeatherDetail   string
}

// TokenPlanTone selects a semantic color without moving palette decisions into ui.
type TokenPlanTone uint8

const (
	TokenPlanToneDefault TokenPlanTone = iota
	TokenPlanToneOK
	TokenPlanToneWarning
	TokenPlanToneCritical
	TokenPlanToneMuted
	TokenPlanToneInfo
)

const (
	TokenPlanRowCapacity = 6
	APIUsageRowCapacity  = 8
	OpenClashRowCapacity = 10
)

// TokenPlanRowFrame is one provider's remaining quota windows.
type TokenPlanRowFrame struct {
	Name, PrimaryLabel, PrimaryValue, FiveHour, Week, Month, Reset, Status string
	PrimaryTone, FiveHourTone, WeekTone, MonthTone, StatusTone             TokenPlanTone
}

// TokenPlanFrame is comparable so the display can suppress unchanged redraws.
type TokenPlanFrame struct {
	TitleText, HeaderStatus, FooterStatus, PageText string
	PlanEmpty                                       string
	Plans                                           [TokenPlanRowCapacity]TokenPlanRowFrame
	PlanCount                                       int
}

// APIUsageRowFrame is one API provider's main balance or cost value.
type APIUsageRowFrame struct {
	Name, StatusText, Currency, Value, Estimate string
	StatusTone                                  TokenPlanTone
	LowBalance                                  bool
}

// APIUsageFrame contains only API values for the third logical page.
type APIUsageFrame struct {
	TitleText, HeaderStatus, FooterStatus, PageText string
	Empty                                           string
	Rows                                            [APIUsageRowCapacity]APIUsageRowFrame
	RowCount                                        int
}

// OpenClashRowFrame is one proxy node's single-line name and ping card.
type OpenClashRowFrame struct {
	Name, Delay string
	Selected    bool
	Tone        TokenPlanTone
}

// OpenClashFrame is comparable so the display can suppress unchanged redraws.
type OpenClashFrame struct {
	TitleText, HeaderStatus, FooterStatus, PageText string
	SubpageText, Empty, OverflowText                string
	Rows                                            [OpenClashRowCapacity]OpenClashRowFrame
	RowCount                                        int
}

// Renderer draws graphical HOME, Token Plan, API Balance, and OpenClash pages or a legacy page.
type Renderer interface {
	DrawHome(HomeFrame) error
	DrawTokenPlan(TokenPlanFrame) error
	DrawAPIUsage(APIUsageFrame) error
	DrawOpenClash(OpenClashFrame) error
	DrawLegacy(string) error
	Close() error
}

var (
	errUnsupported = errors.New("guirender: LVGL framebuffer rendering requires linux with cgo")
	errClosed      = errors.New("guirender: renderer is closed")
)
