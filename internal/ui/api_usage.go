package ui

import (
	"fmt"

	"github.com/galendai/homepi-mon/internal/protocol"
)

// APIUsageRow contains one provider's main API balance or cost value.
type APIUsageRow struct {
	Name   string
	Amount string
	Status protocol.DisplayStatus
}

// APIUsageModel contains secret-free values for the native API Usage page.
type APIUsageModel struct {
	cardPageChrome
	Empty string
	Cards []APIUsageRow
}

// BuildAPIUsageModel derives API cards without consulting subscription quotas.
func BuildAPIUsageModel(vm ViewModel) (APIUsageModel, error) {
	chrome, err := buildCardPageChrome(vm, "API USAGE")
	if err != nil {
		return APIUsageModel{}, err
	}
	model := APIUsageModel{
		cardPageChrome: chrome,
		Cards:          make([]APIUsageRow, 0, len(vm.API)),
	}
	for _, card := range vm.API {
		if card.ActionHint != "" {
			return APIUsageModel{}, fmt.Errorf("API usage: %s requires an action row", card.Name)
		}
		amount := sanitize(card.Amount)
		if amount == "" || card.Status == protocol.DisplayAuth {
			amount = "--"
		} else if card.Estimated {
			amount += " EST"
		}
		if len(amount) > cardAmountCells {
			return APIUsageModel{}, fmt.Errorf("API usage: %s amount exceeds %d cells", card.Name, cardAmountCells)
		}
		model.Cards = append(model.Cards, APIUsageRow{
			Name: truncName(card.Name, 16), Amount: amount, Status: card.Status,
		})
	}
	if len(model.Cards) == 0 {
		model.Empty = "NO API DATA"
	}
	return model, nil
}
