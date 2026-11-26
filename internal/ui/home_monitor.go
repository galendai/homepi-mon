package ui

import (
	"fmt"
	"strings"

	"github.com/galendai/homepi-mon/internal/protocol"
)

// HomeMonitor is the single status line above the native HOME clock.
type HomeMonitor struct {
	Source string
	Detail string
	Status protocol.DisplayStatus
}

func homeMonitor(vm ViewModel) HomeMonitor {
	if vm.Link == protocol.LinkWait || vm.Link == protocol.LinkOffline {
		return HomeMonitor{}
	}
	var best HomeMonitor
	bestPriority := 0
	consider := func(name, fallback, detail string, status protocol.DisplayStatus) {
		priority := 0
		switch status {
		case protocol.DisplayCrit:
			priority = 3
		case protocol.DisplayAuth, protocol.DisplayError:
			priority = 2
		case protocol.DisplayWarn:
			priority = 1
		}
		if priority <= bestPriority {
			return
		}
		name = strings.TrimSpace(sanitize(name))
		if name == "" {
			name = fallback
		}
		if detail == "" {
			detail = string(status)
		}
		best = HomeMonitor{Source: strings.ToUpper(name), Detail: detail, Status: status}
		bestPriority = priority
	}
	for _, card := range vm.Coding {
		consider(card.Name, "PLAN", card.AlertDetail, card.Status)
	}
	for _, card := range vm.API {
		consider(card.Name, "API", card.AlertDetail, card.Status)
	}
	for _, node := range vm.Nodes {
		detail := ""
		if node.Status == protocol.DisplayCrit && node.Online != nil && !*node.Online {
			detail = "OFFLINE"
		}
		consider(node.Name, "NODE", detail, node.Status)
	}
	for _, service := range vm.Services {
		detail := ""
		if service.Status == protocol.DisplayWarn || service.Status == protocol.DisplayCrit {
			switch {
			case service.FiringAlerts != nil && *service.FiringAlerts > 0:
				detail = fmt.Sprintf("%d FIRING", *service.FiringAlerts)
			case service.Healthy != nil && !*service.Healthy:
				detail = "UNHEALTHY"
			}
		}
		consider(service.Name, "SERVICE", detail, service.Status)
	}
	if bestPriority > 0 {
		return best
	}
	return homeProxyMonitor(vm)
}

func metricAlertDetail(m *protocol.ProviderMetric, status protocol.DisplayStatus) string {
	if status == protocol.DisplayAuth || status == protocol.DisplayError {
		return string(status)
	}
	estimate := ""
	if m.Precision.Soft() {
		estimate = "~"
	}
	switch m.MetricKind {
	case protocol.KindQuota:
		if pct, ok := m.Percent(); ok {
			window := "QUOTA"
			switch m.Window {
			case protocol.WindowRolling5h:
				window = "5H"
			case protocol.WindowDaily:
				window = "DAY"
			case protocol.WindowWeekly:
				window = "WEEK"
			case protocol.WindowMonthly:
				window = "MONTH"
				if m.Provider == "kimi" && strings.HasSuffix(m.ID, ".monthly-code") {
					window = "CODE"
				}
			}
			return fmt.Sprintf("%s   %s%d%% LEFT", window, estimate, pct)
		}
	case protocol.KindBalance:
		if amount := formatAmount(m); amount != "" && len(amount) <= 16 {
			return estimate + amount
		}
		return "LOW BALANCE"
	}
	return string(status)
}

func homeProxyMonitor(vm ViewModel) HomeMonitor {
	report := vm.OpenClash
	if report == nil {
		return HomeMonitor{}
	}
	status := homeLabStatus(report.Status, report.ErrorClass, report.ObservedAt, report.StaleAfter, vm.Now)
	switch status {
	case protocol.DisplayAuth, protocol.DisplayError, protocol.DisplayStale, protocol.DisplayDelayed, protocol.DisplayNA:
		return HomeMonitor{"PROXY", string(status), status}
	}
	for _, node := range report.Nodes {
		if node.Name != report.SelectedNode || report.SelectedNode == "" {
			continue
		}
		switch state := openClashDelayState(node, report, vm.Now); state {
		case "":
			return HomeMonitor{"PROXY", fmt.Sprintf("%d ms", *node.DelayMS), protocol.DisplayOK}
		case "FAIL":
			return HomeMonitor{"PROXY", state, protocol.DisplayCrit}
		default:
			return HomeMonitor{"PROXY", state, protocol.DisplayNA}
		}
	}
	return HomeMonitor{"PROXY", "--", protocol.DisplayNA}
}
