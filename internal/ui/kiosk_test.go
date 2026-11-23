package ui_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/galendai/homepi-mon/internal/protocol"
	"github.com/galendai/homepi-mon/internal/ui"
)

func kioskModel() ui.ViewModel {
	vm := compactModel()
	vm.Page = string(ui.PageCoding)
	vm.RotationEnabled = true
	vm.PageNumber, vm.PageCount, vm.DwellSeconds = 1, 5, 15
	return vm
}

func TestKioskCodingDefaultsToCompleteCompactOverview(t *testing.T) {
	vm := kioskModel()
	for _, style := range []ui.Style{ui.StyleASCII, ui.StyleRich} {
		frame := ui.RenderKioskString(vm, style)
		for _, token := range []string{
			"OVERVIEW", "Codex", "MiniMax", "Kimi Code", "Grok", "OpenCode Go",
			"CNY 8.20", "CNY 49.58",
			"USD -1.24", "CNY 123456789.12", "PI 52C", "CPU 3%", "MEM 18%", "1/5 AUTO 15S",
		} {
			if !strings.Contains(stripSGR(frame), token) {
				t.Errorf("%s: missing %q in kiosk frame", style, token)
			}
		}
		assertPhase3Grid(t, frame, style == ui.StyleRich)
	}
}

func TestKioskPreservesOtherDetailPagesAndRemoteNotice(t *testing.T) {
	vm := kioskModel()
	for _, style := range []ui.Style{ui.StyleASCII, ui.StyleRich} {
		for _, page := range []ui.Page{ui.PageAPI, ui.PageHomeLab, ui.PageServices, ui.PageSystem} {
			vm.Page = string(page)
			if got, want := ui.RenderKioskString(vm, style), ui.RenderStyledString(vm, style); got != want {
				t.Errorf("%s %s detail changed", page, style)
			}
		}
		vm.Page = string(ui.PageCoding)
		vm.RemoteNotice = &ui.RemoteNotice{
			Text: "MAINTENANCE\x1b[2J\nIN 10 MINUTES", Severity: "warning",
			Source: "dev-mac", ReturnIn: 25 * time.Second,
		}
		notice := ui.RenderKioskString(vm, style)
		if notice != ui.RenderStyledString(vm, style) {
			t.Fatalf("%s remote notice layout changed", style)
		}
		for _, token := range []string{"REMOTE NOTICE", "WARN", "SOURCE DEV-MAC", "RETURN IN 25S"} {
			if !strings.Contains(stripSGR(notice), token) {
				t.Errorf("%s: notice lost %q", style, token)
			}
		}
		assertPhase3Grid(t, notice, style == ui.StyleRich)
		vm.RemoteNotice = nil
		if !strings.Contains(ui.RenderKioskString(vm, style), "OVERVIEW") {
			t.Errorf("%s: notice did not return to overview", style)
		}
	}
}

func TestKioskOverflowFallsBackToLabelledDetailsWithoutLosingPlans(t *testing.T) {
	vm := kioskModel()
	vm.Coding = nil
	for i := 0; i < 15; i++ {
		vm.Coding = append(vm.Coding, ui.Card{
			Name: fmt.Sprintf("Plan-%02d", i), PercentLeft: pct(42), Status: protocol.DisplayOK,
		})
	}
	for _, style := range []ui.Style{ui.StyleASCII, ui.StyleRich} {
		seen := make(map[string]bool)
		for i := 0; i < 8; i++ {
			frame := ui.RenderKioskString(vm, style)
			if !strings.Contains(stripSGR(frame), "COMPACT FULL") || strings.Contains(frame, "OVERVIEW") {
				t.Fatalf("%s overflow claimed to be a complete overview", style)
			}
			if !strings.Contains(stripSGR(frame), "CODING PLANS ") || !strings.Contains(stripSGR(frame), "/8") {
				t.Fatalf("%s fallback pagination missing", style)
			}
			assertPhase3Grid(t, frame, style == ui.StyleRich)
			for _, card := range vm.Coding {
				if strings.Contains(stripSGR(frame), card.Name) {
					seen[card.Name] = true
				}
			}
			vm.Now = vm.Now.Add(15 * time.Second)
		}
		if len(seen) != len(vm.Coding) {
			t.Errorf("%s: saw %d of %d plans", style, len(seen), len(vm.Coding))
		}
	}
	wide := kioskModel()
	wide.API[0].Amount = "CNY " + strings.Repeat("1", 30) + ".00"
	if frame := ui.RenderKioskString(wide, ui.StyleASCII); !strings.Contains(frame, "COMPACT FULL") {
		t.Fatal("oversized balance was silently clipped into an overview")
	}
}

func TestKioskPausedOverviewDoesNotChangeForAgeSeconds(t *testing.T) {
	vm := kioskModel()
	vm.RotationEnabled = false
	age := 31 * time.Second
	vm.SyncedAgo = &age
	first := ui.RenderKioskString(vm, ui.StyleASCII)
	vm.Now = vm.Now.Add(time.Second)
	age += time.Second
	second := ui.RenderKioskString(vm, ui.StyleASCII)
	if first != second {
		t.Fatal("paused overview changed for a one-second age tick")
	}
	if !strings.Contains(first, "1/5 HOLD") {
		t.Fatal("paused rotation is not visible")
	}
	if age != 32*time.Second {
		t.Fatal("renderer mutated the caller's precise snapshot age")
	}
	vm.Coding[0].Status = protocol.DisplayStale
	if second == ui.RenderKioskString(vm, ui.StyleASCII) {
		t.Fatal("freshness status changes were suppressed")
	}
}

func TestKioskOverviewUsesExactFreshnessDespiteMinuteAgeLabel(t *testing.T) {
	now := at("14:32")
	snapshot := &protocol.MetricSnapshot{GeneratedAt: now, Metrics: goMetrics(now)[:1]}
	snapshot.Metrics[0].StaleAfter = protocol.Duration(40 * time.Second)
	render := func(elapsed time.Duration) string {
		vm := ui.Build(snapshot, ui.BuildOptions{
			Page: string(ui.PageCoding), Now: now.Add(elapsed), Connected: true,
			Thresholds: protocol.DefaultThresholds(), PageNumber: 1, PageCount: 5,
		})
		return ui.RenderKioskString(vm, ui.StyleASCII)
	}
	fresh, stale := render(39*time.Second), render(41*time.Second)
	if strings.Contains(fresh, "STALE") || !strings.Contains(stale, "STALE") || fresh == stale {
		t.Fatal("minute-age presentation delayed the actual freshness transition")
	}
	if !strings.Contains(fresh, "SNAPSHOT 0S AGO") || !strings.Contains(stale, "SNAPSHOT 0S AGO") {
		t.Fatal("test did not hold the visible snapshot age constant across the transition")
	}
	if !strings.Contains(render(61*time.Second), "SNAPSHOT 1M AGO") {
		t.Fatal("overview snapshot age did not advance after a minute")
	}
}

func TestKioskEmptyOverviewKeepsWaitStateAndGrid(t *testing.T) {
	vm := kioskModel()
	vm.Coding, vm.API, vm.SyncedAgo = nil, nil, nil
	vm.HasSnapshot = false
	vm.Link = protocol.LinkWait
	for _, style := range []ui.Style{ui.StyleASCII, ui.StyleRich} {
		frame := ui.RenderKioskString(vm, style)
		for _, token := range []string{"OVERVIEW", "WAIT", "NO SNAPSHOT", "NO DATA", "1/5 AUTO 15S"} {
			if !strings.Contains(stripSGR(frame), token) {
				t.Errorf("%s empty overview lost %q", style, token)
			}
		}
		if strings.Contains(frame, "0%") {
			t.Fatal("empty overview invented quota data")
		}
		assertPhase3Grid(t, frame, style == ui.StyleRich)
	}
}
