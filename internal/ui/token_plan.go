package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/galendai/homepi-mon/internal/protocol"
)

type cardPageChrome struct {
	TitleText    string
	HeaderStatus string
	FooterStatus string
	PageText     string
}

func buildCardPageChrome(vm ViewModel, title string) (cardPageChrome, error) {
	if vm.RemoteNotice != nil {
		return cardPageChrome{}, fmt.Errorf("card page: remote notices use the legacy layout")
	}
	if !vm.hasSnapshot() {
		return cardPageChrome{}, fmt.Errorf("card page: no snapshot uses the legacy empty state")
	}
	headerStatus := sanitize(string(vm.Link))
	if headerStatus == "" {
		headerStatus = string(protocol.LinkWait)
	}
	chrome := cardPageChrome{
		TitleText:    title,
		HeaderStatus: headerStatus,
		FooterStatus: fmt.Sprintf("%d ALERTS | SNAPSHOT --", vm.AlertCount),
		PageText:     strings.TrimLeft(compactRotationLabel(vm), " |"),
	}
	if vm.SyncedAgo != nil {
		age := vm.SyncedAgo.Truncate(time.Minute)
		chrome.FooterStatus = fmt.Sprintf("%d ALERTS | SNAPSHOT %s AGO", vm.AlertCount, shortDuration(age))
	}
	if vm.Link == protocol.LinkOffline {
		chrome.FooterStatus = "OFFLINE | " + chrome.FooterStatus
	}
	if chrome.PageText == "" {
		number, count := vm.PageNumber, vm.PageCount
		if number <= 0 {
			number = 1
		}
		if count <= 0 {
			count = 1
		}
		chrome.PageText = fmt.Sprintf("%d/%d", number, count)
	}
	return chrome, nil
}

// TokenPlanRow contains display-ready quota values for the pixel Token Plan page.
type TokenPlanRow struct {
	Name           string
	PrimaryLabel   string
	PrimaryValue   string
	FiveHour       string
	Week           string
	Month          string
	Reset          string
	PrimaryStatus  protocol.DisplayStatus
	FiveHourStatus protocol.DisplayStatus
	WeekStatus     protocol.DisplayStatus
	MonthStatus    protocol.DisplayStatus
	Status         protocol.DisplayStatus
}

// TokenPlanModel contains secret-free values for the native Token Plan page.
type TokenPlanModel struct {
	cardPageChrome
	PlanEmpty string
	Plans     []TokenPlanRow
}

func BuildTokenPlanModel(vm ViewModel) (TokenPlanModel, error) {
	chrome, err := buildCardPageChrome(vm, "TOKEN PLAN")
	if err != nil {
		return TokenPlanModel{}, err
	}
	model := TokenPlanModel{
		cardPageChrome: chrome,
		Plans:          make([]TokenPlanRow, 0, len(vm.Coding)),
	}
	for _, card := range vm.Coding {
		if card.ActionHint != "" {
			return TokenPlanModel{}, fmt.Errorf("token plan: %s requires an action row", card.Name)
		}
		name := truncName(card.Name, 16)
		if card.Estimated && card.Status != protocol.DisplayAuth {
			name = truncName(card.Name, 12) + " EST"
		}
		fiveHour, week, month := card.PercentLeft, card.WeekPercentLeft, card.MonthPercentLeft
		if card.WeeklyOnly {
			fiveHour, week = nil, card.PercentLeft
		}
		reset := card.ResetFine
		if reset == "" {
			reset = card.ResetLabel
		}
		reset = strings.TrimPrefix(sanitize(reset), "RESET ")
		if reset == "" {
			reset = "--"
		}
		if card.Status == protocol.DisplayAuth {
			fiveHour, week, month, reset = nil, nil, nil, "--"
		}
		fiveHourLabel, weekLabel, monthLabel := percentLabel(fiveHour), percentLabel(week), percentLabel(month)
		if len(fiveHourLabel) > 4 || len(weekLabel) > 4 || len(monthLabel) > 4 {
			return TokenPlanModel{}, fmt.Errorf("token plan: %s quota exceeds card value fields", name)
		}
		primaryLabel, primaryValue, primaryStatus := "5H LEFT", fiveHourLabel, tokenPlanQuotaStatus(fiveHour)
		switch {
		case tokenPlanValueAvailable(fiveHour):
			primaryLabel, primaryValue, primaryStatus = "5H LEFT", fiveHourLabel, tokenPlanQuotaStatus(fiveHour)
		case tokenPlanValueAvailable(week):
			primaryLabel, primaryValue, primaryStatus = "WEEK LEFT", weekLabel, tokenPlanQuotaStatus(week)
		case tokenPlanValueAvailable(month):
			primaryLabel, primaryValue, primaryStatus = "MONTH LEFT", monthLabel, tokenPlanQuotaStatus(month)
		}
		model.Plans = append(model.Plans, TokenPlanRow{
			Name: name, PrimaryLabel: primaryLabel, PrimaryValue: primaryValue, PrimaryStatus: primaryStatus,
			FiveHour: fiveHourLabel, Week: weekLabel, Month: monthLabel,
			Reset: truncName(reset, 7), FiveHourStatus: tokenPlanQuotaStatus(fiveHour),
			WeekStatus: tokenPlanQuotaStatus(week), MonthStatus: tokenPlanQuotaStatus(month), Status: card.Status,
		})
	}
	if len(model.Plans) == 0 {
		model.PlanEmpty = "NO CODING DATA"
	}
	return model, nil
}

func tokenPlanQuotaStatus(percent *int) protocol.DisplayStatus {
	if percent == nil || *percent < 0 {
		return protocol.DisplayNA
	}
	switch {
	case *percent < 20:
		return protocol.DisplayCrit
	case *percent <= 50:
		return protocol.DisplayWarn
	default:
		return protocol.DisplayOK
	}
}

func tokenPlanValueAvailable(percent *int) bool {
	return percent != nil && *percent >= 0
}
