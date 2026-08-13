package ui_test

import (
	"testing"
	"time"

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

func TestRouterRotatesPreemptsAndRestoresRemainingDwell(t *testing.T) {
	config, err := ui.ParseRotationConfig(ui.DefaultPageOrderText,
		"CODING:10,API:10,HOMELAB:10,SERVICES:10,SYSTEM:10")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Unix(0, 0)
	router := ui.NewRouter(config, start)
	if page := router.Update(start, nil); page != ui.PageCoding {
		t.Fatal(page)
	}
	if page := router.Update(start.Add(10*time.Second), nil); page != ui.PageAPI {
		t.Fatal(page)
	}
	if page := router.Update(start.Add(13*time.Second), map[ui.Page]bool{ui.PageHomeLab: true}); page != ui.PageHomeLab {
		t.Fatal(page)
	}
	if page := router.Update(start.Add(30*time.Second), map[ui.Page]bool{ui.PageHomeLab: true}); page != ui.PageHomeLab {
		t.Fatal(page)
	}
	if page := router.Update(start.Add(31*time.Second), nil); page != ui.PageAPI {
		t.Fatalf("restored %s", page)
	}
	if page := router.Update(start.Add(37*time.Second), nil); page != ui.PageAPI {
		t.Fatalf("remaining dwell lost: %s", page)
	}
	if page := router.Update(start.Add(38*time.Second), nil); page != ui.PageHomeLab {
		t.Fatalf("did not resume after 7s: %s", page)
	}
}

func TestRouterChoosesFirstCriticalInConfiguredOrder(t *testing.T) {
	config, _ := ui.ParseRotationConfig("SYSTEM,SERVICES,HOMELAB,API,CODING", ui.DefaultPageDwellText)
	router := ui.NewRouter(config, time.Unix(0, 0))
	page := router.Update(time.Unix(1, 0), map[ui.Page]bool{ui.PageHomeLab: true, ui.PageServices: true})
	if page != ui.PageServices {
		t.Fatalf("page = %s", page)
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
	if page := router.Update(start.Add(121*time.Second), nil); page != ui.PageHomeLab {
		t.Fatalf("page after 121s = %s, want HOMELAB", page)
	}
}

func TestRouterRemoteOverrideCriticalPriorityAndRemainingDwell(t *testing.T) {
	config, err := ui.ParseRotationConfig(ui.DefaultPageOrderText,
		"CODING:10,API:10,HOMELAB:10,SERVICES:10,SYSTEM:10")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Unix(0, 0)
	router := ui.NewRouter(config, start)
	router.ShowPage(ui.PageAPI, 10*time.Second, start.Add(3*time.Second))
	if page := router.Update(start.Add(4*time.Second), nil); page != ui.PageAPI {
		t.Fatalf("remote page=%s", page)
	}
	if page := router.Update(start.Add(5*time.Second), map[ui.Page]bool{ui.PageHomeLab: true}); page != ui.PageHomeLab {
		t.Fatalf("critical page=%s", page)
	}
	if position, _ := router.Position(); position != 3 || !router.CriticalActive() {
		t.Fatalf("critical position=%d active=%v", position, router.CriticalActive())
	}
	if page := router.Update(start.Add(7*time.Second), nil); page != ui.PageAPI {
		t.Fatalf("remote page after critical=%s", page)
	}
	if router.CriticalActive() {
		t.Fatal("critical state remained active")
	}
	if page := router.Update(start.Add(13*time.Second), nil); page != ui.PageCoding {
		t.Fatalf("restored page=%s", page)
	}
	if page := router.Update(start.Add(20*time.Second), nil); page != ui.PageAPI {
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
	if page := router.Update(start.Add(9*time.Second), nil); page != ui.PageCoding {
		t.Fatalf("disabled page=%s", page)
	}
	if page := router.Update(start.Add(10*time.Second), nil); page != ui.PageCoding || !router.RotationEnabled() {
		t.Fatalf("expired page=%s enabled=%v", page, router.RotationEnabled())
	}
	if page := router.Update(start.Add(25*time.Second), nil); page != ui.PageAPI {
		t.Fatalf("restored rotation page=%s", page)
	}
}
