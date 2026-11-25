package main

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"

	"github.com/galendai/homepi-mon/internal/guirender"
	"github.com/galendai/homepi-mon/internal/ui"
)

// pixelScreen renders the landing page with LVGL and preserves every existing
// page as a fixed-grid ASCII frame using the same semantic ViewModel.
type pixelScreen struct {
	mu         sync.Mutex
	out        io.Writer
	renderer   guirender.Renderer
	mode       string
	lastHome   guirender.HomeFrame
	lastLegacy string
}

var rendererFactory = guirender.NewRenderer

func newPixelScreen(out io.Writer, log *slog.Logger) (*pixelScreen, error) {
	var failures []string
	for _, device := range []string{"/dev/fb0", "/dev/fb1"} {
		renderer, err := rendererFactory(device)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", device, err))
			continue
		}
		log.Info("framebuffer selected", "device", device)
		return &pixelScreen{out: out, renderer: renderer}, nil
	}
	return nil, fmt.Errorf("no usable framebuffer: %s", strings.Join(failures, "; "))
}

func (s *pixelScreen) enter() {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _ = io.WriteString(s.out, seqResetStyle+seqHideCursor+seqClearScreen)
}

func (s *pixelScreen) restore() {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.renderer.Close()
	_, _ = io.WriteString(s.out, seqResetStyle+seqShowCursor)
}

func (s *pixelScreen) draw(vm ui.ViewModel) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ui.Page(strings.ToUpper(vm.Page)) == ui.PageHome && vm.RemoteNotice == nil {
		home := ui.BuildHomeModel(vm)
		frame := guirender.HomeFrame{
			NodeLabel: home.NodeLabel, TimeText: home.TimeText, DateText: home.DateText,
			TemperatureText: home.TemperatureText, ConditionText: home.ConditionText,
			HighText: home.HighText, LowText: home.LowText,
			HeaderStatus: home.HeaderStatus, FooterStatus: home.FooterStatus,
			PageText: home.PageText,
		}
		if s.mode == "home" && frame == s.lastHome {
			return nil
		}
		if err := s.renderer.DrawHome(frame); err != nil {
			return err
		}
		s.mode, s.lastHome = "home", frame
		return nil
	}
	frame := ui.RenderKioskString(vm, ui.StyleASCII)
	if s.mode == "legacy" && frame == s.lastLegacy {
		return nil
	}
	if err := s.renderer.DrawLegacy(frame); err != nil {
		return err
	}
	s.mode, s.lastLegacy = "legacy", frame
	return nil
}
