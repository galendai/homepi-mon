//go:build linux && cgo

package guirender

/*
#cgo CFLAGS: -I${SRCDIR} -DLV_CONF_INCLUDE_SIMPLE -DLV_KCONFIG_IGNORE
#cgo LDFLAGS: -llvgl -lm
#include <stdlib.h>
#include "bridge.h"
*/
import "C"

import (
	"fmt"
	"sync"
	"unicode/utf8"
	"unsafe"
)

type lvglRenderer struct {
	mu     sync.Mutex
	native *C.gui_renderer
	closed bool
}

// NewRenderer opens a 480x320 RGB565 Linux framebuffer using LVGL's fbdev driver.
func NewRenderer(fbDevice string) (Renderer, error) {
	if fbDevice == "" {
		fbDevice = "/dev/fb0"
	}
	device := C.CString(fbDevice)
	defer C.free(unsafe.Pointer(device))

	errorBuffer := make([]byte, 256)
	native := C.gui_renderer_create(device, (*C.char)(unsafe.Pointer(&errorBuffer[0])), C.size_t(len(errorBuffer)))
	if native == nil {
		return nil, fmt.Errorf("guirender: %s", C.GoString((*C.char)(unsafe.Pointer(&errorBuffer[0]))))
	}
	return &lvglRenderer{native: native}, nil
}

var homeFieldNames = [...]string{
	"TitleText", "TimeText", "DateText", "TemperatureText",
	"ConditionText", "HighText", "LowText", "HeaderStatus", "FooterStatus", "PageText",
	"MonitorSource", "MonitorDetail", "WeatherDetail",
}

func (r *lvglRenderer) DrawHome(frame HomeFrame) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return errClosed
	}

	values := [...]string{
		frame.TitleText, frame.TimeText, frame.DateText, frame.TemperatureText,
		frame.ConditionText, frame.HighText, frame.LowText,
		frame.HeaderStatus, frame.FooterStatus, frame.PageText,
		frame.MonitorSource, frame.MonitorDetail, frame.WeatherDetail,
	}
	for i, value := range values {
		data := (*C.char)(unsafe.Pointer(unsafe.StringData(value)))
		result := C.gui_renderer_set_home_text(r.native, C.int(i), data, C.size_t(len(value)))
		switch result {
		case 1:
			return fmt.Errorf("guirender: %s exceeds %d-byte buffer", homeFieldNames[i], int(C.GUI_HOME_TEXT_CAPACITY)-1)
		case 2:
			return fmt.Errorf("guirender: %s contains a NUL byte", homeFieldNames[i])
		}
	}
	C.gui_renderer_draw_home(r.native, C.int(frame.MonitorTone))
	return nil
}

func (r *lvglRenderer) setTokenPlanText(field int, value string) error {
	data := (*C.char)(unsafe.Pointer(unsafe.StringData(value)))
	result := C.gui_renderer_set_token_plan_text(r.native, C.int(field), data, C.size_t(len(value)))
	switch result {
	case 0:
		return nil
	case 1:
		return fmt.Errorf("guirender: token plan field %d exceeds %d-byte buffer", field, int(C.GUI_TOKEN_PLAN_TEXT_CAPACITY)-1)
	case 2:
		return fmt.Errorf("guirender: token plan field %d contains a NUL byte", field)
	default:
		return fmt.Errorf("guirender: token plan field %d is invalid", field)
	}
}

func (r *lvglRenderer) setTokenPlanTone(field int, tone TokenPlanTone) error {
	if C.gui_renderer_set_token_plan_tone(r.native, C.int(field), C.int(tone)) != 0 {
		return fmt.Errorf("guirender: token plan tone %d/%d is invalid", field, tone)
	}
	return nil
}

// DrawTokenPlan renders subscription-only cards in one LVGL refresh.
func (r *lvglRenderer) DrawTokenPlan(frame TokenPlanFrame) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return errClosed
	}
	if frame.PlanCount < 0 || frame.PlanCount > len(frame.Plans) {
		return fmt.Errorf("guirender: invalid token plan count %d", frame.PlanCount)
	}
	header := [...]string{frame.TitleText, frame.HeaderStatus, frame.FooterStatus, frame.PageText, frame.PlanEmpty}
	for field, value := range header {
		if err := r.setTokenPlanText(field, value); err != nil {
			return err
		}
	}
	rowFields := int(C.GUI_TOKEN_PLAN_ROW_FIELDS)
	for index := range frame.PlanCount {
		row := &frame.Plans[index]
		field := int(C.GUI_TOKEN_PLAN_HEADER_FIELDS) + index*rowFields
		values := [...]string{row.Name, row.PrimaryLabel, row.PrimaryValue, row.FiveHour, row.Week, row.Month, row.Reset, row.Status}
		for offset, value := range values {
			if err := r.setTokenPlanText(field+offset, value); err != nil {
				return err
			}
		}
		tones := [...]TokenPlanTone{row.PrimaryTone, row.FiveHourTone, row.WeekTone, row.MonthTone, row.StatusTone}
		for offset, tone := range tones {
			if err := r.setTokenPlanTone(index*5+offset, tone); err != nil {
				return err
			}
		}
	}
	if C.gui_renderer_draw_token_plan(r.native, C.int(frame.PlanCount)) != 0 {
		return fmt.Errorf("guirender: cannot draw token plan")
	}
	return nil
}

// DrawAPIUsage renders API-only cards in one LVGL refresh.
func (r *lvglRenderer) DrawAPIUsage(frame APIUsageFrame) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return errClosed
	}
	if frame.RowCount < 0 || frame.RowCount > len(frame.Rows) {
		return fmt.Errorf("guirender: invalid API Usage count %d", frame.RowCount)
	}
	setText := func(field int, value string) error {
		data := (*C.char)(unsafe.Pointer(unsafe.StringData(value)))
		result := C.gui_renderer_set_api_usage_text(r.native, C.int(field), data, C.size_t(len(value)))
		switch result {
		case 0:
			return nil
		case 1:
			return fmt.Errorf("guirender: API Usage field %d exceeds %d-byte buffer", field, int(C.GUI_API_USAGE_TEXT_CAPACITY)-1)
		case 2:
			return fmt.Errorf("guirender: API Usage field %d contains a NUL byte", field)
		default:
			return fmt.Errorf("guirender: API Usage field %d is invalid", field)
		}
	}
	header := [...]string{frame.TitleText, frame.HeaderStatus, frame.FooterStatus, frame.PageText, frame.Empty}
	for field, value := range header {
		if err := setText(field, value); err != nil {
			return err
		}
	}
	for index := range frame.RowCount {
		row := &frame.Rows[index]
		field := int(C.GUI_API_USAGE_HEADER_FIELDS) + index*int(C.GUI_API_USAGE_ROW_FIELDS)
		values := [...]string{row.Name, row.Amount, row.Status}
		for offset, value := range values {
			if err := setText(field+offset, value); err != nil {
				return err
			}
		}
		if C.gui_renderer_set_api_usage_tone(r.native, C.int(index), C.int(row.StatusTone)) != 0 {
			return fmt.Errorf("guirender: API Usage tone %d/%d is invalid", index, row.StatusTone)
		}
	}
	if C.gui_renderer_draw_api_usage(r.native, C.int(frame.RowCount)) != 0 {
		return fmt.Errorf("guirender: cannot draw API Usage")
	}
	return nil
}

func (r *lvglRenderer) setOpenClashText(field int, value string) error {
	if !utf8.ValidString(value) {
		return fmt.Errorf("guirender: OpenClash field %d contains invalid UTF-8", field)
	}
	data := (*C.char)(unsafe.Pointer(unsafe.StringData(value)))
	result := C.gui_renderer_set_openclash_text(r.native, C.int(field), data, C.size_t(len(value)))
	switch result {
	case 0:
		return nil
	case 1:
		return fmt.Errorf("guirender: OpenClash field %d exceeds %d-byte buffer", field, int(C.GUI_OPENCLASH_TEXT_CAPACITY)-1)
	case 2:
		return fmt.Errorf("guirender: OpenClash field %d contains a NUL byte", field)
	case 3:
		return fmt.Errorf("guirender: OpenClash field %d contains invalid UTF-8", field)
	case 4:
		return fmt.Errorf("guirender: OpenClash field %d exceeds its native pixel width", field)
	default:
		return fmt.Errorf("guirender: OpenClash field %d is invalid", field)
	}
}

// DrawOpenClash renders a 2x5 grid of proxy node cards in one LVGL refresh.
func (r *lvglRenderer) DrawOpenClash(frame OpenClashFrame) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return errClosed
	}
	if frame.RowCount < 0 || frame.RowCount > len(frame.Rows) {
		return fmt.Errorf("guirender: invalid OpenClash count %d", frame.RowCount)
	}
	header := [...]string{
		frame.TitleText, frame.HeaderStatus, frame.FooterStatus, frame.PageText,
		frame.SubpageText, frame.Empty, frame.OverflowText,
	}
	for field, value := range header {
		if err := r.setOpenClashText(field, value); err != nil {
			return err
		}
	}
	for index := range frame.RowCount {
		row := &frame.Rows[index]
		field := int(C.GUI_OPENCLASH_HEADER_FIELDS) + index*int(C.GUI_OPENCLASH_ROW_FIELDS)
		values := [...]string{row.Name, row.Delay}
		for offset, value := range values {
			if err := r.setOpenClashText(field+offset, value); err != nil {
				return err
			}
		}
		selected := 0
		if row.Selected {
			selected = 1
		}
		if C.gui_renderer_set_openclash_row(r.native, C.int(index), C.int(selected), C.int(row.Tone)) != 0 {
			return fmt.Errorf("guirender: OpenClash row %d tone %d is invalid", index, row.Tone)
		}
	}
	if C.gui_renderer_draw_openclash(r.native, C.int(frame.RowCount)) != 0 {
		return fmt.Errorf("guirender: cannot draw OpenClash")
	}
	return nil
}

func (r *lvglRenderer) DrawLegacy(text string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return errClosed
	}
	data := (*C.char)(unsafe.Pointer(unsafe.StringData(text)))
	result := C.gui_renderer_draw_legacy(r.native, data, C.size_t(len(text)))
	switch result {
	case 1:
		return fmt.Errorf("guirender: legacy text exceeds %d-byte buffer", int(C.GUI_LEGACY_TEXT_CAPACITY)-1)
	case 2:
		return fmt.Errorf("guirender: legacy text contains a NUL byte")
	}
	return nil
}

func (r *lvglRenderer) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	C.gui_renderer_destroy(r.native)
	r.native = nil
	r.closed = true
	return nil
}
