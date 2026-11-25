package ui_test

import (
	"strings"
	"testing"

	"github.com/galendai/homepi-mon/internal/ui"
)

func TestAPIUsageModelPreservesAmountsAndPageChrome(t *testing.T) {
	vm := baseModel()
	vm.HasSnapshot = true
	vm.RotationEnabled = true
	vm.PageNumber, vm.PageCount, vm.DwellSeconds = 3, 6, 30
	model, err := ui.BuildAPIUsageModel(vm)
	if err != nil {
		t.Fatal(err)
	}
	if len(model.Cards) != 2 || model.Cards[0].Amount != "CNY 8.20" || model.Cards[1].Amount != "CNY 49.58" {
		t.Fatalf("API amounts changed meaning: %+v", model.Cards)
	}
	if model.TitleText != "API USAGE" || model.PageText != "3/6 AUTO 30S" || model.FooterStatus != "1 ALERTS | SNAPSHOT 0S AGO" {
		t.Fatalf("unexpected API page chrome: %+v", model)
	}
}

func TestAPIUsageModelRejectsOnlyAPIFallbacks(t *testing.T) {
	tests := map[string]func(*ui.ViewModel){
		"action": func(vm *ui.ViewModel) {
			vm.API[0].ActionHint = "update API key on DEV-MAC"
		},
		"wide amount": func(vm *ui.ViewModel) {
			vm.API[0].Amount = "CNY 12345678901234567890"
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			vm := baseModel()
			vm.HasSnapshot = true
			mutate(&vm)
			if _, err := ui.BuildAPIUsageModel(vm); err == nil {
				t.Fatal("legacy-only API content was accepted")
			}
			if _, err := ui.BuildTokenPlanModel(vm); err != nil {
				t.Fatalf("API-only content blocked Token Plan: %v", err)
			}
		})
	}

	vm := emptyModel()
	vm.HasSnapshot = true
	model, err := ui.BuildAPIUsageModel(vm)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(model.Empty, "NO API DATA") {
		t.Fatalf("empty API data was not explicit: %+v", model)
	}
	vm = baseModel()
	vm.HasSnapshot = true
	vm.API = nil
	vm.Coding[0].ActionHint = "re-authenticate on DEV-MAC"
	if _, err := ui.BuildAPIUsageModel(vm); err != nil {
		t.Fatalf("subscription-only content blocked API Usage: %v", err)
	}
}
