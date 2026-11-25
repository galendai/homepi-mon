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
	Name, Amount, Status string
	StatusTone           TokenPlanTone
}

// APIUsageFrame contains only API values for the third logical page.
type APIUsageFrame struct {
	TitleText, HeaderStatus, FooterStatus, PageText string
	Empty                                           string
	Rows                                            [APIUsageRowCapacity]APIUsageRowFrame
	RowCount                                        int
}

// Renderer draws graphical HOME, Token Plan, and API Usage pages or a legacy page.
type Renderer interface {
	DrawHome(HomeFrame) error
	DrawTokenPlan(TokenPlanFrame) error
	DrawAPIUsage(APIUsageFrame) error
	DrawLegacy(string) error
	Close() error
}

var (
	errUnsupported = errors.New("guirender: LVGL framebuffer rendering requires linux with cgo")
	errClosed      = errors.New("guirender: renderer is closed")
)
