package ui_test

import (
	"strings"
	"testing"
	"time"

	"github.com/galendai/homepi-mon/internal/decimal"
	"github.com/galendai/homepi-mon/internal/protocol"
	"github.com/galendai/homepi-mon/internal/ui"
)

func monthlyMetrics(now time.Time) []protocol.ProviderMetric {
	metrics := make([]protocol.ProviderMetric, 0, 3)
	for _, item := range []struct {
		suffix, value string
		window        protocol.Window
		order         int
	}{
		{"5h", "1", protocol.WindowRolling5h, 30},
		{"monthly-total", "0.0953", protocol.WindowMonthly, 32},
		{"monthly-code", "0.0971", protocol.WindowMonthly, 33},
	} {
		value, limit := decimal.MustParse(item.value), decimal.MustParse("1")
		metrics = append(metrics, protocol.ProviderMetric{
			ID: "kimi-main." + item.suffix, Provider: "kimi", DisplayName: "Kimi Coding",
			MetricKind: protocol.KindQuota, Group: "coding", Value: &value, Limit: &limit,
			Unit: "ratio", Window: item.window, Order: item.order, ObservedAt: now,
			Status: protocol.StatusOK, Precision: protocol.PrecisionVerified,
		})
	}
	return metrics
}

func TestMonthlyUsageKeepsPrimaryAndParticipatesInStatus(t *testing.T) {
	now := at("14:32")
	for _, scenario := range []string{"critical", "stale", "auth"} {
		t.Run(scenario, func(t *testing.T) {
			metrics := monthlyMetrics(now)
			want := protocol.DisplayCrit
			switch scenario {
			case "stale":
				metrics[1].ObservedAt = now.Add(-10 * time.Minute)
				metrics[2].ObservedAt = metrics[1].ObservedAt
				want = protocol.DisplayStale
			case "auth":
				metrics[0].ErrorClass = protocol.ErrAuth
				want = protocol.DisplayAuth
			}
			// Reverse input to ensure monthly data cannot overwrite the main bar.
			metrics[0], metrics[2] = metrics[2], metrics[0]
			snap := &protocol.MetricSnapshot{SourceNode: "dev-mac", GeneratedAt: now, Metrics: metrics}
			opts := ui.BuildOptions{Now: now, Connected: true, Thresholds: protocol.DefaultThresholds()}
			vm := ui.Build(snap, opts)
			card := vm.Coding[0]
			wantAlerts := 1
			if scenario == "stale" {
				wantAlerts = 0
			}
			if card.Status != want || vm.AlertCount != wantAlerts {
				t.Fatalf("card=%+v, alerts=%d", card, vm.AlertCount)
			}
			frame := ui.RenderString(vm)
			if scenario == "auth" {
				if card.PercentLeft != nil || card.MonthTotalUsed != "" || card.MonthCodeUsed != "" ||
					strings.Contains(frame, "90.5%") || strings.Contains(frame, "MONTH USED") {
					t.Fatal(frame)
				}
			} else if card.PercentLeft == nil || *card.PercentLeft != 100 ||
				card.MonthTotalUsed != "90.5%" || card.MonthCodeUsed != "90.3%" {
				t.Fatalf("card=%+v", card)
			}
			if got := ui.CriticalPages(snap, now, opts.Thresholds)[ui.PageCoding]; got != (scenario == "critical") {
				t.Fatalf("critical page = %v", got)
			}
		})
	}
}

func TestMonthlyUsageOptionalValuesAndPrecision(t *testing.T) {
	now := at("14:32")
	for _, tc := range []struct{ name, value, want string }{
		{"unused", "1", "0.0%"}, {"exhausted", "0", "100.0%"},
		{"half rounding", "0.8755", "12.5%"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			metrics := monthlyMetrics(now)
			metrics = metrics[:2]
			value := decimal.MustParse(tc.value)
			metrics[1].Value = &value
			vm := ui.Build(&protocol.MetricSnapshot{GeneratedAt: now, Metrics: metrics}, ui.BuildOptions{Now: now})
			card := vm.Coding[0]
			if card.MonthTotalUsed != tc.want || card.MonthCodeUsed != "" {
				t.Fatalf("card=%+v", card)
			}
			frame := ui.RenderString(vm)
			if !strings.Contains(frame, "MONTH USED: TOTAL "+tc.want) || strings.Contains(frame, "CODE ") {
				t.Fatal(frame)
			}
		})
	}
	vm := baseModel()
	vm.Coding = []ui.Card{{Name: "Kimi Coding", PercentLeft: pct(100), MonthCodeUsed: "25.0%", Status: protocol.DisplayOK}}
	if frame := ui.RenderString(vm); !strings.Contains(frame, "MONTH USED: CODE 25.0%") || strings.Contains(frame, "TOTAL ") {
		t.Fatal(frame)
	}
	vm.Coding[0].MonthCodeUsed = ""
	if frame := ui.RenderString(vm); strings.Contains(frame, "MONTH") {
		t.Fatal(frame)
	}
}

func TestFourCodingCardsWithMonthlyUsageFitBothGrids(t *testing.T) {
	for _, rotation := range []bool{false, true} {
		vm := baseModel()
		vm.RotationEnabled = rotation
		vm.Coding = []ui.Card{
			{Name: "Codex", PercentLeft: pct(80), Status: protocol.DisplayOK},
			{Name: "MiniMax", PercentLeft: pct(70), Status: protocol.DisplayOK},
			{Name: "Kimi Coding", PercentLeft: pct(100), MonthTotalUsed: "90.5%", MonthCodeUsed: "90.3%", Status: protocol.DisplayCrit},
			{Name: "Grok", PercentLeft: pct(50), WeeklyOnly: true, Status: protocol.DisplayStale},
		}
		for _, style := range []ui.Style{ui.StyleASCII, ui.StyleRich} {
			out := ui.RenderStyledString(vm, style)
			plain := stripSGR(out)
			assertPhase3Grid(t, out, style == ui.StyleRich)
			lines := strings.Split(plain, "\n")
			if !strings.Contains(lines[8], "Kimi Coding") || !strings.Contains(lines[9], "MONTH USED: TOTAL 90.5% | CODE 90.3%") ||
				!strings.Contains(lines[10], "5H 100% LEFT") || !strings.Contains(lines[10], "CRIT") ||
				!strings.Contains(lines[11], "Grok") || !strings.Contains(lines[12], "5H --") ||
				!strings.Contains(lines[12], "STALE") {
				t.Fatal(plain)
			}
			if style == ui.StyleRich && !strings.Contains(lines[11], "████████░░░░░░░░") {
				t.Fatalf("Grok bar style shifted by monthly line:\n%s", plain)
			}
		}
	}
}

func TestMonthlyUsagePreservesFollowingAPIBalanceStyle(t *testing.T) {
	vm := baseModel()
	vm.Coding = []ui.Card{{Name: "Kimi Coding", PercentLeft: pct(100), MonthTotalUsed: "90.5%", Status: protocol.DisplayCrit}}
	out := ui.RenderStyledString(vm, ui.StyleRich)
	lines := strings.Split(out, "\n")
	// Three quota rows, one spacer, then the API title and balances.
	if !strings.Contains(lines[8], "\x1b[1;36mAPI BALANCE") ||
		!strings.Contains(lines[9], "\x1b[1;36mDeepSeek") {
		t.Fatal(out)
	}
	assertPhase3Grid(t, out, true)
}
