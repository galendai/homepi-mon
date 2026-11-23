package ui_test

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/galendai/homepi-mon/internal/decimal"
	"github.com/galendai/homepi-mon/internal/protocol"
	"github.com/galendai/homepi-mon/internal/ui"
)

func compactModel() ui.ViewModel {
	vm := baseModel()
	vm.Coding[2].MonthTotalUsed, vm.Coding[2].MonthCodeUsed = "90.5%", "90.3%"
	vm.Coding[2].MonthPercentLeft = pct(10)
	vm.Coding = append(vm.Coding,
		ui.Card{Name: "Grok", PercentLeft: pct(42), WeeklyOnly: true, ResetFine: "RESET 4D", Status: protocol.DisplayOK},
		ui.Card{Name: "OpenCode Go", PercentLeft: pct(80), WeekPercentLeft: pct(65), MonthPercentLeft: pct(50), ResetFine: "RESET 2H14M", Status: protocol.DisplayOK},
	)
	vm.API = append(vm.API,
		ui.Card{Name: "Another API", Amount: "USD -1.24", Status: protocol.DisplayWarn},
		ui.Card{Name: "Fourth API", Amount: "CNY 123456789.12", Status: protocol.DisplayOK},
	)
	return vm
}

func TestCodexReviewDoesNotOverwriteMainWeeklyQuota(t *testing.T) {
	now := at("14:32")
	mainReset, reviewReset := now.Add(4*24*time.Hour), now.Add(24*time.Hour)
	mainValue, reviewValue, limit := decimal.MustParse("54"), decimal.MustParse("75"), decimal.MustParse("100")
	metrics := []protocol.ProviderMetric{
		{ID: "codex.weekly", Provider: "openai", DisplayName: "Codex", Group: "coding", MetricKind: protocol.KindQuota,
			Window: protocol.WindowWeekly, Value: &mainValue, Limit: &limit, Unit: "percent", Status: protocol.StatusOK,
			Precision: protocol.PrecisionVerified, ObservedAt: now, ResetsAt: &mainReset, Order: 21},
		{ID: "codex.code_review", Provider: "openai", DisplayName: "Codex Review", Group: "coding", MetricKind: protocol.KindQuota,
			Window: protocol.WindowWeekly, Value: &reviewValue, Limit: &limit, Unit: "percent", Status: protocol.StatusOK,
			Precision: protocol.PrecisionVerified, ObservedAt: now, ResetsAt: &reviewReset, Order: 22},
	}
	for _, reverse := range []bool{false, true} {
		if reverse {
			metrics[0], metrics[1] = metrics[1], metrics[0]
		}
		vm := ui.Build(&protocol.MetricSnapshot{GeneratedAt: now, Metrics: metrics}, ui.BuildOptions{Now: now, Connected: true, Page: string(ui.PageCoding)})
		if len(vm.Coding) != 2 || vm.Coding[0].Name != "Codex" || vm.Coding[1].Name != "Codex Review" {
			t.Fatalf("main and review quotas were mixed: %+v", vm.Coding)
		}
		for _, style := range []ui.Style{ui.StyleASCII, ui.StyleRich} {
			frame := ui.RenderKioskString(vm, style)
			assertPhase3Grid(t, frame, style == ui.StyleRich)
			found := 0
			for _, line := range strings.Split(stripSGR(frame), "\n") {
				cells := []rune(line)
				name := strings.TrimSpace(string(cells[2:18]))
				wantPercent, wantReset := "54%", "4D"
				if name == "Codex Review" {
					wantPercent, wantReset = "75%", "1D"
				} else if name != "Codex" {
					continue
				}
				found++
				if strings.TrimSpace(string(cells[19:24])) != "--" || strings.TrimSpace(string(cells[25:30])) != wantPercent ||
					strings.TrimSpace(string(cells[31:36])) != "--" || !strings.Contains(line, wantReset) {
					t.Fatal(line)
				}
			}
			if found != 2 {
				t.Fatal("quota rows missing")
			}
		}
	}
}

func TestCompactOverviewKeepsAllMainValuesOnOnePage(t *testing.T) {
	vm := compactModel()
	frame, err := ui.RenderCompactOverviewString(vm, ui.StyleASCII)
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"OVERVIEW", "Codex", "MiniMax", "Kimi Code", "Grok", "OpenCode Go", "2H14M", "CNY 8.20", "CNY 49.58", "USD -1.24", "CNY 123456789.12"} {
		if !strings.Contains(frame, token) {
			t.Errorf("missing %q in frame:\n%s", token, frame)
		}
	}
	for name, windows := range map[string][3]string{
		"Codex":       {"42%", "67%", "--"},
		"MiniMax":     {"68%", "--", "--"},
		"Kimi Code":   {"35%", "51%", "10%"},
		"Grok":        {"--", "42%", "--"},
		"OpenCode Go": {"80%", "65%", "50%"},
	} {
		for _, line := range strings.Split(frame, "\n") {
			if !strings.HasPrefix(line, "| "+name) {
				continue
			}
			if len(line) != ui.Cols {
				t.Fatalf("wrong row width: %q", line)
			}
			got := [3]string{strings.TrimSpace(line[19:24]), strings.TrimSpace(line[25:30]), strings.TrimSpace(line[31:36])}
			if got != windows {
				t.Errorf("%s windows = %v, want %v", name, got, windows)
			}
		}
	}
	if strings.Contains(frame, "PLANS 1/") {
		t.Fatal("compact overview still paginates the sample")
	}
	lines := strings.Split(frame, "\n")
	for i, line := range lines {
		if strings.Contains(line, "API BALANCE") && (i == 0 || strings.TrimSpace(lines[i-1][1:ui.Cols-1]) != "") {
			t.Fatal("plan and API sections need a blank line")
		}
	}
	if lines[2] != "+"+strings.Repeat("-", ui.Cols-2)+"+" || !strings.Contains(lines[3], "PLAN / LEFT") {
		t.Fatal("title bar must have a separator above the plan heading")
	}
}

func TestCompactOverviewColorsEachQuotaWindowIndependently(t *testing.T) {
	for _, tc := range []struct {
		name              string
		five, week, month *int
		colors            map[string]string
	}{
		{"mixed", pct(51), pct(50), pct(19), map[string]string{"51%": "\x1b[1;32m", "50%": "\x1b[1;33m", "19%": "\x1b[1;31m"}},
		{"bounds", pct(100), pct(20), pct(0), map[string]string{"100%": "\x1b[1;32m", "20%": "\x1b[1;33m", "0%": "\x1b[1;31m"}},
		{"missing", nil, pct(42), nil, map[string]string{"--": "\x1b[0;37m", "42%": "\x1b[1;33m"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vm := baseModel()
			vm.Coding = []ui.Card{{Name: "Quota", PercentLeft: tc.five, WeekPercentLeft: tc.week, MonthPercentLeft: tc.month, ResetFine: "RESET 1H", Status: protocol.DisplayCrit}}
			frame, err := ui.RenderCompactOverviewString(vm, ui.StyleRich)
			if err != nil {
				t.Fatal(err)
			}
			line := strings.Split(frame, "\n")[4]
			for token, want := range tc.colors {
				if got := activeSGRAt(line, token); got != want {
					t.Errorf("%s style = %q, want %q", token, got, want)
				}
			}
			if got := activeSGRAt(line, "CRIT"); got != "\x1b[1;31m" {
				t.Errorf("status color = %q", got)
			}
			assertPhase3Grid(t, frame, true)
		})
	}
}

func TestCompactOverviewKeepsMonthlyPlanOnOneRow(t *testing.T) {
	vm := compactModel()
	for _, style := range []ui.Style{ui.StyleASCII, ui.StyleRich} {
		frame, err := ui.RenderCompactOverviewString(vm, style)
		if err != nil {
			t.Fatal(err)
		}
		plain := stripSGR(frame)
		if strings.Contains(plain, "MONTH USED") || strings.Contains(plain, "90.5%") || strings.Contains(plain, "90.3%") {
			t.Fatal("compact plan rows must not include monthly usage details")
		}
		lines := strings.Split(plain, "\n")
		for index, card := range vm.Coding {
			if name := strings.TrimSpace(string([]rune(lines[index+4])[2:18])); name != card.Name {
				t.Fatalf("plan %s is not on its single consecutive row: %s", card.Name, plain)
			}
		}
		spacer := len(vm.Coding) + 4
		if strings.TrimSpace(string([]rune(lines[spacer])[1:ui.Cols-1])) != "" || !strings.Contains(lines[spacer+1], "API BALANCE") {
			t.Fatal("single plan rows must precede exactly one section spacer")
		}
		assertPhase3Grid(t, frame, style == ui.StyleRich)
	}
}

func TestCompactOverviewSpacerCountsTowardsCapacity(t *testing.T) {
	vm := kioskModel()
	vm.API = append(vm.API, ui.Card{Name: "Fifth API", Amount: "USD 1.00", Status: protocol.DisplayOK})
	for _, style := range []ui.Style{ui.StyleASCII, ui.StyleRich} {
		if _, err := ui.RenderCompactOverviewString(vm, style); err != nil {
			t.Fatalf("exactly 13 data rows should fit: %v", err)
		}
	}
	vm.API = append(vm.API, ui.Card{Name: "Sixth API", Amount: "USD 2.00", Status: protocol.DisplayOK})
	for _, style := range []ui.Style{ui.StyleASCII, ui.StyleRich} {
		if frame, err := ui.RenderCompactOverviewString(vm, style); err == nil || frame != "" {
			t.Fatalf("14 data rows should reject overflow: err=%v", err)
		}
		if frame := ui.RenderKioskString(vm, style); !strings.Contains(stripSGR(frame), "COMPACT FULL") {
			t.Fatal("overflow must fall back to labelled details")
		}
	}
}

func TestCompactOverviewPreservesStateAndGridInBothStyles(t *testing.T) {
	auth := compactModel()
	auth.Coding[0].Status, auth.Coding[0].ActionHint = protocol.DisplayAuth, "re-auth with official CLI on DEV-MAC"
	auth.Coding[0].MonthTotalUsed = "77.7%"
	auth.API[0].Status, auth.API[0].Amount = protocol.DisplayAuth, "CNY 999.99"
	auth.API[0].ActionHint = "update API key on DEV-MAC"
	// The title divider and two remedy rows leave room for three API rows.
	auth.API = auth.API[:3]
	estimated := compactModel()
	estimated.Coding[0].Estimated = true
	estimated.API[0].Estimated = true
	for name, vm := range map[string]ui.ViewModel{
		"live": compactModel(), "offline": offlineModel(), "auth": auth,
		"estimated": estimated, "empty": emptyModel(), "hostile": hostileModel(),
	} {
		t.Run(name, func(t *testing.T) {
			ascii, err := ui.RenderCompactOverviewString(vm, ui.StyleASCII)
			if err != nil {
				t.Fatal(err)
			}
			rich, err := ui.RenderCompactOverviewString(vm, ui.StyleRich)
			if err != nil {
				t.Fatal(err)
			}
			plain := strings.NewReplacer("┌", "+", "┐", "+", "└", "+", "┘", "+", "├", "+", "┤", "+", "─", "-", "│", "|").Replace(stripSGR(rich))
			if plain != ascii {
				t.Fatal("rich and ASCII do not carry the same visible content")
			}
			lines := strings.Split(plain, "\n")
			if len(lines) != ui.Rows {
				t.Fatalf("got %d rows", len(lines))
			}
			for _, line := range lines {
				if utf8.RuneCountInString(line) != ui.Cols {
					t.Fatalf("wrong row width: %q", line)
				}
				for _, cell := range line {
					if cell < 0x20 || cell > 0x7e {
						t.Fatalf("untrusted control/non-ASCII survived: %q", line)
					}
				}
			}
			if strings.Contains(stripSGR(rich), "\x1b") {
				t.Fatal("non-SGR terminal control survived")
			}
			if lines[2] != "+"+strings.Repeat("-", ui.Cols-2)+"+" || !strings.Contains(lines[3], "PLAN / LEFT") {
				t.Fatal("title divider missing or displaced")
			}
			richDivider := strings.Split(rich, "\n")[2]
			if stripSGR(richDivider) != "├"+strings.Repeat("─", ui.Cols-2)+"┤" || activeSGRAt(richDivider, "├") != "\x1b[1;36m" {
				t.Fatal("rich title divider must use the cyan frame style")
			}
			switch name {
			case "auth":
				for _, hidden := range []string{"67%", "77.7%", "CNY 999.99"} {
					if strings.Contains(ascii, hidden) {
						t.Errorf("AUTH leaked cached value %q", hidden)
					}
				}
				for _, line := range lines {
					if strings.Contains(line, "Codex") && !strings.Contains(line, "   --    --    -- --") {
						t.Errorf("AUTH retains quota cells: %q", line)
					}
				}
				if !strings.Contains(ascii, "ACTION: re-auth with official CLI on DEV-MAC") || !strings.Contains(ascii, "ACTION: update API key on DEV-MAC") {
					t.Error("AUTH remedy missing")
				}
			case "offline":
				if !strings.Contains(ascii, "OFFLINE") || !strings.Contains(ascii, "STALE") || !strings.Contains(ascii, "42%") {
					t.Error("offline values or state missing")
				}
			case "estimated":
				if !strings.Contains(ascii, "Codex EST") || !strings.Contains(ascii, "CNY 8.20 EST") {
					t.Error("estimate markers missing")
				}
			case "empty":
				if !strings.Contains(ascii, "NO SNAPSHOT") || strings.Contains(ascii, "0%") {
					t.Error("empty state invents data")
				}
			}
		})
	}
}

func TestCompactOverviewRejectsOverflowAndClippedValues(t *testing.T) {
	tooMany := compactModel()
	tooMany.Coding = append(tooMany.Coding, tooMany.Coding...)
	wide := compactModel()
	wide.API[0].Amount = "CNY " + strings.Repeat("1", 30) + ".00"
	for name, vm := range map[string]ui.ViewModel{"capacity": tooMany, "amount": wide} {
		t.Run(name, func(t *testing.T) {
			frame, err := ui.RenderCompactOverviewString(vm, ui.StyleASCII)
			if err == nil || frame != "" {
				t.Fatalf("overflow was silently rendered: error=%v frame=%q", err, frame)
			}
		})
	}
}
