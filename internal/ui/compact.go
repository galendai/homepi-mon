package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/galendai/homepi-mon/internal/protocol"
)

const compactDataRows = Rows - 7

type compactRow struct {
	text    string
	status  protocol.DisplayStatus
	heading bool
	colors  []compactColor
}

type compactColor struct {
	start, end int
	style      string
}

// RenderKioskString selects the compact overview for the logical CODING page.
// Other pages and remote notices retain their existing detail presentation.
// A full overview falls back to visibly labelled, paginated CODING details.
func RenderKioskString(vm ViewModel, style Style) string {
	if Page(strings.ToUpper(vm.Page)) != PageCoding || vm.RemoteNotice != nil {
		return RenderStyledString(vm, style)
	}
	compact := vm
	if vm.SyncedAgo != nil {
		// Keep paused rotation from repainting a slow SPI panel every second.
		// Build has already derived freshness against the original exact time.
		age := vm.SyncedAgo.Truncate(time.Minute)
		compact.SyncedAgo = &age
	}
	if frame, err := RenderCompactOverviewString(compact, style); err == nil {
		return frame
	}
	vm.compactFallback = true
	return RenderStyledString(vm, style)
}

// RenderCompactOverviewString renders the strict overview in UI-001 6.2.
// It rejects overflow instead of hiding entities or clipping main values.
func RenderCompactOverviewString(vm ViewModel, style Style) (string, error) {
	if style != StyleASCII && style != StyleRich {
		return "", fmt.Errorf("compact overview: invalid style %q", style)
	}
	if vm.RemoteNotice != nil {
		return "", fmt.Errorf("compact overview: remote notices use the detail layout")
	}
	data := []compactRow{{
		text:    fmt.Sprintf(" %-16s %5s %5s %5s %-11s %8s", "PLAN / LEFT", "5H", "WEEK", "MONTH", "RESET", "STATUS"),
		heading: true,
	}}
	if !vm.hasSnapshot() {
		data = append(data, compactRow{text: " NO SNAPSHOT - CHECK HOMEPI-NODE ON SOURCE HOST"})
	} else {
		if len(vm.Coding) == 0 {
			data = append(data, compactRow{text: " NO CODING DATA"})
		}
		for _, card := range vm.Coding {
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
			data = append(data, compactRow{
				text: fmt.Sprintf(" %-16s %5s %5s %5s %-11s %8s", name,
					percentLabel(fiveHour), percentLabel(week), percentLabel(month), reset, card.Status),
				status: card.Status,
				colors: []compactColor{
					{19, 24, quotaPercentStyle(fiveHour)},
					{25, 30, quotaPercentStyle(week)},
					{31, 36, quotaPercentStyle(month)},
				},
			})
			if card.ActionHint != "" {
				data = append(data, compactRow{text: "   ACTION: " + truncName(card.ActionHint, inner-11)})
			}
		}
		data = append(data, compactRow{}, compactRow{text: " API BALANCE (CURRENCY)", heading: true})
		if len(vm.API) == 0 {
			data = append(data, compactRow{text: " NO API DATA"})
		}
		for _, card := range vm.API {
			amount := sanitize(card.Amount)
			if amount == "" || card.Status == protocol.DisplayAuth {
				amount = "--"
			} else if card.Estimated {
				amount += " EST"
			}
			data = append(data, compactRow{
				text:   fmt.Sprintf(" %-16s %29s %8s", truncName(card.Name, 16), amount, card.Status),
				status: card.Status,
			})
			if card.ActionHint != "" {
				data = append(data, compactRow{text: "   ACTION: " + truncName(card.ActionHint, inner-11)})
			}
		}
	}
	if len(data) > compactDataRows {
		return "", fmt.Errorf("compact overview needs %d data rows; %d available", len(data), compactDataRows)
	}
	for index, row := range data {
		if len(sanitize(row.text)) > inner {
			return "", fmt.Errorf("compact overview data row %d exceeds %d cells; main values cannot be clipped", index+1, inner)
		}
	}

	vm.Page = "OVERVIEW"
	rows := make([]compactRow, Rows)
	rows[0].text, rows[Rows-4].text, rows[Rows-1].text = border(), border(), border()
	rows[1].text = content(vm.header())
	rows[2].text = border()
	for index := 0; index < compactDataRows; index++ {
		if index < len(data) {
			rows[index+3] = data[index]
		}
		rows[index+3].text = content(rows[index+3].text)
	}
	rows[Rows-3] = compactRow{text: content(vm.deviceLine()), status: vm.Pi.LANStatus}
	footer := " NO DATA | CHECK HOMEPI-NODE"
	if vm.SyncedAgo != nil {
		footer = fmt.Sprintf(" %d ALERTS | SNAPSHOT %s AGO", vm.AlertCount, shortDuration(*vm.SyncedAgo))
		if vm.Link == protocol.LinkOffline {
			footer = " OFFLINE |" + footer
		}
	}
	rows[Rows-2].text = content(footer + compactRotationLabel(vm))
	frame := make([]string, Rows)
	for index, row := range rows {
		frame[index] = row.text
		if style == StyleASCII {
			continue
		}
		cells := []rune(row.text)
		styles := make([]string, Cols)
		borderRow := index == 0 || index == 2 || index == Rows-4 || index == Rows-1
		if borderRow {
			frameRow := 2
			if index == 0 || index == Rows-1 {
				frameRow = index
			}
			decorateFrame(cells, styles, frameRow)
		} else {
			decorateFrame(cells, styles, 1)
			paint(styles, 1, Cols-1, sgrWhite)
			if row.heading {
				paint(styles, 1, Cols-1, sgrCyan)
			}
			if index == 1 {
				paint(styles, 1+colHeadNode, 1+colHeadPage, sgrMagenta)
				paint(styles, 1+colHeadPage, 1+colHeadLink, sgrCyan)
				paint(styles, 1+colHeadLink, 1+colHeadClock, linkStyle(vm.Link))
			}
			if index == Rows-2 {
				paint(styles, 1, Cols-1, footerStyle(vm))
			}
			if row.status != "" {
				paintLastToken(styles, cells, string(row.status), statusStyle(row.status))
			}
			for _, color := range row.colors {
				paint(styles, color.start, color.end, color.style)
			}
			paintToken(styles, cells, "EST", sgrYellow)
		}
		frame[index] = encodeStyled(cells, styles)
	}
	return strings.Join(frame, "\n"), nil
}

func compactRotationLabel(vm ViewModel) string {
	if !vm.RotationEnabled && vm.PageCount <= 1 {
		return ""
	}
	number, count, dwell := vm.PageNumber, vm.PageCount, vm.DwellSeconds
	if number <= 0 {
		number = 1
	}
	if count <= 0 {
		count = 5
	}
	if dwell <= 0 {
		dwell = 15
	}
	mode := "HOLD"
	if vm.RotationEnabled {
		mode = fmt.Sprintf("AUTO %dS", dwell)
	}
	return fmt.Sprintf(" | %d/%d %s", number, count, mode)
}
