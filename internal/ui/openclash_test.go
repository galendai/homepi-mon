package ui_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/galendai/homepi-mon/internal/protocol"
	"github.com/galendai/homepi-mon/internal/ui"
)

func TestOpenClashModelSplitsNineteenNodesAcrossTwoCardSubpages(t *testing.T) {
	now := time.Unix(900, 0)
	report := &protocol.OpenClashReport{
		ObservedAt: now.Add(-5 * time.Second), StaleAfter: protocol.Duration(time.Minute),
		DelayStaleAfter: protocol.Duration(2 * time.Minute), SelectedGroup: "🚀 PROXY",
		SelectedNode: "NODE-19", TotalConnections: 46, Status: protocol.StatusOK,
	}
	for index := 19; index >= 1; index-- {
		delay := 120
		alive := true
		checkedAt := now.Add(-time.Duration(index) * time.Second)
		report.Nodes = append(report.Nodes, protocol.OpenClashNode{
			Name: fmt.Sprintf("NODE-%02d", index), Alive: &alive, DelayMS: &delay,
			CheckedAt: &checkedAt, ActiveConnections: index,
		})
	}
	vm := ui.ViewModel{
		Page: string(ui.PageOpenClash), HasSnapshot: true, OpenClash: report,
		Now: now, Link: protocol.LinkLive, PageNumber: 4, PageCount: 4,
		RotationEnabled: true, DwellSeconds: 15,
	}
	model, err := ui.BuildOpenClashModel(vm)
	if err != nil {
		t.Fatal(err)
	}
	if len(model.Rows) != ui.OpenClashCardLimit || model.Rows[0].Name != "NODE-01" ||
		model.Rows[9].Name != "NODE-10" {
		t.Fatalf("first sub-page cards = %+v", model.Rows)
	}
	for _, row := range model.Rows {
		if row.Delay != "120ms" || row.Selected {
			t.Fatalf("first sub-page card = %+v", row)
		}
	}
	if model.OverflowText != "" || model.SubpageText != "OPENCLASH 1/2" ||
		model.PageText != "4/4 AUTO 15S" {
		t.Fatalf("first sub-page chrome = %+v", model)
	}

	vm.Now = now.Add(15 * time.Second)
	model, err = ui.BuildOpenClashModel(vm)
	if err != nil {
		t.Fatal(err)
	}
	if len(model.Rows) != 9 || model.Rows[0].Name != "NODE-11" || model.Rows[8].Name != "NODE-19" {
		t.Fatalf("second sub-page cards = %+v", model.Rows)
	}
	exit := model.Rows[8]
	if exit.Name != "NODE-19" || !exit.Selected || exit.Delay != "120ms" {
		t.Fatalf("current exit card = %+v", exit)
	}
	if model.SubpageText != "OPENCLASH 2/2" {
		t.Fatalf("second sub-page subpage = %q", model.SubpageText)
	}
}

func TestOpenClashModelRotatesLongNodeListsThroughSubpages(t *testing.T) {
	report := &protocol.OpenClashReport{ObservedAt: time.Unix(0, 0), Status: protocol.StatusOK}
	for index := 1; index <= 25; index++ {
		report.Nodes = append(report.Nodes, protocol.OpenClashNode{Name: fmt.Sprintf("NODE-%02d", index)})
	}
	// DwellSeconds 0 is out of range and must clamp to the 15s card cycle.
	vm := ui.ViewModel{Page: string(ui.PageOpenClash), HasSnapshot: true, OpenClash: report}
	want := []struct {
		now            time.Time
		first, last    string
		count          int
		subpageSummary string
	}{
		{time.Unix(0, 0), "NODE-01", "NODE-10", 10, "OPENCLASH 1/3"},
		{time.Unix(15, 0), "NODE-11", "NODE-20", 10, "OPENCLASH 2/3"},
		{time.Unix(30, 0), "NODE-21", "NODE-25", 5, "OPENCLASH 3/3"},
		{time.Unix(45, 0), "NODE-01", "NODE-10", 10, "OPENCLASH 1/3"},
	}
	for _, step := range want {
		vm.Now = step.now
		model, err := ui.BuildOpenClashModel(vm)
		if err != nil {
			t.Fatal(err)
		}
		if len(model.Rows) != step.count || model.Rows[0].Name != step.first ||
			model.Rows[step.count-1].Name != step.last {
			t.Fatalf("sub-page at %s = %+v", step.now, model.Rows)
		}
		if model.SubpageText != step.subpageSummary || model.OverflowText != "" {
			t.Fatalf("sub-page at %s subpage = %q overflow = %q",
				step.now, model.SubpageText, model.OverflowText)
		}
	}
}

func TestOpenClashModelEmptyReportKeepsSummaryShape(t *testing.T) {
	model, err := ui.BuildOpenClashModel(ui.ViewModel{
		Page: string(ui.PageOpenClash), HasSnapshot: true, Now: time.Unix(0, 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	if model.Empty != "NO OPENCLASH DATA" || model.SubpageText != "OPENCLASH 1/1" || len(model.Rows) != 0 {
		t.Fatalf("empty model = %+v", model)
	}
}

func TestOpenClashDelayStatesNeverInventZero(t *testing.T) {
	now := time.Unix(1000, 0)
	checkedAt := now.Add(-time.Second)
	oldCheck := now.Add(-2 * time.Minute)
	report := &protocol.OpenClashReport{
		ObservedAt: now, DelayStaleAfter: protocol.Duration(time.Minute), Status: protocol.StatusOK,
		Nodes: []protocol.OpenClashNode{
			{Name: "FAST", Alive: new(true), DelayMS: new(299), CheckedAt: &checkedAt},
			{Name: "MEDIUM", Alive: new(true), DelayMS: new(300), CheckedAt: &checkedAt},
			{Name: "SLOW", Alive: new(true), DelayMS: new(801), CheckedAt: &checkedAt},
			{Name: "FAILED", Alive: new(false), CheckedAt: &checkedAt},
			{Name: "PENDING", Alive: new(true), CheckedAt: &checkedAt},
			{Name: "UNKNOWN", DelayMS: new(0), CheckedAt: &checkedAt},
			{Name: "OLD", Alive: new(true), DelayMS: new(20), CheckedAt: &oldCheck},
		},
	}
	model, err := ui.BuildOpenClashModel(ui.ViewModel{HasSnapshot: true, OpenClash: report, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]struct {
		delay string
		tone  protocol.DisplayStatus
	}{
		"FAST": {"299ms", protocol.DisplayOK}, "MEDIUM": {"300ms", protocol.DisplayWarn},
		"SLOW": {"801ms", protocol.DisplayCrit}, "FAILED": {"FAIL", protocol.DisplayCrit},
		"PENDING": {"--", protocol.DisplayNA}, "UNKNOWN": {"--", protocol.DisplayNA},
		"OLD": {"STALE", protocol.DisplayNA},
	}
	for _, row := range model.Rows {
		expected := want[row.Name]
		if row.Delay != expected.delay || row.Tone != expected.tone {
			t.Errorf("%s delay/tone = %s/%s, want %s/%s",
				row.Name, row.Delay, row.Tone, expected.delay, expected.tone)
		}
		if row.Delay == "0ms" {
			t.Errorf("%s fabricated a zero measurement", row.Name)
		}
	}

	report.Status = protocol.StatusStale
	model, err = ui.BuildOpenClashModel(ui.ViewModel{HasSnapshot: true, OpenClash: report, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(model.FooterStatus, "STALE | ") {
		t.Fatalf("stale footer = %q", model.FooterStatus)
	}
	for _, row := range model.Rows {
		expected := want[row.Name]
		if row.Delay != expected.delay || row.Tone != expected.tone {
			t.Errorf("stale %s delay/tone = %s/%s, want %s/%s",
				row.Name, row.Delay, row.Tone, expected.delay, expected.tone)
		}
	}
}

func TestOpenClashAuthAndRateLimitStates(t *testing.T) {
	now := time.Unix(1000, 0)
	checkedAt := now.Add(-time.Second)
	base := &protocol.OpenClashReport{
		ObservedAt: now, StaleAfter: protocol.Duration(time.Minute),
		DelayStaleAfter: protocol.Duration(time.Minute), Status: protocol.StatusOK,
		Nodes: []protocol.OpenClashNode{{
			Name: "FAST", Alive: new(true), DelayMS: new(299), CheckedAt: &checkedAt,
		}},
	}

	auth := *base
	auth.ErrorClass = protocol.ErrAuth
	model, err := ui.BuildOpenClashModel(ui.ViewModel{HasSnapshot: true, OpenClash: &auth, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(model.FooterStatus, "AUTH | ") || model.Rows[0].Delay != "AUTH" {
		t.Fatalf("auth model = %+v", model.Rows[0])
	}

	rateLimited := *base
	rateLimited.ErrorClass = protocol.ErrRateLimited
	model, err = ui.BuildOpenClashModel(ui.ViewModel{HasSnapshot: true, OpenClash: &rateLimited, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(model.FooterStatus, "DELAY | ") || model.Rows[0].Delay != "299ms" {
		t.Fatalf("rate-limited model = %+v", model.Rows[0])
	}

	failed := *base
	failed.ErrorClass = protocol.ErrUpstream
	failed.Nodes = nil
	model, err = ui.BuildOpenClashModel(ui.ViewModel{HasSnapshot: true, OpenClash: &failed, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if model.Empty != "OPENCLASH ERROR" || !strings.HasPrefix(model.FooterStatus, "ERROR | ") ||
		model.SubpageText != "OPENCLASH 1/1" {
		t.Fatalf("upstream error model = %+v", model)
	}
}

func TestOpenClashUnicodeAndWidthBudgets(t *testing.T) {
	now := time.Unix(1000, 0)
	checkedAt := now.Add(-time.Second)
	report := &protocol.OpenClashReport{
		ObservedAt: now, DelayStaleAfter: protocol.Duration(time.Minute),
		SelectedGroup: "🚀 PROXY", SelectedNode: "香港 01", Status: protocol.StatusOK,
		Nodes: []protocol.OpenClashNode{
			{Name: "香港 01", Alive: new(true), DelayMS: new(120000), CheckedAt: &checkedAt, ActiveConnections: 10000},
			{Name: strings.Repeat("界", 40)},
		},
	}
	model, err := ui.BuildOpenClashModel(ui.ViewModel{HasSnapshot: true, OpenClash: report, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if model.SubpageText != "OPENCLASH 1/1" {
		t.Fatalf("subpage = %q", model.SubpageText)
	}
	var hongKong, long ui.OpenClashRow
	for _, row := range model.Rows {
		switch {
		case strings.HasPrefix(row.Name, "香港"):
			hongKong = row
		case strings.HasPrefix(row.Name, "界"):
			long = row
		}
	}
	if hongKong.Name != "香港 01" || hongKong.LegacyName != "01" || !hongKong.Selected || hongKong.Delay != "9999+" {
		t.Fatalf("CJK/overflow card = %+v", hongKong)
	}
	if len([]rune(long.Name)) != 7 || !strings.HasSuffix(long.Name, "~") ||
		long.LegacyName != "ROW-01" || long.Delay != "--" {
		t.Fatalf("long CJK card = %+v", long)
	}
}

func TestOpenClashLegacyCardsFillHalfWidthColumns(t *testing.T) {
	now := time.Unix(1015, 0)
	report := &protocol.OpenClashReport{ObservedAt: now, SelectedNode: "NODE-11", Status: protocol.StatusOK}
	for index := 1; index <= 11; index++ {
		report.Nodes = append(report.Nodes, protocol.OpenClashNode{Name: fmt.Sprintf("NODE-%02d", index)})
	}
	vm := ui.ViewModel{
		Page: string(ui.PageOpenClash), HasSnapshot: true, OpenClash: report,
		Now: now, Link: protocol.LinkLive, DwellSeconds: 15,
	}
	out := ui.RenderString(vm)
	lines := strings.Split(out, "\n")
	if len(lines) != ui.Rows {
		t.Fatalf("legacy rows = %d, want %d", len(lines), ui.Rows)
	}
	subpage := lines[3]
	if !strings.Contains(subpage, "OPENCLASH 2/2") || strings.Contains(subpage, "NODE-11") {
		t.Fatalf("unexpected legacy sub-page title = %q", subpage)
	}
	var card string
	for _, line := range lines {
		if strings.Contains(line, "NODE-11") {
			card = line
		}
	}
	if card == "" {
		t.Fatal("second sub-page card missing from legacy grid")
	}
	// 11 nodes: the second sub-page holds the selected card in the left
	// 29-cell half and leaves the right half blank.
	left := fmt.Sprintf("*%-22s %5s", "NODE-11", "--")
	if want := "|" + left + strings.Repeat(" ", 29) + "|"; card != want {
		t.Fatalf("selected half-width card line = %q, want %q", card, want)
	}

	vm.Now = now.Add(-15 * time.Second)
	lines = strings.Split(ui.RenderString(vm), "\n")
	if !strings.Contains(strings.Join(lines, "\n"), "OPENCLASH 1/2") || strings.Contains(strings.Join(lines, "\n"), "*") {
		t.Fatal("first legacy sub-page lost its title or showed an off-page active marker")
	}
	var pair int
	for _, line := range lines {
		if len(line) != ui.Cols {
			t.Fatalf("legacy line has %d cells: %q", len(line), line)
		}
		if strings.Contains(line, "NODE-01") {
			if !strings.Contains(line, "NODE-06") {
				t.Fatalf("first sub-page columns not paired: %q", line)
			}
			pair++
		}
	}
	if pair != 1 {
		t.Fatalf("first sub-page card rows = %d, want one paired row", pair)
	}
}

func TestOpenClashBuildAndLegacyLayoutsCoverEmptyStaleAndUnicode(t *testing.T) {
	now := time.Unix(1000, 0)
	report := &protocol.OpenClashReport{ObservedAt: now, SelectedNode: "EXIT", Status: protocol.StatusOK}
	snap := &protocol.MetricSnapshot{GeneratedAt: now, OpenClash: report}
	vm := ui.Build(snap, ui.BuildOptions{Page: string(ui.PageOpenClash), Now: now, Connected: true})
	if vm.OpenClash != report {
		t.Fatal("snapshot OpenClash report did not reach the view model")
	}

	longName := strings.Repeat("界", 40) + "\x1b[2J\nTAIL"
	report.SelectedGroup, report.SelectedNode = strings.Repeat("组", 30), longName
	for index := range 24 {
		report.Nodes = append(report.Nodes, protocol.OpenClashNode{Name: fmt.Sprintf("NODE-%02d", index)})
	}
	report.Nodes = append(report.Nodes, protocol.OpenClashNode{Name: longName})
	out := ui.RenderStyledString(vm, ui.StyleASCII)
	assertOpenClashLegacyGrid(t, out)
	if strings.Contains(out, "TEST") || strings.Contains(out, " MORE") || strings.Contains(out, "\x1b") {
		t.Fatalf("legacy subtitle or sanitisation missing:\n%s", out)
	}

	empty := ui.RenderStyledString(ui.ViewModel{
		Page: string(ui.PageOpenClash), HasSnapshot: true, Now: now,
	}, ui.StyleASCII)
	if !strings.Contains(empty, "NO OPENCLASH DATA") {
		t.Fatalf("empty page =\n%s", empty)
	}
	assertOpenClashLegacyGrid(t, empty)

	staleReport := &protocol.OpenClashReport{
		ObservedAt: now.Add(-time.Hour), StaleAfter: protocol.Duration(time.Minute), Status: protocol.StatusStale,
	}
	stale := ui.RenderStyledString(ui.ViewModel{
		Page: string(ui.PageOpenClash), HasSnapshot: true, OpenClash: staleReport, Now: now,
	}, ui.StyleASCII)
	if !strings.Contains(stale, "OPENCLASH STALE") {
		t.Fatalf("stale page =\n%s", stale)
	}
	assertOpenClashLegacyGrid(t, stale)
}

func assertOpenClashLegacyGrid(t *testing.T, frame string) {
	t.Helper()
	lines := strings.Split(frame, "\n")
	if len(lines) != ui.Rows {
		t.Fatalf("legacy rows = %d, want %d", len(lines), ui.Rows)
	}
	for _, line := range lines {
		if len(line) != ui.Cols {
			t.Fatalf("legacy line has %d cells: %q", len(line), line)
		}
		for _, char := range []byte(line) {
			if char < 0x20 || char > 0x7e {
				t.Fatalf("non-ASCII byte %#x in %q", char, line)
			}
		}
	}
}
