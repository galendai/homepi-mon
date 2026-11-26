package ui_test

import (
	"testing"
	"time"

	"github.com/galendai/homepi-mon/internal/protocol"
	"github.com/galendai/homepi-mon/internal/ui"
)

func TestRotationConfigRejectsMissingDuplicateUnknownAndOutOfRange(t *testing.T) {
	tests := []struct{ order, dwell string }{
		{"CODING,API,HOMELAB,SERVICES", ui.DefaultPageDwellText},
		{"CODING,API,HOMELAB,SERVICES,CODING", ui.DefaultPageDwellText},
		{"CODING,API,HOMELAB,SERVICES,OTHER", ui.DefaultPageDwellText},
		{ui.DefaultPageOrderText, "CODING:4,API:15,HOMELAB:15,SERVICES:15,SYSTEM:15"},
		{ui.DefaultPageOrderText, "CODING:15,API:15,HOMELAB:15,SERVICES:15,SYSTEM:301"},
	}
	for _, tc := range tests {
		if _, err := ui.ParseRotationConfig(tc.order, tc.dwell); err == nil {
			t.Errorf("ParseRotationConfig(%q, %q) succeeded", tc.order, tc.dwell)
		}
	}
}

func TestRouterAlertChangesDoNotResetDwell(t *testing.T) {
	config, err := ui.ParseRotationConfig(ui.DefaultPageOrderText,
		"CODING:10,API:10,HOMELAB:10,SERVICES:10,SYSTEM:10")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Unix(0, 0)
	router := ui.NewRouter(config, start)
	for _, step := range []struct {
		seconds  int
		critical bool
		page     ui.Page
	}{
		{0, false, ui.PageCoding}, {3, true, ui.PageCoding},
		{5, false, ui.PageCoding}, {10, true, ui.PageAPI},
		{13, false, ui.PageAPI}, {30, true, ui.PageServices},
	} {
		now := start.Add(time.Duration(step.seconds) * time.Second)
		metrics := monthlyMetrics(now)
		if !step.critical {
			metrics = metrics[:1]
		}
		snap := &protocol.MetricSnapshot{GeneratedAt: now, Metrics: metrics}
		if got := ui.CriticalPages(snap, now, protocol.DefaultThresholds())[ui.PageCoding]; got != step.critical {
			t.Fatalf("at %ds critical=%v", step.seconds, got)
		}
		if page := router.Update(now); page != step.page {
			t.Fatalf("at %ds page=%s, want %s", step.seconds, page, step.page)
		}
	}
}

func TestRouterDeterministicallyCatchesUpAfterLongClockJump(t *testing.T) {
	config, err := ui.ParseRotationConfig(ui.DefaultPageOrderText,
		"CODING:10,API:10,HOMELAB:10,SERVICES:10,SYSTEM:10")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Unix(0, 0)
	router := ui.NewRouter(config, start)
	if page := router.Update(start.Add(121 * time.Second)); page != ui.PageHomeLab {
		t.Fatalf("page after 121s = %s, want HOMELAB", page)
	}
}

func TestRouterRemoteOverridePreservesRemainingDwell(t *testing.T) {
	config, err := ui.ParseRotationConfig(ui.DefaultPageOrderText,
		"CODING:10,API:10,HOMELAB:10,SERVICES:10,SYSTEM:10")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Unix(0, 0)
	router := ui.NewRouter(config, start)
	router.ShowPage(ui.PageAPI, 10*time.Second, start.Add(3*time.Second))
	if page := router.Update(start.Add(4 * time.Second)); page != ui.PageAPI {
		t.Fatalf("remote page=%s", page)
	}
	if page := router.Update(start.Add(5 * time.Second)); page != ui.PageAPI {
		t.Fatalf("remote page=%s", page)
	}
	if position, _ := router.Position(); position != 2 {
		t.Fatalf("remote position=%d", position)
	}
	if page := router.Update(start.Add(7 * time.Second)); page != ui.PageAPI {
		t.Fatalf("remote page=%s", page)
	}
	if page := router.Update(start.Add(13 * time.Second)); page != ui.PageCoding {
		t.Fatalf("restored page=%s", page)
	}
	if page := router.Update(start.Add(20 * time.Second)); page != ui.PageAPI {
		t.Fatalf("remaining dwell was not restored: %s", page)
	}
}

func TestRouterTemporaryRotationOverrideExpires(t *testing.T) {
	start := time.Unix(0, 0)
	router := ui.NewRouter(ui.DefaultRotationConfig(), start)
	router.SetRotation(false, 0, 10*time.Second, start)
	if router.RotationEnabled() {
		t.Fatal("rotation remains enabled")
	}
	if page := router.Update(start.Add(9 * time.Second)); page != ui.PageCoding {
		t.Fatalf("disabled page=%s", page)
	}
	if page := router.Update(start.Add(10 * time.Second)); page != ui.PageCoding || !router.RotationEnabled() {
		t.Fatalf("expired page=%s enabled=%v", page, router.RotationEnabled())
	}
	if page := router.Update(start.Add(25 * time.Second)); page != ui.PageAPI {
		t.Fatalf("restored rotation page=%s", page)
	}
}

func TestOpenClashRuntimeRotationLeavesPersistentFivePagesUnchanged(t *testing.T) {
	base := ui.DefaultRotationConfig()
	legacy, err := ui.SelectRotationPages(base, "CODING,API,SYSTEM")
	if err != nil {
		t.Fatal(err)
	}
	if len(legacy.Order) != 3 || len(base.Order) != 5 {
		t.Fatalf("legacy rotation=%v persistent=%v", legacy.Order, base.Order)
	}
	selected, err := ui.SelectRotationPages(base, "HOME,CODING,API,OPENCLASH")
	if err != nil {
		t.Fatal(err)
	}
	if len(base.Order) != 5 || selected.Dwell[ui.PageOpenClash] != 15*time.Second {
		t.Fatalf("persistent=%v OpenClash dwell=%v", base.Order, selected.Dwell[ui.PageOpenClash])
	}
	start := time.Unix(0, 0)
	router := ui.NewSubsetRouter(selected, start)
	for _, step := range []struct {
		at   time.Duration
		page ui.Page
	}{
		{0, ui.PageHome}, {30 * time.Second, ui.PageCoding},
		{45 * time.Second, ui.PageAPI}, {60 * time.Second, ui.PageOpenClash},
		{75 * time.Second, ui.PageHome},
	} {
		if page := router.Update(start.Add(step.at)); page != step.page {
			t.Fatalf("page at %s = %s, want %s", step.at, page, step.page)
		}
	}
	if _, err := ui.ParseRotationConfig("CODING,API,HOMELAB,SERVICES,OPENCLASH", ui.DefaultPageDwellText); err == nil {
		t.Fatal("OpenClash entered the persistent five-page configuration")
	}
}

func TestOpenClashRemoteTemporaryAndStepPages(t *testing.T) {
	start := time.Unix(0, 0)
	legacy := ui.NewRouter(ui.DefaultRotationConfig(), start)
	legacy.ShowPage(ui.PageOpenClash, 10*time.Second, start.Add(3*time.Second))
	if page := legacy.Update(start.Add(4 * time.Second)); page != ui.PageOpenClash {
		t.Fatalf("temporary remote page = %s", page)
	}
	if page := legacy.Update(start.Add(14 * time.Second)); page != ui.PageCoding {
		t.Fatalf("expired remote page = %s", page)
	}

	selected, err := ui.SelectRotationPages(ui.DefaultRotationConfig(), "HOME,CODING,API,OPENCLASH")
	if err != nil {
		t.Fatal(err)
	}
	router := ui.NewSubsetRouter(selected, start)
	router.ShowPage(ui.PageOpenClash, 30*time.Second, start)
	router.NextPage(30*time.Second, start.Add(time.Second))
	if page := router.Update(start.Add(2 * time.Second)); page != ui.PageHome {
		t.Fatalf("next from OpenClash = %s", page)
	}
	router.PreviousPage(30*time.Second, start.Add(3*time.Second))
	if page := router.Update(start.Add(4 * time.Second)); page != ui.PageOpenClash {
		t.Fatalf("previous to OpenClash = %s", page)
	}
}
