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
	"NodeLabel", "TimeText", "DateText", "TemperatureText",
	"ConditionText", "HighText", "LowText", "HeaderStatus", "FooterStatus", "PageText",
}

func (r *lvglRenderer) DrawHome(frame HomeFrame) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return errClosed
	}

	values := [...]string{
		frame.NodeLabel, frame.TimeText, frame.DateText, frame.TemperatureText,
		frame.ConditionText, frame.HighText, frame.LowText,
		frame.HeaderStatus, frame.FooterStatus, frame.PageText,
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
	C.gui_renderer_draw_home(r.native)
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
