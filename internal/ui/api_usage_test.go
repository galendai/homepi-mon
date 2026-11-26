package ui_test

import (
	"strings"
	"testing"

	"github.com/galendai/homepi-mon/internal/decimal"
	"github.com/galendai/homepi-mon/internal/protocol"
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
	if len(model.Cards) != 2 || model.Cards[0].AmountText() != "WARN ¥ 8.20" || model.Cards[1].AmountText() != "¥ 49.58" {
		t.Fatalf("API amounts changed meaning: %+v", model.Cards)
	}
	if model.TitleText != "API BALANCE" || model.PageText != "3/6 AUTO 30S" || model.FooterStatus != "1 ALERTS | SNAPSHOT 0S AGO" {
		t.Fatalf("unexpected API page chrome: %+v", model)
	}
}

func TestAPIBalanceUsesProviderAndPrimaryAmount(t *testing.T) {
	now := at("12:00")
	available, voucher := decimal.MustParse("19.405"), decimal.MustParse("7.50")
	main := protocol.ProviderMetric{Provider: "kimi", DisplayName: "Kimi Available", Group: ui.GroupAPI,
		MetricKind: protocol.KindBalance, Unit: "CNY", Value: &available, Order: 50, ObservedAt: now, Precision: protocol.PrecisionExact}
	other := main
	other.DisplayName, other.Value, other.Order = "Kimi Voucher", &voucher, 51
	vm := ui.Build(&protocol.MetricSnapshot{GeneratedAt: now, Metrics: []protocol.ProviderMetric{other, main}},
		ui.BuildOptions{Connected: true, Now: now})
	page, err := ui.BuildAPIUsageModel(vm)
	if err != nil || len(page.Cards) != 1 || page.Cards[0].Name != "Kimi" || page.Cards[0].AmountText() != "¥ 19.41" || !page.Cards[0].LowBalance {
		t.Fatalf("primary balance = %+v, error = %v", page.Cards, err)
	}
}

func TestAPIBalanceKeepsCurrencyAndExceptionalStatesOnOneLine(t *testing.T) {
	for _, tt := range []struct {
		amount    string
		status    protocol.DisplayStatus
		estimated bool
		want      string
		low       bool
	}{
		{"CNY 19.99", protocol.DisplayOK, false, "¥ 19.99", true},
		{"CNY 20.00", protocol.DisplayOK, false, "¥ 20.00", false},
		{"CNY 20.01", protocol.DisplayOK, false, "¥ 20.01", false},
		{"CNY 0.00", protocol.DisplayOK, false, "¥ 0.00", true},
		{"¥ -1.20", protocol.DisplayOK, false, "¥ -1.20", true},
		{"USD -1.23", protocol.DisplayOK, false, "$ -1.23", false},
		{"EUR 5.90", protocol.DisplayOK, false, "EUR 5.90", false},
		{"CNY 1.50", protocol.DisplayStale, true, "STALE ¥ 1.50 EST", true},
		{"CNY 50.00", protocol.DisplayWarn, false, "WARN ¥ 50.00", false},
		{"CNY 5.00", protocol.DisplayAuth, false, "AUTH", false},
		{"", protocol.DisplayError, false, "ERROR", false},
		{"", protocol.DisplayOK, false, "--", false},
	} {
		vm := baseModel()
		vm.API = []ui.Card{{Name: "Balance", Amount: tt.amount, Status: tt.status, Estimated: tt.estimated}}
		page, err := ui.BuildAPIUsageModel(vm)
		if err != nil || len(page.Cards) != 1 || page.Cards[0].AmountText() != tt.want ||
			page.Cards[0].LowBalance != tt.low || page.Cards[0].Status != tt.status {
			t.Errorf("amount %q (%s): got %+v, err %v, want %q, low %t", tt.amount, tt.status, page.Cards, err, tt.want, tt.low)
		}
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
