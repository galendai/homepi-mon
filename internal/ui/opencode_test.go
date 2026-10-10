package ui_test

import (
	"strings"
	"testing"
	"time"

	"github.com/galendai/homepi-mon/internal/decimal"
	"github.com/galendai/homepi-mon/internal/protocol"
	"github.com/galendai/homepi-mon/internal/ui"
)

func goMetrics(now time.Time) []protocol.ProviderMetric {
	var metrics []protocol.ProviderMetric
	for i, item := range []struct {
		suffix, value string
		window        protocol.Window
	}{
		{"5h", "80", protocol.WindowRolling5h}, {"weekly", "65", protocol.WindowWeekly},
		{"monthly", "5", protocol.WindowMonthly},
	} {
		value, limit := decimal.MustParse(item.value), decimal.MustParse("100")
		metrics = append(metrics, protocol.ProviderMetric{ID: "go." + item.suffix, Provider: "opencode",
			DisplayName: "OpenCode Go", MetricKind: protocol.KindQuota, Group: "coding", Window: item.window,
			Value: &value, Limit: &limit, Unit: "percent", Order: 50 + i, ObservedAt: now,
			Status: protocol.StatusOK, Precision: protocol.PrecisionVerified})
	}
	return metrics
}

func TestGoMonthlyStatusAndAuthentication(t *testing.T) {
	now := at("14:32")
	for _, scenario := range []string{"critical", "stale", "auth"} {
		t.Run(scenario, func(t *testing.T) {
			metrics := goMetrics(now)
			want := protocol.DisplayCrit
			if scenario == "stale" {
				metrics[2].ObservedAt = now.Add(-10 * time.Minute)
				want = protocol.DisplayStale
			}
			if scenario == "auth" {
				metrics[2].ErrorClass = protocol.ErrAuth
				want = protocol.DisplayAuth
			}
			vm := ui.Build(&protocol.MetricSnapshot{GeneratedAt: now, Metrics: metrics}, ui.BuildOptions{Now: now, Thresholds: protocol.DefaultThresholds()})
			card := vm.Coding[0]
			if card.Status != want || ui.CriticalPages(&protocol.MetricSnapshot{Metrics: metrics}, now, protocol.DefaultThresholds())[ui.PageCoding] != (scenario == "critical") {
				t.Fatalf("card=%+v", card)
			}
			frame := ui.RenderString(vm)
			if scenario == "auth" {
				if card.PercentLeft != nil || card.WeekPercentLeft != nil || card.MonthPercentLeft != nil || strings.Contains(frame, "MONTH") {
					t.Fatal(frame)
				}
			} else if *card.PercentLeft != 80 || *card.MonthPercentLeft != 5 || !strings.Contains(frame, "MONTH 5% LEFT") {
				t.Fatal(frame)
			}
		})
	}
}

func TestFiveSubscriptionsRemainVisibleWithoutSplittingCards(t *testing.T) {
	model := func(rotation bool) ui.ViewModel {
		vm := baseModel()
		vm.RotationEnabled = rotation
		vm.DwellSeconds = 15
		vm.Coding = []ui.Card{
			{Name: "Codex", PercentLeft: pct(80), Status: protocol.DisplayOK},
			{Name: "MiniMax", PercentLeft: pct(70), Status: protocol.DisplayOK},
			{Name: "Kimi Coding", PercentLeft: pct(100), MonthTotalUsed: "90.5%", Status: protocol.DisplayCrit},
			{Name: "Grok", PercentLeft: pct(50), WeeklyOnly: true, Status: protocol.DisplayOK},
			{Name: "OpenCode Go", PercentLeft: pct(80), WeekPercentLeft: pct(65), MonthPercentLeft: pct(50), Status: protocol.DisplayOK},
		}
		return vm
	}

	// The legacy overview packs whole two-line cards into the rows below the
	// title; five subscriptions take two subpages.
	for _, style := range []ui.Style{ui.StyleASCII, ui.StyleRich} {
		vm := model(false)
		vm.Now = time.Unix(0, 0)
		first := ui.RenderStyledString(vm, style)
		assertPhase3Grid(t, first, style == ui.StyleRich)
		first = stripSGR(first)
		if !strings.Contains(first, "CODING PLANS 1/2") || !strings.Contains(first, "Kimi Coding") ||
			!strings.Contains(first, "MONTH USED: TOTAL 90.5%") || !strings.Contains(first, "Grok") || strings.Contains(first, "OpenCode Go") {
			t.Fatal(first)
		}
		vm.Now = vm.Now.Add(15 * time.Second)
		second := ui.RenderStyledString(vm, style)
		assertPhase3Grid(t, second, style == ui.StyleRich)
		second = stripSGR(second)
		if !strings.Contains(second, "CODING PLANS 2/2") || !strings.Contains(second, "OpenCode Go") ||
			!strings.Contains(second, "5H 80% | WEEK 65% | MONTH 50% LEFT") {
			t.Fatal(second)
		}
		if style == ui.StyleRich && !strings.Contains(second, "█████████████░░░") {
			t.Fatal(second)
		}
	}

	// The rotating CODING page shows two three-line cards per subpage; five
	// subscriptions take three subpages and every card stays whole.
	for _, style := range []ui.Style{ui.StyleASCII, ui.StyleRich} {
		vm := model(true)
		vm.Now = time.Unix(0, 0)
		first := ui.RenderStyledString(vm, style)
		assertPhase3Grid(t, first, style == ui.StyleRich)
		first = stripSGR(first)
		if !strings.Contains(first, "CODING PLANS 1/3") || !strings.Contains(first, "Codex") ||
			!strings.Contains(first, "MiniMax") || strings.Contains(first, "Kimi Coding") {
			t.Fatal(first)
		}
		vm.Now = vm.Now.Add(15 * time.Second)
		second := ui.RenderStyledString(vm, style)
		assertPhase3Grid(t, second, style == ui.StyleRich)
		second = stripSGR(second)
		if !strings.Contains(second, "CODING PLANS 2/3") || !strings.Contains(second, "Kimi Coding") ||
			!strings.Contains(second, "USED TOT 90.5%") || !strings.Contains(second, "Grok") ||
			strings.Contains(second, "OpenCode Go") {
			t.Fatal(second)
		}
		vm.Now = vm.Now.Add(15 * time.Second)
		third := ui.RenderStyledString(vm, style)
		assertPhase3Grid(t, third, style == ui.StyleRich)
		third = stripSGR(third)
		if !strings.Contains(third, "CODING PLANS 3/3") || !strings.Contains(third, "OpenCode Go") ||
			!strings.Contains(third, "5H 80% | WK 65% | MO 50%") {
			t.Fatal(third)
		}
		if style == ui.StyleRich && !strings.Contains(third, strings.Repeat("█", 27)+strings.Repeat("░", 7)) {
			t.Fatal(third)
		}
	}
}
