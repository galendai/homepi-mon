package ui_test

import (
	"strings"
	"testing"
	"time"

	"github.com/galendai/homepi-mon/internal/protocol"
	"github.com/galendai/homepi-mon/internal/ui"
)

func TestTokenPlanModelPreservesQuotaMeaning(t *testing.T) {
	vm := baseModel()
	vm.HasSnapshot = true
	vm.RotationEnabled = true
	vm.PageNumber, vm.PageCount, vm.DwellSeconds = 2, 6, 30
	age := 65 * time.Second
	vm.SyncedAgo = &age
	vm.Coding = append(vm.Coding, ui.Card{
		Name: "Grok", PercentLeft: pct(42), WeeklyOnly: true, Estimated: true,
		ResetLabel: "RESET 4D", Status: protocol.DisplayOK,
	})

	model, err := ui.BuildTokenPlanModel(vm)
	if err != nil {
		t.Fatal(err)
	}
	if len(model.Plans) != 4 {
		t.Fatalf("plans=%d", len(model.Plans))
	}
	miniMax := model.Plans[1]
	if miniMax.Name != "MiniMax" || miniMax.PrimaryLabel != "5H LEFT" || miniMax.PrimaryValue != "68%" ||
		miniMax.FiveHour != "68%" || miniMax.Week != "--" || miniMax.Month != "--" || miniMax.Reset != "2H" ||
		miniMax.PrimaryStatus != protocol.DisplayOK || miniMax.FiveHourStatus != protocol.DisplayOK ||
		miniMax.WeekStatus != protocol.DisplayNA {
		t.Fatalf("MiniMax quota changed meaning: %+v", miniMax)
	}
	weekly := model.Plans[3]
	if weekly.Name != "Grok EST" || weekly.PrimaryLabel != "WEEK LEFT" || weekly.PrimaryValue != "42%" ||
		weekly.PrimaryStatus != protocol.DisplayWarn || weekly.FiveHour != "--" || weekly.Week != "42%" ||
		weekly.FiveHourStatus != protocol.DisplayNA || weekly.WeekStatus != protocol.DisplayWarn {
		t.Fatalf("weekly-only quota changed windows: %+v", weekly)
	}
	if model.TitleText != "TOKEN PLAN" || model.FooterStatus != "1 ALERTS | SNAPSHOT 1M AGO" || model.PageText != "2/6 AUTO 30S" {
		t.Fatalf("unexpected page status: %+v", model)
	}
}

func TestTokenPlanModelUsesMonthlyAsLastPrimaryFallback(t *testing.T) {
	vm := baseModel()
	vm.HasSnapshot = true
	vm.Coding = []ui.Card{{Name: "Monthly", MonthPercentLeft: pct(73), Status: protocol.DisplayOK}}
	model, err := ui.BuildTokenPlanModel(vm)
	if err != nil {
		t.Fatal(err)
	}
	if len(model.Plans) != 1 || model.Plans[0].PrimaryLabel != "MONTH LEFT" ||
		model.Plans[0].PrimaryValue != "73%" || model.Plans[0].PrimaryStatus != protocol.DisplayOK ||
		model.Plans[0].MonthStatus != protocol.DisplayOK ||
		model.Plans[0].FiveHour != "--" || model.Plans[0].Week != "--" {
		t.Fatalf("monthly-only primary = %+v", model.Plans)
	}
}

func TestTokenPlanModelSkipsNegativeWindowsWhenChoosingPrimary(t *testing.T) {
	negative, weekly, monthly := -1, 42, 73
	vm := baseModel()
	vm.HasSnapshot = true
	vm.Coding = []ui.Card{{
		Name: "Mixed", PercentLeft: &negative, WeekPercentLeft: &weekly, Status: protocol.DisplayOK,
	}}
	model, err := ui.BuildTokenPlanModel(vm)
	if err != nil {
		t.Fatal(err)
	}
	if model.Plans[0].PrimaryLabel != "WEEK LEFT" || model.Plans[0].PrimaryValue != "42%" ||
		model.Plans[0].FiveHour != "--" || model.Plans[0].FiveHourStatus != protocol.DisplayNA {
		t.Fatalf("negative 5-hour hid the weekly value: %+v", model.Plans)
	}
	vm.Coding = []ui.Card{{
		Name: "Monthly", WeeklyOnly: true, PercentLeft: &negative, MonthPercentLeft: &monthly,
		Status: protocol.DisplayOK,
	}}
	model, err = ui.BuildTokenPlanModel(vm)
	if err != nil {
		t.Fatal(err)
	}
	if model.Plans[0].PrimaryLabel != "MONTH LEFT" || model.Plans[0].PrimaryValue != "73%" ||
		model.Plans[0].PrimaryStatus != protocol.DisplayOK {
		t.Fatalf("negative weekly hid the monthly value: %+v", model.Plans)
	}
	vm.Coding = []ui.Card{{
		Name: "Unavailable", PercentLeft: &negative, WeekPercentLeft: &negative, MonthPercentLeft: &negative,
		Status: protocol.DisplayOK,
	}}
	model, err = ui.BuildTokenPlanModel(vm)
	if err != nil {
		t.Fatal(err)
	}
	if model.Plans[0].PrimaryLabel != "5H LEFT" || model.Plans[0].PrimaryValue != "--" ||
		model.Plans[0].PrimaryStatus != protocol.DisplayNA {
		t.Fatalf("fully unavailable primary was fabricated: %+v", model.Plans)
	}
}
func TestTokenPlanModelRejectsContentThatNeedsLegacyLayout(t *testing.T) {
	tests := map[string]func(*ui.ViewModel){
		"no snapshot": func(vm *ui.ViewModel) {
			vm.HasSnapshot = false
			vm.Coding, vm.API = nil, nil
		},
		"plan action": func(vm *ui.ViewModel) {
			vm.Coding[0].ActionHint = "re-authenticate on DEV-MAC"
		},
		"wide five-hour quota": func(vm *ui.ViewModel) {
			vm.Coding[0].PercentLeft = pct(100000)
		},
		"wide monthly quota": func(vm *ui.ViewModel) {
			vm.Coding[0].MonthPercentLeft = pct(1000000)
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			vm := baseModel()
			vm.HasSnapshot = true
			mutate(&vm)
			if _, err := ui.BuildTokenPlanModel(vm); err == nil {
				t.Fatal("legacy-only content was accepted")
			}
		})
	}

	vm := emptyModel()
	vm.HasSnapshot = true
	model, err := ui.BuildTokenPlanModel(vm)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(model.PlanEmpty, "NO CODING DATA") {
		t.Fatalf("empty data was not explicit: %+v", model)
	}
}

func TestTokenPlanModelIgnoresAPIOnlyFallbacks(t *testing.T) {
	vm := baseModel()
	vm.HasSnapshot = true
	vm.API[0].ActionHint = "update API key on DEV-MAC"
	vm.API[0].Amount = "CNY 12345678901234567890"
	if _, err := ui.BuildTokenPlanModel(vm); err != nil {
		t.Fatalf("API-only content blocked Token Plan: %v", err)
	}
}

func TestTokenPlanTitleDoesNotUseNodeLabel(t *testing.T) {
	vm := baseModel()
	vm.HasSnapshot = true
	vm.NodeLabel = strings.Repeat("W", 64)
	model, err := ui.BuildTokenPlanModel(vm)
	if err != nil {
		t.Fatal(err)
	}
	if model.TitleText != "TOKEN PLAN" {
		t.Fatalf("Token Plan title = %q", model.TitleText)
	}
}
