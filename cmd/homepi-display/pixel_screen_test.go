package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/galendai/homepi-mon/internal/guirender"
	"github.com/galendai/homepi-mon/internal/protocol"
	"github.com/galendai/homepi-mon/internal/ui"
)

type fakePixelRenderer struct {
	homeFrames      []guirender.HomeFrame
	tokenPlanFrames []guirender.TokenPlanFrame
	apiUsageFrames  []guirender.APIUsageFrame
	openClashFrames []guirender.OpenClashFrame
	legacyFrames    []string
	openClashErr    error
}

func (f *fakePixelRenderer) DrawHome(frame guirender.HomeFrame) error {
	f.homeFrames = append(f.homeFrames, frame)
	return nil
}

func (f *fakePixelRenderer) DrawTokenPlan(frame guirender.TokenPlanFrame) error {
	f.tokenPlanFrames = append(f.tokenPlanFrames, frame)
	return nil
}

func (f *fakePixelRenderer) DrawAPIUsage(frame guirender.APIUsageFrame) error {
	f.apiUsageFrames = append(f.apiUsageFrames, frame)
	return nil
}

func (f *fakePixelRenderer) DrawOpenClash(frame guirender.OpenClashFrame) error {
	f.openClashFrames = append(f.openClashFrames, frame)
	return f.openClashErr
}

func (f *fakePixelRenderer) DrawLegacy(frame string) error {
	f.legacyFrames = append(f.legacyFrames, frame)
	return nil
}

func (f *fakePixelRenderer) Close() error { return nil }

func TestPixelScreenFallsBackToFb1(t *testing.T) {
	original := rendererFactory
	defer func() { rendererFactory = original }()
	var attempts []string
	rendererFactory = func(device string) (guirender.Renderer, error) {
		attempts = append(attempts, device)
		if device == "/dev/fb0" {
			return nil, errors.New("missing")
		}
		return &fakePixelRenderer{}, nil
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	screen, err := newPixelScreen(io.Discard, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer screen.renderer.Close()
	if want := []string{"/dev/fb0", "/dev/fb1"}; !reflect.DeepEqual(attempts, want) {
		t.Fatalf("framebuffer attempts = %v, want %v", attempts, want)
	}
}

func TestPixelScreenReportsAllFramebufferFailures(t *testing.T) {
	original := rendererFactory
	defer func() { rendererFactory = original }()
	rendererFactory = func(device string) (guirender.Renderer, error) {
		return nil, errors.New("unavailable")
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if _, err := newPixelScreen(io.Discard, logger); err == nil ||
		!bytes.Contains([]byte(err.Error()), []byte("/dev/fb1")) {
		t.Fatalf("missing framebuffer error = %v", err)
	}
}

func TestPixelScreenSkipsUnchangedFramesAndPreservesRemoteNotice(t *testing.T) {
	renderer := &fakePixelRenderer{}
	screen := &pixelScreen{out: &bytes.Buffer{}, renderer: renderer}
	now := time.Date(2026, time.November, 24, 14, 32, 0, 0, time.Local)
	vm := ui.ViewModel{Page: string(ui.PageHome), NodeLabel: "GALEN-MAC", Now: now, PageNumber: 1, PageCount: 6}
	if err := screen.draw(vm); err != nil {
		t.Fatal(err)
	}
	vm.Now = now.Add(time.Second)
	if err := screen.draw(vm); err != nil {
		t.Fatal(err)
	}
	if len(renderer.homeFrames) != 1 {
		t.Fatalf("unchanged home drew %d frames", len(renderer.homeFrames))
	}
	vm.Now = now.Add(time.Minute)
	if err := screen.draw(vm); err != nil {
		t.Fatal(err)
	}
	if len(renderer.homeFrames) != 2 || renderer.homeFrames[1].TimeText != "14:33" {
		t.Fatalf("minute change did not reach home: %+v", renderer.homeFrames)
	}
	vm.RemoteNotice = &ui.RemoteNotice{Text: "MAINTENANCE", Severity: "warning", Source: "dev-mac", ReturnIn: 30 * time.Second}
	if err := screen.draw(vm); err != nil {
		t.Fatal(err)
	}
	if len(renderer.legacyFrames) != 1 || !bytes.Contains([]byte(renderer.legacyFrames[0]), []byte("MAINTENANCE")) {
		t.Fatalf("remote notice did not replace home: %+v", renderer.legacyFrames)
	}
	if err := screen.draw(vm); err != nil {
		t.Fatal(err)
	}
	if len(renderer.legacyFrames) != 1 {
		t.Fatal("unchanged remote notice was redrawn")
	}
}

func TestPixelScreenRedrawsWhenOnlyHomeSummaryChanges(t *testing.T) {
	renderer := &fakePixelRenderer{}
	screen := &pixelScreen{out: io.Discard, renderer: renderer}
	humidity := 56
	vm := ui.ViewModel{Page: string(ui.PageHome), Now: time.Now(), Link: protocol.LinkLive,
		Weather: &protocol.WeatherSummary{Status: protocol.StatusOK, HumidityPercent: &humidity}}
	draw := func() guirender.HomeFrame {
		t.Helper()
		if err := screen.draw(vm); err != nil {
			t.Fatal(err)
		}
		return renderer.homeFrames[len(renderer.homeFrames)-1]
	}
	if frame := draw(); frame.WeatherDetail != "RH 56%" || frame.MonitorDetail != "" {
		t.Fatal(frame)
	}
	vm.Coding = []ui.Card{{Name: "Codex", Status: protocol.DisplayCrit, AlertDetail: "5H   8% LEFT"}}
	if frame := draw(); frame.MonitorSource != "CODEX" || frame.MonitorDetail != "5H   8% LEFT" ||
		frame.MonitorTone != guirender.TokenPlanToneCritical {
		t.Fatal(frame)
	}
	vm.Weather.HumidityPercent = nil
	if frame := draw(); frame.WeatherDetail != "" {
		t.Fatal(frame)
	}
	draw()
	if len(renderer.homeFrames) != 3 {
		t.Fatalf("changed HOME summary frames = %d, want 3", len(renderer.homeFrames))
	}
}

func TestPixelScreenDrawsTokenPlanCards(t *testing.T) {
	renderer := &fakePixelRenderer{}
	screen := &pixelScreen{out: io.Discard, renderer: renderer}
	remaining, age := 68, 65*time.Second
	vm := ui.ViewModel{
		NodeLabel: "DEV-MAC", Page: string(ui.PageCoding), Link: protocol.LinkLive, HasSnapshot: true,
		Coding: []ui.Card{
			{Name: "MiniMax", PercentLeft: &remaining, ResetFine: "RESET 2H14M", Status: protocol.DisplayOK},
			{Name: "Codex", PercentLeft: &remaining, WeekPercentLeft: &remaining, Status: protocol.DisplayOK},
			{Name: "Kimi Code", PercentLeft: &remaining, WeekPercentLeft: &remaining, Status: protocol.DisplayOK},
			{Name: "OpenCode Go", PercentLeft: &remaining, Status: protocol.DisplayOK},
			{Name: "Plan Five", PercentLeft: &remaining, Status: protocol.DisplayOK},
			{Name: "Plan Six", PercentLeft: &remaining, Status: protocol.DisplayOK},
		},
		API:       []ui.Card{{Name: "Kimi API", Amount: "CNY 49.58", Status: protocol.DisplayOK}},
		SyncedAgo: &age, RotationEnabled: true, PageNumber: 2, PageCount: 6, DwellSeconds: 30,
	}
	if err := screen.draw(vm); err != nil {
		t.Fatal(err)
	}
	if len(renderer.tokenPlanFrames) != 1 || len(renderer.legacyFrames) != 0 {
		t.Fatalf("native frames=%d legacy frames=%d", len(renderer.tokenPlanFrames), len(renderer.legacyFrames))
	}
	frame := renderer.tokenPlanFrames[0]
	if frame.PlanCount != 6 || frame.Plans[5].Name != "Plan Six" ||
		frame.Plans[0].Name != "MiniMax" || frame.Plans[0].PrimaryLabel != "5H LEFT" ||
		frame.Plans[0].PrimaryValue != "68%" || frame.Plans[0].FiveHour != "68%" ||
		frame.Plans[0].Week != "--" || frame.Plans[0].Reset != "2H14M" {
		t.Fatalf("unexpected token plan cards: %+v", frame.Plans)
	}
	if len(renderer.apiUsageFrames) != 0 {
		t.Fatal("Token Plan page drew API Usage cards")
	}
	if frame.FooterStatus != "0 ALERTS | SNAPSHOT 1M AGO" || frame.PageText != "2/6 AUTO 30S" {
		t.Fatalf("unexpected status: %+v", frame)
	}
	if err := screen.draw(vm); err != nil {
		t.Fatal(err)
	}
	if len(renderer.tokenPlanFrames) != 1 {
		t.Fatal("unchanged token plan was redrawn")
	}
	low := 19
	vm.Coding[0].PercentLeft = &low
	if err := screen.draw(vm); err != nil {
		t.Fatal(err)
	}
	if len(renderer.tokenPlanFrames) != 2 || renderer.tokenPlanFrames[1].Plans[0].FiveHour != "19%" ||
		renderer.tokenPlanFrames[1].Plans[0].PrimaryTone != guirender.TokenPlanToneCritical ||
		renderer.tokenPlanFrames[1].Plans[0].FiveHourTone != guirender.TokenPlanToneCritical {
		t.Fatalf("low quota did not update: %+v", renderer.tokenPlanFrames)
	}
}

func TestPixelScreenTokenPlanFallbacksPreserveContent(t *testing.T) {
	remaining := 68
	base := ui.ViewModel{
		NodeLabel: "DEV-MAC", Page: string(ui.PageCoding), Link: protocol.LinkLive, HasSnapshot: true,
		Coding: []ui.Card{{Name: "MiniMax", PercentLeft: &remaining, Status: protocol.DisplayOK}},
		API:    []ui.Card{{Name: "Kimi API", Amount: "CNY 49.58", Status: protocol.DisplayOK}},
	}
	tests := map[string]struct {
		mutate func(*ui.ViewModel)
		want   string
	}{
		"capacity": {func(vm *ui.ViewModel) {
			for index := 1; index <= guirender.TokenPlanRowCapacity; index++ {
				vm.Coding = append(vm.Coding, ui.Card{Name: fmt.Sprintf("PLAN-%d", index), PercentLeft: &remaining, Status: protocol.DisplayOK})
			}
		}, "PLAN-6"},
		"action": {func(vm *ui.ViewModel) {
			vm.Coding[0].Status = protocol.DisplayAuth
			vm.Coding[0].ActionHint = "update API key on DEV-MAC"
		}, "ACTION: update API key on DEV-MAC"},
		"wide quota": {func(vm *ui.ViewModel) {
			wide := 100000
			vm.Coding[0].PercentLeft = &wide
		}, "100000%"},
		"remote": {func(vm *ui.ViewModel) {
			vm.RemoteNotice = &ui.RemoteNotice{Text: "MAINTENANCE", Severity: "warning", Source: "dev-mac", ReturnIn: 30 * time.Second}
		}, "MAINTENANCE"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			renderer := &fakePixelRenderer{}
			screen := &pixelScreen{out: io.Discard, renderer: renderer}
			vm := base
			vm.Coding = append([]ui.Card(nil), base.Coding...)
			tc.mutate(&vm)
			if err := screen.draw(vm); err != nil {
				t.Fatal(err)
			}
			if len(renderer.tokenPlanFrames) != 0 || len(renderer.legacyFrames) != 1 ||
				!strings.Contains(renderer.legacyFrames[0], tc.want) {
				t.Fatalf("fallback lost %q: native=%d legacy=%+v", tc.want, len(renderer.tokenPlanFrames), renderer.legacyFrames)
			}
		})
	}
}

func TestPixelScreenDrawsAPIUsageCards(t *testing.T) {
	renderer := &fakePixelRenderer{}
	screen := &pixelScreen{out: io.Discard, renderer: renderer}
	vm := ui.ViewModel{
		NodeLabel: "DEV-MAC", Page: string(ui.PageAPI), Link: protocol.LinkLive, HasSnapshot: true,
		PageNumber: 3, PageCount: 6, RotationEnabled: true, DwellSeconds: 30,
	}
	for index := range guirender.APIUsageRowCapacity {
		vm.API = append(vm.API, ui.Card{
			Name: fmt.Sprintf("API-%d", index+1), Amount: fmt.Sprintf("CNY %d.00", index+1), Status: protocol.DisplayOK,
		})
	}
	if err := screen.draw(vm); err != nil {
		t.Fatal(err)
	}
	if len(renderer.apiUsageFrames) != 1 || len(renderer.legacyFrames) != 0 || len(renderer.tokenPlanFrames) != 0 {
		t.Fatalf("API page routing is wrong: API=%d legacy=%d plans=%d",
			len(renderer.apiUsageFrames), len(renderer.legacyFrames), len(renderer.tokenPlanFrames))
	}
	frame := renderer.apiUsageFrames[0]
	if frame.RowCount != guirender.APIUsageRowCapacity || frame.Rows[7].Amount != "CNY 8.00" || frame.PageText != "3/6 AUTO 30S" {
		t.Fatalf("unexpected API Usage frame: %+v", frame)
	}
	if err := screen.draw(vm); err != nil {
		t.Fatal(err)
	}
	if len(renderer.apiUsageFrames) != 1 {
		t.Fatal("unchanged API Usage page was redrawn")
	}
	vm.API[0].Amount = "CNY 9.00"
	if err := screen.draw(vm); err != nil {
		t.Fatal(err)
	}
	if len(renderer.apiUsageFrames) != 2 || renderer.apiUsageFrames[1].Rows[0].Amount != "CNY 9.00" {
		t.Fatal("API amount change did not reach the native page")
	}
}

func TestPixelScreenAPIUsageOverflowFallsBack(t *testing.T) {
	renderer := &fakePixelRenderer{}
	screen := &pixelScreen{out: io.Discard, renderer: renderer}
	vm := ui.ViewModel{Page: string(ui.PageAPI), Link: protocol.LinkLive, HasSnapshot: true, RotationEnabled: true, PageCount: 6, DwellSeconds: 15}
	for index := range guirender.APIUsageRowCapacity + 1 {
		vm.API = append(vm.API, ui.Card{Name: fmt.Sprintf("API-%d", index+1), Amount: "CNY 1.00", Status: protocol.DisplayOK})
	}
	if err := screen.draw(vm); err != nil {
		t.Fatal(err)
	}
	if len(renderer.apiUsageFrames) != 0 || len(renderer.legacyFrames) != 1 ||
		!strings.Contains(renderer.legacyFrames[0], "API-9") {
		t.Fatalf("API overflow was not preserved by legacy rendering: %+v", renderer.legacyFrames)
	}
}

func TestPixelScreenAPIUsageActionFallsBackWithRemedy(t *testing.T) {
	renderer := &fakePixelRenderer{}
	screen := &pixelScreen{out: io.Discard, renderer: renderer}
	vm := ui.ViewModel{
		Page: string(ui.PageAPI), Link: protocol.LinkLive, HasSnapshot: true,
		RotationEnabled: false, PageCount: 1, DwellSeconds: 15,
		Coding: []ui.Card{{Name: "MiniMax"}},
		API: []ui.Card{{
			Name: "Kimi API", Status: protocol.DisplayAuth, ActionHint: "update API key on DEV-MAC",
		}},
	}
	if err := screen.draw(vm); err != nil {
		t.Fatal(err)
	}
	if len(renderer.apiUsageFrames) != 0 || len(renderer.legacyFrames) != 1 ||
		!strings.Contains(renderer.legacyFrames[0], "ACTION: update API key on DEV-MAC") ||
		strings.Contains(renderer.legacyFrames[0], "CODING PLANS") {
		t.Fatalf("paused single-page API fallback was wrong: %+v", renderer.legacyFrames)
	}
}

func TestPixelScreenAPIUsageActionsRotateCompleteLegacyPages(t *testing.T) {
	for _, scenario := range []struct {
		name       string
		cards      int
		actionFrom int
	}{
		{name: "all actions", cards: 6, actionFrom: 0},
		{name: "tail action", cards: 10, actionFrom: 9},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			renderer := &fakePixelRenderer{}
			screen := &pixelScreen{out: io.Discard, renderer: renderer}
			vm := ui.ViewModel{
				Page: string(ui.PageAPI), Link: protocol.LinkLive, HasSnapshot: true,
				Now: time.Unix(0, 0), RotationEnabled: true, PageCount: 6, DwellSeconds: 15,
			}
			for index := range scenario.cards {
				card := ui.Card{Name: fmt.Sprintf("API-%d", index), Amount: "CNY 1.00", Status: protocol.DisplayAuth}
				if index >= scenario.actionFrom {
					card.ActionHint = fmt.Sprintf("remedy-%d", index)
				}
				vm.API = append(vm.API, card)
			}
			var frames []string
			for range 2 {
				if err := screen.draw(vm); err != nil {
					t.Fatal(err)
				}
				vm.Now = vm.Now.Add(15 * time.Second)
			}
			if len(renderer.apiUsageFrames) != 0 || len(renderer.legacyFrames) != 2 {
				t.Fatalf("API fallback frames = %+v", renderer.legacyFrames)
			}
			frames = append(frames, renderer.legacyFrames...)
			combined := strings.Join(frames, "\n")
			if !strings.Contains(frames[0], "API BALANCE 1/2") || !strings.Contains(frames[1], "API BALANCE 2/2") {
				t.Fatalf("API page labels missing: %+v", frames)
			}
			for index := range scenario.cards {
				if !strings.Contains(combined, fmt.Sprintf("API-%d", index)) {
					t.Fatalf("API-%d missing from legacy pages", index)
				}
				if index >= scenario.actionFrom && !strings.Contains(combined, fmt.Sprintf("ACTION: remedy-%d", index)) {
					t.Fatalf("ACTION remedy-%d missing from legacy pages", index)
				}
			}
		})
	}
}

func TestPixelScreenDrawsOpenClashAndSkipsUnchangedFrames(t *testing.T) {
	renderer := &fakePixelRenderer{}
	screen := &pixelScreen{out: io.Discard, renderer: renderer}
	now := time.Date(2026, time.November, 25, 12, 0, 0, 0, time.UTC)
	checkedAt := now.Add(-time.Second)
	delay := 120
	alive := true
	age := time.Second
	vm := ui.ViewModel{
		Page: string(ui.PageOpenClash), HasSnapshot: true, Now: now,
		Link: protocol.LinkLive, SyncedAgo: &age, RotationEnabled: true,
		PageNumber: 4, PageCount: 4, DwellSeconds: 15,
		OpenClash: &protocol.OpenClashReport{
			ObservedAt: now, StaleAfter: protocol.Duration(time.Minute),
			SelectedGroup: "PROXY", SelectedNode: "BETA", TotalConnections: 3, Status: protocol.StatusOK,
			Nodes: []protocol.OpenClashNode{
				{Name: "BETA", Alive: &alive, DelayMS: &delay, CheckedAt: &checkedAt, ActiveConnections: 2},
				{Name: "ALPHA", Alive: &alive, DelayMS: &delay, CheckedAt: &checkedAt, ActiveConnections: 1},
			},
		},
	}
	if err := screen.draw(vm); err != nil {
		t.Fatal(err)
	}
	if len(renderer.openClashFrames) != 1 || len(renderer.legacyFrames) != 0 {
		t.Fatalf("OpenClash routing native=%d legacy=%d", len(renderer.openClashFrames), len(renderer.legacyFrames))
	}
	frame := renderer.openClashFrames[0]
	if frame.RowCount != 2 || frame.Rows[0].Name != "ALPHA" || frame.Rows[1].Name != "BETA" ||
		frame.Rows[1].Delay != "120ms" || !frame.Rows[1].Selected ||
		frame.Rows[1].Tone != guirender.TokenPlanToneOK || frame.SubpageText != "OPENCLASH 1/1" ||
		frame.PageText != "4/4 AUTO 15S" {
		t.Fatalf("unexpected OpenClash frame: %+v", frame)
	}
	if err := screen.draw(vm); err != nil {
		t.Fatal(err)
	}
	if len(renderer.openClashFrames) != 1 {
		t.Fatal("unchanged OpenClash frame was redrawn")
	}
	delay = 850
	if err := screen.draw(vm); err != nil {
		t.Fatal(err)
	}
	if len(renderer.openClashFrames) != 2 || renderer.openClashFrames[1].Rows[0].Tone != guirender.TokenPlanToneCritical {
		t.Fatalf("delay change did not reach native page: %+v", renderer.openClashFrames)
	}
	vm.RemoteNotice = &ui.RemoteNotice{Text: "MAINTENANCE", Severity: "warning", Source: "dev-mac", ReturnIn: 30 * time.Second}
	if err := screen.draw(vm); err != nil {
		t.Fatal(err)
	}
	if len(renderer.legacyFrames) != 1 || !strings.Contains(renderer.legacyFrames[0], "REMOTE NOTICE") {
		t.Fatalf("OpenClash remote notice fallback = %+v", renderer.legacyFrames)
	}
}

func TestPixelScreenOpenClashSubpagesRotateTenCardPages(t *testing.T) {
	renderer := &fakePixelRenderer{}
	screen := &pixelScreen{out: io.Discard, renderer: renderer}
	now := time.Unix(900, 0)
	checkedAt := now.Add(-time.Second)
	delay := 120
	alive := true
	vm := ui.ViewModel{
		Page: string(ui.PageOpenClash), HasSnapshot: true, Now: now,
		Link: protocol.LinkLive, RotationEnabled: true, DwellSeconds: 15,
		OpenClash: &protocol.OpenClashReport{
			ObservedAt: now, StaleAfter: protocol.Duration(time.Minute),
			SelectedGroup: "PROXY", SelectedNode: "NODE-12", TotalConnections: 5, Status: protocol.StatusOK,
		},
	}
	for index := 1; index <= 12; index++ {
		vm.OpenClash.Nodes = append(vm.OpenClash.Nodes, protocol.OpenClashNode{
			Name: fmt.Sprintf("NODE-%02d", index), Alive: &alive, DelayMS: &delay, CheckedAt: &checkedAt,
		})
	}
	if err := screen.draw(vm); err != nil {
		t.Fatal(err)
	}
	if len(renderer.openClashFrames) != 1 {
		t.Fatalf("first sub-page frames = %d", len(renderer.openClashFrames))
	}
	first := renderer.openClashFrames[0]
	if first.RowCount != 10 || first.Rows[0].Name != "NODE-01" || first.Rows[9].Name != "NODE-10" ||
		first.SubpageText != "OPENCLASH 1/2" {
		t.Fatalf("first sub-page frame = %+v", first)
	}
	if err := screen.draw(vm); err != nil {
		t.Fatal(err)
	}
	if len(renderer.openClashFrames) != 1 {
		t.Fatal("unchanged sub-page frame was redrawn")
	}
	vm.Now = now.Add(15 * time.Second)
	if err := screen.draw(vm); err != nil {
		t.Fatal(err)
	}
	if len(renderer.openClashFrames) != 2 {
		t.Fatalf("second sub-page frames = %d", len(renderer.openClashFrames))
	}
	second := renderer.openClashFrames[1]
	if second.RowCount != 2 || second.Rows[0].Name != "NODE-11" ||
		second.Rows[1].Name != "NODE-12" || !second.Rows[1].Selected ||
		second.Rows[1].Delay != "120ms" || second.SubpageText != "OPENCLASH 2/2" {
		t.Fatalf("second sub-page frame = %+v", second)
	}
}

func TestPixelScreenOpenClashNativeErrorFallsBackToLegacy(t *testing.T) {
	renderer := &fakePixelRenderer{openClashErr: errors.New("native width exceeded")}
	screen := &pixelScreen{out: io.Discard, renderer: renderer}
	now := time.Date(2026, time.November, 25, 12, 0, 0, 0, time.UTC)
	checkedAt := now.Add(-time.Second)
	delay := 120
	alive := true
	age := time.Second
	vm := ui.ViewModel{
		Page: string(ui.PageOpenClash), HasSnapshot: true, Now: now,
		Link: protocol.LinkLive, SyncedAgo: &age, RotationEnabled: true,
		PageNumber: 4, PageCount: 4, DwellSeconds: 15,
		OpenClash: &protocol.OpenClashReport{
			ObservedAt: now, StaleAfter: protocol.Duration(time.Minute),
			SelectedGroup: "PROXY", SelectedNode: "BETA", TotalConnections: 3, Status: protocol.StatusOK,
			Nodes: []protocol.OpenClashNode{
				{Name: "BETA", Alive: &alive, DelayMS: &delay, CheckedAt: &checkedAt, ActiveConnections: 2},
				{Name: "ALPHA", Alive: &alive, DelayMS: &delay, CheckedAt: &checkedAt, ActiveConnections: 1},
			},
		},
	}
	if err := screen.draw(vm); err != nil {
		t.Fatal(err)
	}
	if len(renderer.openClashFrames) != 1 || len(renderer.legacyFrames) != 1 ||
		!strings.Contains(renderer.legacyFrames[0], "ALPHA") {
		t.Fatalf("OpenClash native failure fallback = native:%d legacy:%d",
			len(renderer.openClashFrames), len(renderer.legacyFrames))
	}
	if screen.mode != "legacy" {
		t.Fatalf("fallback mode = %q", screen.mode)
	}
}

func TestPixelScreenOpenClashNoDataUsesNativeEmptyState(t *testing.T) {
	renderer := &fakePixelRenderer{}
	screen := &pixelScreen{out: io.Discard, renderer: renderer}
	if err := screen.draw(ui.ViewModel{Page: string(ui.PageOpenClash), HasSnapshot: true, Now: time.Unix(0, 0)}); err != nil {
		t.Fatal(err)
	}
	if len(renderer.openClashFrames) != 1 || renderer.openClashFrames[0].Empty != "NO OPENCLASH DATA" ||
		renderer.openClashFrames[0].SubpageText != "OPENCLASH 1/1" ||
		len(renderer.legacyFrames) != 0 {
		t.Fatalf("OpenClash empty state = %+v", renderer.openClashFrames)
	}
}
