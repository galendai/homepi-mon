// Package ui renders the 60x20 Linux console kiosk defined by UI-Spec-001.
//
// Rendering is a pure function of a ViewModel: given the same model it always
// produces the same frame, which is what makes the golden-screen tests
// meaningful and keeps redraws deterministic (UI-001 10).
//
// Every line is padded or truncated to exactly Cols cells and the frame is
// exactly Rows lines, so nothing can ever be written outside the 60x20 grid.
package ui

import (
	"strings"
	"time"

	"github.com/galendai/homepi-mon/internal/protocol"
)

// Grid dimensions of the Phase 1 baseline screen (UI-001 3).
const (
	Cols = 60
	Rows = 20
	// inner is the usable width between the left and right border cells.
	inner = Cols - 2
	// barCells is the fixed progress-bar width (UI-001 4.2).
	barCells = 16
	// maxProviderName is the display-name budget (UI-001 3).
	maxProviderName = 12
)

// Column layout, as 0-based offsets inside the 58-cell content area.
//
// UI-001 4 draws these positions by hand and is one cell inconsistent between
// its normal and empty footers; the constants below standardise them so every
// state stacks in the same columns. The golden screens are the authoritative
// rendering.
const (
	colName      = 1  // provider name, maxProviderName wide
	colBar       = 13 // "[" + 16 cells + "]"
	colReset     = 46 // reset column, left aligned
	colBadgeEnd  = 52 // status badge, right aligned to this offset
	colAlertEnd  = 54 // section alert count, right aligned
	colAmountEnd = 28 // balance amount, right aligned
	colFooterR   = 24 // footer right-hand column
	colFooterEnd = 54 // footer far-right token, right aligned

	colHeadNode  = 9
	colHeadPage  = 26
	colHeadLink  = 39
	colHeadClock = 50

	colDevCPU = 13
	colDevMem = 25
	colDevLAN = 38

	colFootKiosk   = 21
	colFootVersion = 39
)

// Card is one provider row in the CODING PLANS or API BALANCE section.
type Card struct {
	// Name is the provider display name. Over-long names are truncated with a
	// trailing '~' (UI-001 3).
	Name string
	// PercentLeft is the primary remaining percentage; nil renders an empty bar
	// and "--" rather than a fabricated value (UI-001 4.2).
	PercentLeft *int
	// WeekPercentLeft is the optional weekly window shown on the detail line.
	WeekPercentLeft *int
	// ResetLabel is "RESET 2H", "ROLLING" or empty.
	ResetLabel string
	// Amount is the pre-formatted balance, e.g. "CNY 8.20". Used by API cards.
	Amount string
	// Status is the badge printed in the status column.
	Status protocol.DisplayStatus
	// Estimated appends EST to the primary value (UI-001 4.2).
	Estimated bool
	// ActionHint replaces the detail line with a required user action, e.g. the
	// official-CLI re-authentication instruction (UI-001 5.2).
	ActionHint string
}

// PiHealth is the device status line.
type PiHealth struct {
	TempC      int
	CPUPercent int
	MemPercent int
	LANStatus  protocol.DisplayStatus
	// Known false renders "--" placeholders instead of misleading zeros.
	Known bool
}

// ViewModel is everything the renderer needs. It holds no provider secrets and
// no raw upstream payloads.
type ViewModel struct {
	// NodeLabel is the single bound node's alias (ADR-014).
	NodeLabel string
	// Page is the page name in the header, e.g. "CODING".
	Page string
	// Link is the header connection badge.
	Link protocol.LinkStatus
	// Now is the local time used for the clock.
	Now time.Time

	Coding []Card
	API    []Card

	Pi PiHealth

	// AlertCount is the number of alerting cards shown in the header and footer.
	AlertCount int
	// SyncedAgo is the age of the displayed snapshot; nil means no snapshot yet.
	SyncedAgo *time.Duration
	// LastSyncAt is shown while offline (UI-001 5.1).
	LastSyncAt *time.Time
	// RetryIn is shown when there is no snapshot yet.
	RetryIn *time.Duration
	// Version is the footer build label, e.g. "V0.1.0".
	Version string
	// KioskLabel is the fixed footer text, defaulting to "KIOSK LOCKED".
	KioskLabel string
}

// Render produces the ASCII fallback as exactly Rows lines of exactly Cols
// printable 7-bit ASCII characters. The rich renderer decorates this safe frame.
func Render(vm ViewModel) []string {
	if vm.hasSnapshot() {
		return frame(vm.dataBody(), vm)
	}
	return frame(vm.emptyBody(), vm)
}

// RenderString joins the frame with newlines, for writing to a terminal.
func RenderString(vm ViewModel) string {
	return strings.Join(Render(vm), "\n")
}

func (vm ViewModel) hasSnapshot() bool {
	return len(vm.Coding) > 0 || len(vm.API) > 0
}

// frame assembles header, body, device line and footer into the fixed grid.
func frame(body []string, vm ViewModel) []string {
	// 5 border rows + 1 header + 1 device + 2 footer leaves 11 body rows.
	const bodyRows = Rows - 5 - 1 - 1 - 2

	out := make([]string, 0, Rows)
	out = append(out, border())
	out = append(out, content(vm.header()))
	out = append(out, border())
	for i := 0; i < bodyRows; i++ {
		if i < len(body) {
			out = append(out, content(body[i]))
		} else {
			out = append(out, content(""))
		}
	}
	out = append(out, border())
	out = append(out, content(vm.deviceLine()))
	out = append(out, border())
	for _, line := range vm.footer() {
		out = append(out, content(line))
	}
	out = append(out, border())
	return out
}

func border() string { return "+" + strings.Repeat("-", inner) + "+" }

// content wraps one body line in the side borders, forcing it to the exact
// inner width. Sanitising happens here so no upstream string can break the grid
// or inject a control sequence, whatever the caller passed in.
func content(s string) string {
	return "|" + fit(sanitize(s), inner) + "|"
}

// badged right-aligns a status badge at colBadgeEnd on an otherwise finished
// line.
func badged(line string, status protocol.DisplayStatus) string {
	s := string(status)
	if s == "" {
		return line
	}
	return padTo(line, colBadgeEnd-len(s)) + s
}

func (vm ViewModel) header() string {
	line := padTo(" HOMEPI", colHeadNode) + truncName(strings.ToUpper(vm.NodeLabel), maxProviderName)
	line = padTo(line, colHeadPage) + clamp(strings.ToUpper(vm.Page), 10)
	line = padTo(line, colHeadLink) + string(vm.Link)
	return padTo(line, colHeadClock) + vm.Now.Format("15:04")
}

func (vm ViewModel) dataBody() []string {
	out := make([]string, 0, 11)

	title := " CODING PLANS"
	if label := vm.alertLabel(); label != "" {
		title = padTo(title, colAlertEnd-len(label)) + label
	}
	out = append(out, title)
	for _, c := range vm.Coding {
		out = append(out, c.quotaLines()...)
	}

	out = append(out, "")
	out = append(out, " API BALANCE")
	for _, c := range vm.API {
		out = append(out, c.balanceLine())
	}
	return out
}

func (vm ViewModel) emptyBody() []string {
	return []string{
		"",
		"",
		center("WAITING FOR " + strings.ToUpper(vm.NodeLabel)),
		"",
		center("CHECK homepi-node STATUS ON REMOTE"),
		"",
		center("NO SNAPSHOT RECEIVED"),
	}
}

func (vm ViewModel) alertLabel() string {
	switch {
	case vm.AlertCount <= 0:
		return ""
	case vm.AlertCount == 1:
		return "1 WARN"
	default:
		return itoa(vm.AlertCount) + " WARN"
	}
}

// quotaLines renders a coding-plan card as a bar line plus a detail line.
//
// The badge sits on the detail line for a normal card, and on the main line for
// a card whose detail line is a required user action (UI-001 5.2).
func (c Card) quotaLines() []string {
	main := padTo(" ", colName) + padTo(truncName(c.Name, maxProviderName), colBar-colName)
	main += bar(c.PercentLeft) + " " + percentLabel(c.PercentLeft) + " LEFT"
	if c.Estimated {
		main += " EST"
	}

	if c.ActionHint != "" {
		return []string{
			badged(main, c.Status),
			"   ACTION: " + strings.ToUpper(c.ActionHint),
		}
	}

	main = padTo(main, colReset) + c.ResetLabel

	var detail string
	switch {
	case c.WeekPercentLeft != nil:
		detail = "   5H " + percentLabel(c.PercentLeft) + " LEFT | WEEK " +
			percentLabel(c.WeekPercentLeft) + " LEFT"
	case c.ResetLabel == "ROLLING":
		detail = "   5H ROLLING"
	default:
		detail = "   5H " + percentLabel(c.PercentLeft) + " LEFT"
	}
	return []string{main, badged(detail, c.Status)}
}

// balanceLine renders an API-balance card on a single row.
//
// UI-001 4.1 forbids dressing a prepaid balance up as a plan-remaining
// percentage, so this row has no progress bar and always keeps its currency.
func (c Card) balanceLine() string {
	amount := c.Amount
	if amount == "" {
		amount = "--"
	}
	if c.Estimated {
		amount += " EST"
	}
	line := padTo(" ", colName) + truncName(c.Name, maxProviderName+6)
	line += padLeft(amount, colAmountEnd-len(line))
	return badged(line, c.Status)
}

func (vm ViewModel) deviceLine() string {
	temp, cpu, mem := "--", "--", "--"
	if vm.Pi.Known {
		temp, cpu, mem = itoa(vm.Pi.TempC), itoa(vm.Pi.CPUPercent), itoa(vm.Pi.MemPercent)
	}
	line := " PI " + temp + "C"
	line = padTo(line, colDevCPU) + "CPU " + cpu + "%"
	line = padTo(line, colDevMem) + "MEM " + mem + "%"
	line = padTo(line, colDevLAN) + "LAN"
	return badged(line, vm.Pi.LANStatus)
}

func (vm ViewModel) footer() []string {
	var left, right string
	switch {
	case vm.SyncedAgo == nil:
		left, right = " DATA NONE", "RETRYING"
		if vm.RetryIn != nil {
			right = "RETRY IN " + shortDuration(*vm.RetryIn)
		}
	case vm.Link == protocol.LinkOffline:
		left = " DATA STALE " + shortDuration(*vm.SyncedAgo)
		right = "RETRYING"
		if vm.LastSyncAt != nil {
			right = "LAST SYNC " + vm.LastSyncAt.Format("15:04")
		}
	case vm.AlertCount == 1:
		left, right = " 1 WARNING", "SYNCED "+shortDuration(*vm.SyncedAgo)+" AGO"
	case vm.AlertCount > 1:
		left, right = " "+itoa(vm.AlertCount)+" WARNINGS", "SYNCED "+shortDuration(*vm.SyncedAgo)+" AGO"
	default:
		left, right = " ALL OK", "SYNCED "+shortDuration(*vm.SyncedAgo)+" AGO"
	}
	first := padTo(left, colFooterR) + right
	if vm.Link == protocol.LinkOffline && vm.LastSyncAt != nil {
		first = padTo(first, colFooterEnd-len("RETRYING")) + "RETRYING"
	}

	kiosk := vm.KioskLabel
	if kiosk == "" {
		kiosk = "KIOSK LOCKED"
	}
	second := " SOURCE " + truncName(strings.ToUpper(vm.NodeLabel), maxProviderName)
	second = padTo(second, colFootKiosk) + kiosk
	second = padTo(second, colFootVersion) + vm.Version

	return []string{first, second}
}
