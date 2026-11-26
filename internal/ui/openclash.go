package ui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/galendai/homepi-mon/internal/protocol"
)

const (
	// OpenClashCardLimit is the number of node cards shown on one sub-page,
	// laid out as two columns of five cards. Longer node lists split across
	// sub-pages that rotate on the dwell cycle instead of dropping nodes.
	OpenClashCardLimit = 10

	// Width budgets are ASCII cells. A CJK glyph consumes two cells.
	openClashNameCells = 13
)

// OpenClashRow is one proxy node's single-line display-ready card.
type OpenClashRow struct {
	Name  string
	Delay string
	// LegacyName is the pure-ASCII fallback label for the legacy grid.
	LegacyName string
	Selected   bool
	Tone       protocol.DisplayStatus
}

// OpenClashModel contains secret-free values for the native OpenClash card
// page. Rows holds the current sub-page of at most OpenClashCardLimit cards.
type OpenClashModel struct {
	cardPageChrome
	SubpageText string
	Empty       string
	// OverflowText is an abnormal-rendering fallback only; normal
	// pagination shows every node across rotating sub-pages.
	OverflowText string
	Rows         []OpenClashRow
}

// BuildOpenClashModel derives a stable, sorted view of the current proxy
// report as one card sub-page. Sub-pages advance on the dwell cycle so every
// node gets screen time; the current exit is highlighted when its card is
// visible instead of freezing pagination. A stale report retains node context
// and daemon-measured delays; authentication failures never present old delays
// as live values.
func BuildOpenClashModel(vm ViewModel) (OpenClashModel, error) {
	chrome, err := buildCardPageChrome(vm, "OPENCLASH")
	if err != nil {
		return OpenClashModel{}, err
	}
	model := OpenClashModel{cardPageChrome: chrome}
	model.FooterStatus = openClashFooterStatus(model.FooterStatus)
	report := vm.OpenClash
	if report == nil {
		model.Empty = "NO OPENCLASH DATA"
		model.SubpageText = "OPENCLASH 1/1"
		return model, nil
	}

	status := homeLabStatus(report.Status, report.ErrorClass, report.ObservedAt, report.StaleAfter, vm.Now)
	delaySuppressed := false
	switch status {
	case protocol.DisplayAuth:
		model.FooterStatus = "AUTH | " + model.FooterStatus
		delaySuppressed = true
	case protocol.DisplayDelayed:
		model.FooterStatus = "DELAY | " + model.FooterStatus
	case protocol.DisplayError:
		model.FooterStatus = "ERROR | " + model.FooterStatus
	case protocol.DisplayStale:
		model.FooterStatus = "STALE | " + model.FooterStatus
	}

	nodes := append([]protocol.OpenClashNode(nil), report.Nodes...)
	sort.SliceStable(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })
	rows := make([]OpenClashRow, 0, len(nodes))
	for index, node := range nodes {
		rows = append(rows, buildOpenClashRow(node, report, vm.Now, index, delaySuppressed))
	}

	page, pages := openClashSubpage(len(rows), vm.Now, vm.DwellSeconds)
	model.Rows = rows[page*OpenClashCardLimit : min((page+1)*OpenClashCardLimit, len(rows))]
	model.SubpageText = fmt.Sprintf("OPENCLASH %d/%d", page+1, pages)

	if len(model.Rows) == 0 {
		switch status {
		case protocol.DisplayAuth:
			model.Empty = "OPENCLASH AUTH"
		case protocol.DisplayDelayed:
			model.Empty = "OPENCLASH DELAYED"
		case protocol.DisplayError:
			model.Empty = "OPENCLASH ERROR"
		case protocol.DisplayStale:
			model.Empty = "OPENCLASH STALE"
		default:
			model.Empty = "NO OPENCLASH DATA"
		}
	}
	return model, nil
}

// openClashSubpage advances card sub-pages on the dwell cycle. Pages never
// freeze on the selected exit: its card is highlighted when visible while
// every node takes its turn in the card grid.
func openClashSubpage(count int, now time.Time, dwellSeconds int) (page, pages int) {
	if count <= OpenClashCardLimit {
		return 0, 1
	}
	pages = (count + OpenClashCardLimit - 1) / OpenClashCardLimit
	if dwellSeconds < MinDwellSeconds || dwellSeconds > MaxDwellSeconds {
		dwellSeconds = 15
	}
	bucket := now.Unix() / int64(dwellSeconds)
	page = int((bucket%int64(pages) + int64(pages)) % int64(pages))
	return page, pages
}

func openClashFooterStatus(status string) string {
	status = strings.ReplaceAll(status, "SNAPSHOT ", "SYNC ")
	return strings.ReplaceAll(status, " AGO", "")
}

func buildOpenClashRow(node protocol.OpenClashNode, report *protocol.OpenClashReport,
	now time.Time, index int, suppressDelay bool,
) OpenClashRow {
	row := OpenClashRow{
		Name:       openClashText(node.Name, fmt.Sprintf("NODE-%02d", index+1), openClashNameCells),
		Delay:      "--",
		LegacyName: openClashLegacyText(node.Name, index),
		Selected:   node.Name == report.SelectedNode,
		Tone:       protocol.DisplayNA,
	}
	if suppressDelay {
		row.Delay = "AUTH"
		return row
	}
	if missing := openClashDelayState(node, report, now); missing != "" {
		row.Delay = missing
		if missing == "FAIL" {
			row.Tone = protocol.DisplayCrit
		}
		return row
	}
	row.Delay = openClashValueText(*node.DelayMS, 3, "ms")
	switch {
	case *node.DelayMS < 300:
		row.Tone = protocol.DisplayOK
	case *node.DelayMS <= 800:
		row.Tone = protocol.DisplayWarn
	default:
		row.Tone = protocol.DisplayCrit
	}
	return row
}

// A usable delay requires a recent probe of this specific node.
func openClashDelayState(node protocol.OpenClashNode, report *protocol.OpenClashReport, now time.Time) string {
	switch {
	case node.CheckedAt == nil:
		return "--"
	case report.DelayStaleAfter.D() > 0 && now.Sub(*node.CheckedAt) > report.DelayStaleAfter.D():
		return "STALE"
	case node.Alive != nil && !*node.Alive:
		return "FAIL"
	case node.Alive == nil || node.DelayMS == nil || *node.DelayMS < 0:
		return "--"
	default:
		return ""
	}
}

func openClashValueText(value, digits int, suffix string) string {
	text := strconv.Itoa(value)
	if len(text) > digits {
		return "9999+"
	}
	return text + suffix
}

func openClashText(value, fallback string, maxCells int) string {
	runes := []rune(strings.ToValidUTF8(value, ""))
	kept := make([]rune, 0, len(runes))
	for _, char := range runes {
		if openClashDisplayRune(char) {
			kept = append(kept, char)
		}
	}
	cleaned := strings.TrimSpace(string(kept))
	if cleaned == "" {
		cleaned = fallback
	}
	return truncateOpenClashCells(cleaned, maxCells)
}

func openClashDisplayRune(char rune) bool {
	if char < 0x20 || char == 0x7f {
		return false
	}
	if char < 0x80 {
		return true
	}
	return unicode.Is(unicode.Han, char)
}

func truncateOpenClashCells(value string, maxCells int) string {
	if openClashCells(value) <= maxCells {
		return value
	}
	var out strings.Builder
	cells := 0
	for _, char := range value {
		width := 1
		if char >= 0x80 && unicode.Is(unicode.Han, char) {
			width = 2
		}
		if cells+width > maxCells-1 {
			out.WriteString("~")
			break
		}
		out.WriteRune(char)
		cells += width
	}
	return out.String()
}

func openClashCells(value string) int {
	cells := 0
	for _, char := range value {
		if char >= 0x80 && unicode.Is(unicode.Han, char) {
			cells += 2
			continue
		}
		cells++
	}
	return cells
}

func openClashLegacyText(value string, index int) string {
	return openClashLegacyLabel(value, fmt.Sprintf("ROW-%02d", index+1))
}

func openClashLegacyLabel(value, fallback string) string {
	visible := strings.TrimSpace(sanitize(value))
	if strings.Trim(visible, " -_.") == "" {
		return fallback
	}
	return truncName(visible, 12)
}

func (vm ViewModel) openClashBody() []string {
	withoutNotice := vm
	withoutNotice.RemoteNotice = nil
	model, err := BuildOpenClashModel(withoutNotice)
	if err != nil {
		return nil
	}
	// One sub-page title plus five rows of two 29-cell halves fit the fixed
	// legacy grid; frame() pads any remainder to 20 rows.
	body := []string{" " + model.SubpageText}
	if model.Empty != "" {
		body = append(body, center(model.Empty))
	}
	for index := range 5 {
		left, right := legacyOpenClashCard(OpenClashRow{}), legacyOpenClashCard(OpenClashRow{})
		if index < len(model.Rows) {
			left = legacyOpenClashCard(model.Rows[index])
		}
		if index+5 < len(model.Rows) {
			right = legacyOpenClashCard(model.Rows[index+5])
		}
		body = append(body, left+right)
	}
	return body
}

// legacyOpenClashCard renders one node as a 29-cell half line containing only
// its name and right-aligned ping value, matching the single-line native card.
// The selected exit uses "*" because legacy has no background-color channel.
func legacyOpenClashCard(row OpenClashRow) string {
	delay := sanitize(row.Delay)
	if len(delay) > 5 {
		delay = "9999+"
	}
	marker := " "
	if row.Selected {
		marker = "*"
	}
	return marker + padTo(truncName(row.LegacyName, 22), 22) + " " + padLeft(delay, 5)
}
