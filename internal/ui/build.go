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
}

// Build maps a snapshot to a ViewModel.
//
// A nil snapshot produces the "no snapshot yet" screen rather than a blank or
// zero-filled one, so the display never invents data it does not have
// (UI-001 5.3).
func Build(snap *protocol.MetricSnapshot, opts BuildOptions) ViewModel {
	vm := ViewModel{
		Page:      opts.Page,
		Now:       opts.Now,
		Pi:        opts.Pi,
		RetryIn:   opts.RetryIn,
		Version:   opts.Version,
		NodeLabel: opts.FallbackNodeLabel,
	}
	if vm.Page == "" {
		vm.Page = "CODING"
	}

	if snap == nil {
		vm.Link = protocol.DeriveLinkStatus(false, opts.Connected, false)
		return vm
	}

	if snap.SourceNodeLabel != "" {
		vm.NodeLabel = snap.SourceNodeLabel
	} else {
		vm.NodeLabel = snap.SourceNode
	}

	coding, codingAlerts, codingCrit := buildCards(snap.Metrics, GroupCoding, opts)
	api, apiAlerts, apiCrit := buildCards(snap.Metrics, GroupAPI, opts)

	vm.Coding = coding
	vm.API = api
	vm.AlertCount = codingAlerts + apiAlerts
	vm.Link = protocol.DeriveLinkStatus(true, opts.Connected, codingCrit || apiCrit)

	age := opts.Now.Sub(snap.GeneratedAt)
	if age < 0 {
		age = 0
	}
	vm.SyncedAgo = &age
	last := snap.GeneratedAt.In(opts.Now.Location())
	vm.LastSyncAt = &last

	return vm
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
