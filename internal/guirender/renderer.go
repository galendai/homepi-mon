package guirender

import "errors"

// HomeFrame contains display-ready strings for the 480x320 home screen.
type HomeFrame struct {
	NodeLabel       string
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

// Renderer draws either the graphical home screen or a legacy 60x20 text page.
type Renderer interface {
	DrawHome(HomeFrame) error
	DrawLegacy(string) error
	Close() error
}

var (
	errUnsupported = errors.New("guirender: LVGL framebuffer rendering requires linux with cgo")
	errClosed      = errors.New("guirender: renderer is closed")
)
