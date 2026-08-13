package ui

import (
	"sort"
	"strings"
	"time"

	"github.com/galendai/homepi-mon/internal/protocol"
)

// GroupCoding and GroupAPI are the Phase 1 metric groups (UI-001 4.1).
const (
	GroupCoding = "coding"
	GroupAPI    = "api"
)

// BuildOptions carries everything that is not in the snapshot itself.
type BuildOptions struct {
	// Page is the header page name; Phase 1 is always "CODING".
	Page string
	// Now is local time, used for the clock and for freshness derivation.
	Now time.Time
	// Connected reports whether the sync client currently holds a live stream.
	Connected bool
	// Thresholds drive the value-based badges.
	Thresholds protocol.Thresholds
	// Pi is the local device health line.
	Pi PiHealth
	// RetryIn is shown while waiting for the first snapshot.
	RetryIn *time.Duration
	// Version is the footer build label.
	Version string
	// FallbackNodeLabel is used before any snapshot has arrived.
	FallbackNodeLabel string
	RotationEnabled   bool
	PageNumber        int
	PageCount         int
	DwellSeconds      int
	RemoteNotice      *RemoteNotice
}

// Build maps a snapshot to a ViewModel.
//
// A nil snapshot produces the "no snapshot yet" screen rather than a blank or
// zero-filled one, so the display never invents data it does not have
// (UI-001 5.3).
func Build(snap *protocol.MetricSnapshot, opts BuildOptions) ViewModel {
	vm := ViewModel{
		Page:            opts.Page,
		Now:             opts.Now,
		Pi:              opts.Pi,
		RetryIn:         opts.RetryIn,
		Version:         opts.Version,
		NodeLabel:       opts.FallbackNodeLabel,
		RotationEnabled: opts.RotationEnabled,
		PageNumber:      opts.PageNumber,
		PageCount:       opts.PageCount,
		DwellSeconds:    opts.DwellSeconds,
		RemoteNotice:    opts.RemoteNotice,
	}
	if vm.Page == "" {
		vm.Page = "CODING"
	}

	if snap == nil {
		vm.Link = protocol.DeriveLinkStatus(false, opts.Connected, false)
		return vm
	}
	vm.HasSnapshot = true

	if snap.SourceNodeLabel != "" {
		vm.NodeLabel = snap.SourceNodeLabel
	} else {
		vm.NodeLabel = snap.SourceNode
	}

	coding, codingAlerts, codingCrit := buildCards(snap.Metrics, GroupCoding, opts)
	api, apiAlerts, apiCrit := buildCards(snap.Metrics, GroupAPI, opts)

	vm.Coding = coding
	vm.API = api
	nodes, nodeAlerts, nodeCrit := buildNodeCards(snap.HomeLabNodes, opts.Now)
	services, serviceAlerts, serviceCrit := buildServiceCards(snap.HomeLabServices, opts.Now)
	services, extraAlerts := appendMissingHomeLabHealth(services, snap.ConnectorHealth)
	serviceAlerts += extraAlerts
	connectors := buildConnectorCards(snap.ConnectorHealth)
	vm.Nodes, vm.Services, vm.Connectors = nodes, services, connectors
	vm.AlertCount = codingAlerts + apiAlerts + nodeAlerts + serviceAlerts
	vm.Link = protocol.DeriveLinkStatus(true, opts.Connected, codingCrit || apiCrit || nodeCrit || serviceCrit)

	age := opts.Now.Sub(snap.GeneratedAt)
	if age < 0 {
		age = 0
	}
	if opts.RotationEnabled {
		// Phase 3 ticks once per second to honor page deadlines, but the footer
		// only changes once per minute so an unchanged SPI frame is not rewritten
		// on every tick. Freshness status is still evaluated against exact time.
		age = age.Truncate(time.Minute)
	}
	vm.SyncedAgo = &age
	last := snap.GeneratedAt.In(opts.Now.Location())
	vm.LastSyncAt = &last

	return vm
}

func appendMissingHomeLabHealth(cards []ServiceCard, health []protocol.ConnectorHealth) ([]ServiceCard, int) {
	seen := make(map[string]bool, len(cards))
	for _, card := range cards {
		seen[card.Kind] = true
	}
	alerts := 0
	for _, item := range health {
		if item.Provider != "prometheus" && item.Provider != "grafana" && item.Provider != "portainer" || seen[item.Provider] {
			continue
		}
		status := protocol.DisplayOK
		switch item.State {
		case protocol.ConnBlockedAuth:
			status = protocol.DisplayAuth
		case protocol.ConnError:
			status = protocol.DisplayError
		case protocol.ConnDegraded:
			status = protocol.DisplayWarn
		case protocol.ConnDisabled:
			status = protocol.DisplayNA
		}
		cards = append(cards, ServiceCard{Name: item.ConnectorID, Kind: item.Provider, Status: status})
		seen[item.Provider] = true
		if status.Alerting() {
			alerts++
		}
	}
	return cards, alerts
}

func buildNodeCards(nodes []protocol.HomeLabNode, now time.Time) (cards []NodeCard, alerts int, critical bool) {
	for _, node := range nodes {
		status := homeLabStatus(node.Status, node.ErrorClass, node.ObservedAt, node.StaleAfter, now)
		if node.Online != nil && !*node.Online && node.ErrorClass == protocol.ErrNone {
			status = protocol.DisplayCrit
		}
		cards = append(cards, NodeCard{
			Name: node.Name, Online: node.Online, CPUPercent: node.CPUPercent,
			MemoryPercent: node.MemoryPercent, DiskPercent: node.DiskPercent,
			NetworkReceiveBPS: node.NetworkReceiveBPS, NetworkTransmitBPS: node.NetworkTransmitBPS,
			Status: status,
		})
		if status.Alerting() {
			alerts++
		}
		if status.Critical() {
			critical = true
		}
	}
	return cards, alerts, critical
}

func buildServiceCards(services []protocol.HomeLabService, now time.Time) (cards []ServiceCard, alerts int, critical bool) {
	for _, service := range services {
		status := homeLabStatus(service.Status, service.ErrorClass, service.ObservedAt, service.StaleAfter, now)
		if service.Healthy != nil && !*service.Healthy && service.ErrorClass == protocol.ErrNone {
			status = protocol.DisplayCrit
		}
		cards = append(cards, ServiceCard{
			Name: service.Name, Kind: service.Kind, Version: service.Version, Status: status,
			FiringAlerts: service.FiringAlerts, EnvironmentsTotal: service.EnvironmentsTotal,
			EnvironmentsOnline: service.EnvironmentsOnline, ContainersRunning: service.ContainersRunning,
			ContainersStopped: service.ContainersStopped, ContainersFailed: service.ContainersFailed,
			Stacks: service.Stacks,
		})
		if status.Alerting() {
			alerts++
		}
		if status.Critical() {
			critical = true
		}
	}
	return cards, alerts, critical
}

func buildConnectorCards(health []protocol.ConnectorHealth) []ConnectorCard {
	out := make([]ConnectorCard, 0, len(health))
	for _, item := range health {
		status := protocol.DisplayOK
		switch item.State {
		case protocol.ConnBlockedAuth:
			status = protocol.DisplayAuth
		case protocol.ConnError:
			status = protocol.DisplayError
		case protocol.ConnDegraded:
			status = protocol.DisplayWarn
		case protocol.ConnDisabled:
			status = protocol.DisplayNA
		}
		out = append(out, ConnectorCard{Name: item.ConnectorID, Status: status, Failures: item.ConsecutiveFailures})
	}
	return out
}

func homeLabStatus(status protocol.MetricStatus, class protocol.ErrorClass, observedAt time.Time, staleAfter protocol.Duration, now time.Time) protocol.DisplayStatus {
	switch class {
	case protocol.ErrAuth:
		return protocol.DisplayAuth
	case protocol.ErrUnsupported:
		return protocol.DisplayNA
	case protocol.ErrNetwork, protocol.ErrTimeout:
		return protocol.DisplayStale
	case protocol.ErrRateLimited:
		return protocol.DisplayDelayed
	case protocol.ErrSchemaChanged, protocol.ErrInvalidConfig, protocol.ErrUpstream:
		return protocol.DisplayError
	}
	budget := staleAfter.D()
	if budget <= 0 {
		budget = protocol.DefaultStaleAfter
	}
	if !observedAt.IsZero() && now.Sub(observedAt) > budget {
		return protocol.DisplayStale
	}
	switch status {
	case protocol.StatusCritical:
		return protocol.DisplayCrit
	case protocol.StatusWarning:
		return protocol.DisplayWarn
	case protocol.StatusError:
		return protocol.DisplayError
	case protocol.StatusStale:
		return protocol.DisplayStale
	case protocol.StatusUnknown:
		return protocol.DisplayNA
	default:
		return protocol.DisplayOK
	}
}

// CriticalPages returns the CRIT-only preemption set for the rotation state
// machine. AUTH/ERROR/WARN remain visible during normal rotation but do not
// permanently pin a page.
func CriticalPages(snap *protocol.MetricSnapshot, now time.Time, thresholds protocol.Thresholds) map[Page]bool {
	result := make(map[Page]bool)
	if snap == nil {
		return result
	}
	_, _, codingCrit := buildCards(snap.Metrics, GroupCoding, BuildOptions{Now: now, Thresholds: thresholds})
	_, _, apiCrit := buildCards(snap.Metrics, GroupAPI, BuildOptions{Now: now, Thresholds: thresholds})
	_, _, nodeCrit := buildNodeCards(snap.HomeLabNodes, now)
	_, _, serviceCrit := buildServiceCards(snap.HomeLabServices, now)
	result[PageCoding], result[PageAPI] = codingCrit, apiCrit
	result[PageHomeLab], result[PageServices] = nodeCrit, serviceCrit
	return result
}

// providerGroup collects the metrics that belong to one provider card.
type providerGroup struct {
	provider string
	name     string
	order    int
	primary  *protocol.ProviderMetric
	weekly   *protocol.ProviderMetric
	balance  *protocol.ProviderMetric
	status   protocol.DisplayStatus
	action   string
}

// buildCards groups metrics by provider and renders one card per provider.
//
// A provider's rolling-5h window and weekly window are two metrics sharing one
// card; the balance kind gets its own card in the API section. Grouping happens
// here rather than in the protocol so the wire format keeps each window as a
// distinct, independently sourced metric (PRD 5.1).
func buildCards(metrics []protocol.ProviderMetric, group string, opts BuildOptions) (cards []Card, alerts int, critical bool) {
	byProvider := map[string]*providerGroup{}
	var order []string

	for i := range metrics {
		m := metrics[i]
		if m.Group != group {
			continue
		}
		g, ok := byProvider[m.Provider]
		if !ok {
			g = &providerGroup{provider: m.Provider, name: m.DisplayName, order: m.Order}
			byProvider[m.Provider] = g
			order = append(order, m.Provider)
		}
		if m.Order < g.order {
			g.order = m.Order
		}
		if g.name == "" {
			g.name = m.DisplayName
		}

		switch {
		case m.MetricKind == protocol.KindBalance:
			g.balance = &metrics[i]
		case m.Window == protocol.WindowWeekly:
			g.weekly = &metrics[i]
		default:
			g.primary = &metrics[i]
		}
	}

	sort.SliceStable(order, func(i, j int) bool {
		a, b := byProvider[order[i]], byProvider[order[j]]
		if a.order != b.order {
			return a.order < b.order
		}
		return a.provider < b.provider
	})

	for _, provider := range order {
		g := byProvider[provider]
		card, isAlert, isCrit := g.toCard(opts)
		cards = append(cards, card)
		if isAlert {
			alerts++
		}
		if isCrit {
			critical = true
		}
	}
	return cards, alerts, critical
}

func (g *providerGroup) toCard(opts BuildOptions) (Card, bool, bool) {
	card := Card{Name: g.name}

	// The card's badge is the most severe of its metrics, so a healthy weekly
	// window can never mask an exhausted five-hour window.
	worst := protocol.DisplayOK
	rank := func(s protocol.DisplayStatus) int {
		switch s {
		case protocol.DisplayCrit:
			return 5
		case protocol.DisplayAuth:
			return 4
		case protocol.DisplayError:
			return 3
		case protocol.DisplayWarn:
			return 2
		case protocol.DisplayStale, protocol.DisplayDelayed, protocol.DisplayNA:
			return 1
		default:
			return 0
		}
	}
	consider := func(m *protocol.ProviderMetric) {
		if m == nil {
			return
		}
		s := protocol.DeriveDisplayStatus(*m, opts.Thresholds, opts.Now)
		if rank(s) > rank(worst) {
			worst = s
		}
		if m.Precision.Soft() {
			card.Estimated = true
		}
	}
	consider(g.primary)
	consider(g.weekly)
	consider(g.balance)
	card.Status = worst

	if g.balance != nil {
		card.Amount = formatAmount(g.balance)
	}
	if g.primary != nil {
		if p, ok := g.primary.Percent(); ok {
			card.PercentLeft = &p
		}
		card.ResetLabel = ResetLabel(g.primary.ResetsAt, opts.Now,
			g.primary.Window == protocol.WindowRolling5h)
	}
	if g.weekly != nil {
		if p, ok := g.weekly.Percent(); ok {
			card.WeekPercentLeft = &p
		}
	}

	// An AUTH card must show the remedy, never a stale value dressed as live.
	if worst == protocol.DisplayAuth {
		card.PercentLeft = nil
		card.WeekPercentLeft = nil
		card.Amount = ""
		card.ResetLabel = ""
		card.ActionHint = "re-auth with official CLI on " + opts.FallbackNodeLabel
	}

	return card, worst.Alerting(), worst.Critical()
}

// formatAmount renders "CNY 49.58". UI-001 9 requires the currency to always be
// present so a number is never ambiguous.
func formatAmount(m *protocol.ProviderMetric) string {
	if m.Value == nil {
		return ""
	}
	amount := m.Value.Rescale(2)
	unit := strings.ToUpper(strings.TrimSpace(m.Unit))
	if unit == "" {
		return amount
	}
	return unit + " " + amount
}
