package ui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/galendai/homepi-mon/internal/decimal"
	"github.com/galendai/homepi-mon/internal/protocol"
)

// APIUsageRow keeps the currency, value and qualifiers independently styleable.
type APIUsageRow struct {
	Name, StatusText, Currency, Value, Estimate string
	Status                                      protocol.DisplayStatus
	LowBalance                                  bool
}

// AmountText matches the one-cell gaps between visible parts of the native row.
func (r APIUsageRow) AmountText() string {
	var parts []string
	for _, part := range [...]string{r.StatusText, r.Currency, r.Value, r.Estimate} {
		if part != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, " ")
}

// APIUsageModel contains secret-free values for the native API Balance page.
type APIUsageModel struct {
	cardPageChrome
	Empty string
	Cards []APIUsageRow
}

// BuildAPIUsageModel derives API cards without consulting subscription quotas.
func BuildAPIUsageModel(vm ViewModel) (APIUsageModel, error) {
	chrome, err := buildCardPageChrome(vm, "API BALANCE")
	if err != nil {
		return APIUsageModel{}, err
	}
	model := APIUsageModel{
		cardPageChrome: chrome,
		Cards:          make([]APIUsageRow, 0, len(vm.API)),
	}
	for _, card := range vm.API {
		if card.ActionHint != "" {
			return APIUsageModel{}, fmt.Errorf("API balance: %s requires an action row", card.Name)
		}
		name := card.Name
		if card.Provider != "" {
			name = card.Provider
			if word, _, _ := strings.Cut(card.Name, " "); strings.EqualFold(word, name) {
				name = word
			}
		}
		row := apiBalanceRow(card)
		row.Name = truncName(name, 16)
		// Two 8-pixel text columns share 196 pixels, including an 8-pixel gap.
		if len(row.Name)+utf8.RuneCountInString(row.AmountText()) > 23 {
			return APIUsageModel{}, fmt.Errorf("API balance: %s exceeds the single-line card", row.Name)
		}
		model.Cards = append(model.Cards, row)
	}
	if len(model.Cards) == 0 {
		model.Empty = "NO API DATA"
	}
	return model, nil
}

func apiBalanceRow(card Card) APIUsageRow {
	row := APIUsageRow{Status: card.Status}
	if card.Status != "" && card.Status != protocol.DisplayOK {
		row.StatusText = string(card.Status)
	}
	if card.Status == protocol.DisplayAuth || card.Amount == "" {
		if row.StatusText == "" {
			row.StatusText = "--"
		}
		return row
	}
	unit, number, hasUnit := strings.Cut(card.Amount, " ")
	if !hasUnit {
		unit, number = "", unit
	}
	row.Value = sanitize(number)
	switch unit {
	case "CNY", "¥":
		row.Currency = "¥"
		amount, err := decimal.Parse(number)
		row.LowBalance = err == nil && amount.Cmp(decimal.New(20, 0)) < 0
	case "USD":
		row.Currency = "$"
	default:
		row.Currency = sanitize(unit)
	}
	if card.Estimated {
		row.Estimate = "EST"
	}
	return row
}
