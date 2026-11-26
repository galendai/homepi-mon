package main

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"

	"github.com/galendai/homepi-mon/internal/guirender"
	"github.com/galendai/homepi-mon/internal/protocol"
	"github.com/galendai/homepi-mon/internal/ui"
)

// pixelScreen renders HOME, CODING, API, and OPENCLASH with LVGL, preserving
// other pages and fallback states as fixed-grid ASCII frames using the same ViewModel.
type pixelScreen struct {
	mu            sync.Mutex
	out           io.Writer
	renderer      guirender.Renderer
	mode          string
	lastHome      guirender.HomeFrame
	lastTokenPlan guirender.TokenPlanFrame
	lastAPIUsage  guirender.APIUsageFrame
	lastOpenClash guirender.OpenClashFrame
	lastLegacy    string
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
			TitleText: home.TitleText, TimeText: home.TimeText, DateText: home.DateText,
			TemperatureText: home.TemperatureText, ConditionText: home.ConditionText,
			HighText: home.HighText, LowText: home.LowText,
			HeaderStatus: home.HeaderStatus, FooterStatus: home.FooterStatus,
			PageText:      home.PageText,
			MonitorSource: home.Monitor.Source, MonitorDetail: home.Monitor.Detail,
			MonitorTone: pixelTokenPlanTone(home.Monitor.Status), WeatherDetail: home.WeatherDetail,
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
	if ui.Page(strings.ToUpper(vm.Page)) == ui.PageOpenClash && vm.RemoteNotice == nil {
		if frame, ok := buildPixelOpenClashFrame(vm); ok {
			if s.mode == "openclash" && frame == s.lastOpenClash {
				return nil
			}
			if err := s.renderer.DrawOpenClash(frame); err == nil {
				s.mode, s.lastOpenClash = "openclash", frame
				return nil
			}
		}
	}
	if ui.Page(strings.ToUpper(vm.Page)) == ui.PageCoding && vm.RemoteNotice == nil {
		if frame, ok := buildPixelTokenPlanFrame(vm); ok {
			if s.mode == "token-plan" && frame == s.lastTokenPlan {
				return nil
			}
			if err := s.renderer.DrawTokenPlan(frame); err != nil {
				return err
			}
			s.mode, s.lastTokenPlan = "token-plan", frame
			return nil
		}
	}
	if ui.Page(strings.ToUpper(vm.Page)) == ui.PageAPI && vm.RemoteNotice == nil {
		if frame, ok := buildPixelAPIUsageFrame(vm); ok {
			if s.mode == "api-usage" && frame == s.lastAPIUsage {
				return nil
			}
			if err := s.renderer.DrawAPIUsage(frame); err != nil {
				return err
			}
			s.mode, s.lastAPIUsage = "api-usage", frame
			return nil
		}
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

func buildPixelOpenClashFrame(vm ui.ViewModel) (guirender.OpenClashFrame, bool) {
	model, err := ui.BuildOpenClashModel(vm)
	if err != nil || len(model.Rows) > guirender.OpenClashRowCapacity {
		return guirender.OpenClashFrame{}, false
	}
	frame := guirender.OpenClashFrame{
		TitleText: model.TitleText, HeaderStatus: model.HeaderStatus,
		FooterStatus: model.FooterStatus, PageText: model.PageText,
		SubpageText: model.SubpageText, Empty: model.Empty,
		OverflowText: model.OverflowText, RowCount: len(model.Rows),
	}
	for index := range model.Rows {
		row := &model.Rows[index]
		frame.Rows[index] = guirender.OpenClashRowFrame{
			Name: row.Name, Delay: row.Delay, Selected: row.Selected, Tone: pixelTokenPlanTone(row.Tone),
		}
	}
	return frame, true
}

func buildPixelTokenPlanFrame(vm ui.ViewModel) (guirender.TokenPlanFrame, bool) {
	model, err := ui.BuildTokenPlanModel(vm)
	if err != nil || len(model.Plans) > guirender.TokenPlanRowCapacity {
		return guirender.TokenPlanFrame{}, false
	}
	frame := guirender.TokenPlanFrame{
		TitleText: model.TitleText, HeaderStatus: model.HeaderStatus,
		FooterStatus: model.FooterStatus, PageText: model.PageText,
		PlanEmpty: model.PlanEmpty, PlanCount: len(model.Plans),
	}
	for index := range model.Plans {
		row := &model.Plans[index]
		frame.Plans[index] = guirender.TokenPlanRowFrame{
			Name: row.Name, PrimaryLabel: row.PrimaryLabel, PrimaryValue: row.PrimaryValue,
			FiveHour: row.FiveHour, Week: row.Week, Month: row.Month,
			Reset: row.Reset, Status: string(row.Status), PrimaryTone: pixelTokenPlanTone(row.PrimaryStatus),
			FiveHourTone: pixelTokenPlanTone(row.FiveHourStatus), WeekTone: pixelTokenPlanTone(row.WeekStatus),
			MonthTone: pixelTokenPlanTone(row.MonthStatus), StatusTone: pixelTokenPlanTone(row.Status),
		}
	}
	return frame, true
}

func buildPixelAPIUsageFrame(vm ui.ViewModel) (guirender.APIUsageFrame, bool) {
	model, err := ui.BuildAPIUsageModel(vm)
	if err != nil || len(model.Cards) > guirender.APIUsageRowCapacity {
		return guirender.APIUsageFrame{}, false
	}
	frame := guirender.APIUsageFrame{
		TitleText: model.TitleText, HeaderStatus: model.HeaderStatus,
		FooterStatus: model.FooterStatus, PageText: model.PageText,
		Empty: model.Empty, RowCount: len(model.Cards),
	}
	for index := range model.Cards {
		row := &model.Cards[index]
		frame.Rows[index] = guirender.APIUsageRowFrame{
			Name: row.Name, StatusText: row.StatusText, Currency: row.Currency, Value: row.Value,
			Estimate: row.Estimate, StatusTone: pixelTokenPlanTone(row.Status), LowBalance: row.LowBalance,
		}
	}
	return frame, true
}

func pixelTokenPlanTone(status protocol.DisplayStatus) guirender.TokenPlanTone {
	switch status {
	case protocol.DisplayOK:
		return guirender.TokenPlanToneOK
	case protocol.DisplayWarn, protocol.DisplayStale:
		return guirender.TokenPlanToneWarning
	case protocol.DisplayCrit, protocol.DisplayError, protocol.DisplayAuth:
		return guirender.TokenPlanToneCritical
	case protocol.DisplayDelayed:
		return guirender.TokenPlanToneInfo
	case protocol.DisplayNA:
		return guirender.TokenPlanToneMuted
	default:
		return guirender.TokenPlanToneDefault
	}
}
