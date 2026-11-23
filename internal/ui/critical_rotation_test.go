package ui_test

import (
	"strings"
	"testing"
	"time"

	"github.com/galendai/homepi-mon/internal/protocol"
	"github.com/galendai/homepi-mon/internal/ui"
)

func TestRouterCriticalMonthlyQuotaKeepsFivePageRotationAndAlerts(t *testing.T) {
	config, err := ui.ParseRotationConfig(ui.DefaultPageOrderText,
		"CODING:30,API:30,HOMELAB:10,SERVICES:10,SYSTEM:5")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 10, 10, 8, 0, 0, 0, time.UTC)
	router := ui.NewRouter(config, start)
	steps := []struct {
		seconds  int
		page     ui.Page
		position int
	}{
		{0, ui.PageCoding, 1}, {29, ui.PageCoding, 1},
		{30, ui.PageAPI, 2}, {59, ui.PageAPI, 2},
		{60, ui.PageHomeLab, 3}, {69, ui.PageHomeLab, 3},
		{70, ui.PageServices, 4}, {79, ui.PageServices, 4},
		{80, ui.PageSystem, 5}, {84, ui.PageSystem, 5},
		{85, ui.PageCoding, 1}, {115, ui.PageAPI, 2},
	}
	for _, step := range steps {
		now := start.Add(time.Duration(step.seconds) * time.Second)
		snap := &protocol.MetricSnapshot{
			SourceNode: "test-mac", GeneratedAt: now, Metrics: monthlyMetrics(now),
		}
		critical := ui.CriticalPages(snap, now, protocol.DefaultThresholds())
		if !critical[ui.PageCoding] {
			t.Fatal("fixture must retain its critical monthly quota")
		}
		page := router.Update(now)
		if page != step.page {
			t.Fatalf("at %ds page=%s, want %s despite CRIT", step.seconds, page, step.page)
		}
		position, count := router.Position()
		if position != step.position || count != 5 || !router.RotationEnabled() {
			t.Fatalf("at %ds position=%d/%d enabled=%v", step.seconds, position, count, router.RotationEnabled())
		}
		vm := ui.Build(snap, ui.BuildOptions{
			Page: string(page), Now: now, Connected: true, Thresholds: protocol.DefaultThresholds(),
			RotationEnabled: router.RotationEnabled(), PageNumber: position, PageCount: count,
			DwellSeconds: int(router.Dwell() / time.Second),
		})
		if vm.Link != protocol.LinkCrit || vm.AlertCount != 1 || vm.Coding[0].Status != protocol.DisplayCrit {
			t.Fatalf("at %ds critical status or alert count lost: %+v", step.seconds, vm)
		}
		for _, style := range []ui.Style{ui.StyleASCII, ui.StyleRich} {
			frame := ui.RenderKioskString(vm, style)
			assertPhase3Grid(t, frame, style == ui.StyleRich)
			plain := stripSGR(frame)
			if !strings.Contains(plain, "CRIT") ||
				(!strings.Contains(plain, "1 ALERT") && !strings.Contains(plain, "1 WARNING")) {
				t.Fatalf("at %ds %s frame lost critical indicators:\n%s", step.seconds, style, frame)
			}
		}
	}
}

func TestRouterMultipleCriticalPagesKeepConfiguredRotationOrder(t *testing.T) {
	config, err := ui.ParseRotationConfig("SYSTEM,SERVICES,HOMELAB,API,CODING", ui.DefaultPageDwellText)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 10, 10, 8, 0, 0, 0, time.UTC)
	router := ui.NewRouter(config, start)
	want := []ui.Page{ui.PageCoding, ui.PageSystem, ui.PageServices, ui.PageHomeLab, ui.PageAPI, ui.PageCoding}
	for index, expected := range want {
		now := start.Add(time.Duration(index*15) * time.Second)
		snap := &protocol.MetricSnapshot{
			SourceNode: "test-mac", GeneratedAt: now, Metrics: monthlyMetrics(now),
			HomeLabNodes:    []protocol.HomeLabNode{{ID: "test-node", Name: "Test Node", ObservedAt: now, Status: protocol.StatusCritical}},
			HomeLabServices: []protocol.HomeLabService{{ID: "test-service", Name: "Test Service", Kind: "grafana", ObservedAt: now, Status: protocol.StatusCritical}},
		}
		critical := ui.CriticalPages(snap, now, protocol.DefaultThresholds())
		if !critical[ui.PageCoding] || !critical[ui.PageHomeLab] || !critical[ui.PageServices] {
			t.Fatalf("fixture critical pages=%v", critical)
		}
		if page := router.Update(now); page != expected {
			t.Fatalf("at %ds page=%s, want %s despite multiple CRIT pages", index*15, page, expected)
		}
	}
}
