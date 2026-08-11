package ui

import (
	"fmt"
	"strings"

	"github.com/galendai/homepi-mon/internal/protocol"
)

// Style selects the terminal presentation without changing the 60x20 layout.
type Style string

const (
	StyleRich  Style = "rich"
	StyleASCII Style = "ascii"
)

// ParseStyle validates a configured display style. An empty value selects the
// rich DietPi console theme; callers must reject every other unknown value.
func ParseStyle(value string) (Style, error) {
	switch Style(strings.ToLower(strings.TrimSpace(value))) {
	case "", StyleRich:
		return StyleRich, nil
	case StyleASCII:
		return StyleASCII, nil
	default:
		return "", fmt.Errorf("unknown display style %q; want rich or ascii", value)
	}
}

// RenderStyledString renders the selected presentation. Style values must come
// from ParseStyle; an invalid value is an internal programming error.
func RenderStyledString(vm ViewModel, style Style) string {
	switch style {
	case StyleASCII:
		return RenderString(vm)
	case StyleRich:
		return strings.Join(renderRich(vm), "\n")
	default:
		panic("ui: RenderStyledString called with invalid style")
	}
}

const (
	sgrReset   = "\x1b[0m"
	sgrWhite   = "\x1b[1;37m"
	sgrCyan    = "\x1b[1;36m"
	sgrGreen   = "\x1b[1;32m"
	sgrYellow  = "\x1b[1;33m"
	sgrRed     = "\x1b[1;31m"
	sgrMagenta = "\x1b[1;35m"
	sgrMuted   = "\x1b[0;37m"
)

func renderRich(vm ViewModel) []string {
	plain := Render(vm)
	out := make([]string, len(plain))
	for row, line := range plain {
		out[row] = richLine([]rune(line), row, vm)
	}
	return out
}

func richLine(cells []rune, row int, vm ViewModel) string {
	styles := make([]string, len(cells))
	decorateFrame(cells, styles, row)

	switch row {
	case 1:
		paint(styles, 2, 8, sgrCyan)
		paint(styles, 1+colHeadNode, 1+colHeadPage, sgrMagenta)
		paint(styles, 1+colHeadPage, 1+colHeadLink, sgrCyan)
		paint(styles, 1+colHeadLink, 1+colHeadClock, linkStyle(vm.Link))
		paint(styles, 1+colHeadClock, Cols-1, sgrWhite)
	case 15:
		paint(styles, 1, Cols-1, sgrWhite)
		paintStatus(styles, cells, vm.Pi.LANStatus)
	case 17:
		paint(styles, 1, Cols-1, footerStyle(vm))
	case 18:
		paint(styles, 1, Cols-1, sgrMuted)
		paintToken(styles, cells, "KIOSK LOCKED", sgrMagenta)
	}

	if vm.hasSnapshot() {
		decorateData(styles, cells, row, vm)
	} else {
		decorateEmpty(styles, cells, row)
	}

	return encodeStyled(cells, styles)
}

func decorateFrame(cells []rune, styles []string, row int) {
	if row == 0 || row == 2 || row == 14 || row == 16 || row == Rows-1 {
		for i := range cells {
			styles[i] = sgrCyan
		}
		for i := 1; i < len(cells)-1; i++ {
			cells[i] = '─'
		}
		switch row {
		case 0:
			cells[0], cells[len(cells)-1] = '┌', '┐'
		case Rows - 1:
			cells[0], cells[len(cells)-1] = '└', '┘'
		default:
			cells[0], cells[len(cells)-1] = '├', '┤'
		}
		return
	}
	styles[0], styles[len(styles)-1] = sgrCyan, sgrCyan
	cells[0], cells[len(cells)-1] = '│', '│'
}

func decorateData(styles []string, cells []rune, row int, vm ViewModel) {
	if row == 3 {
		paintToken(styles, cells, "CODING PLANS", sgrCyan)
		if vm.AlertCount > 0 {
			paintToken(styles, cells, vm.alertLabel(), alertStyle(vm))
		}
		return
	}

	for i, card := range vm.Coding {
		mainRow := 4 + i*2
		if row == mainRow {
			paint(styles, 1+colName, 1+colBar, sgrCyan)
			paintBar(styles, cells)
			paintStatus(styles, cells, card.Status)
			return
		}
		if row == mainRow+1 {
			paint(styles, 1, Cols-1, sgrMuted)
			paintStatus(styles, cells, card.Status)
			if card.ActionHint != "" {
				paint(styles, 1, Cols-1, statusStyle(card.Status))
			}
			return
		}
	}

	apiTitleRow := 5 + len(vm.Coding)*2
	if row == apiTitleRow {
		paintToken(styles, cells, "API BALANCE", sgrCyan)
		return
	}
	for i, card := range vm.API {
		if row == apiTitleRow+1+i {
			paint(styles, 1+colName, 1+colName+maxProviderName+6, sgrCyan)
			paintStatus(styles, cells, card.Status)
			return
		}
	}
}

func decorateEmpty(styles []string, cells []rune, row int) {
	switch row {
	case 5, 9:
		paint(styles, 1, Cols-1, sgrCyan)
	case 7:
		paint(styles, 1, Cols-1, sgrYellow)
	}
}

func paintBar(styles []string, cells []rune) {
	open, close := -1, -1
	for i, cell := range cells {
		switch cell {
		case '[':
			open = i
		case ']':
			if open >= 0 {
				close = i
			}
		}
	}
	if open < 0 || close <= open {
		return
	}
	paint(styles, open, close+1, sgrCyan)
	for i := open + 1; i < close; i++ {
		switch cells[i] {
		case '#':
			cells[i] = '█'
		case '-':
			cells[i] = '░'
			styles[i] = sgrYellow
		}
	}
}

func paintStatus(styles []string, cells []rune, status protocol.DisplayStatus) {
	if status == "" {
		return
	}
	paintLastToken(styles, cells, string(status), statusStyle(status))
}

func paintToken(styles []string, cells []rune, token, style string) {
	start := findToken(cells, []rune(token), false)
	if start >= 0 {
		paint(styles, start, start+len([]rune(token)), style)
	}
}

func paintLastToken(styles []string, cells []rune, token, style string) {
	start := findToken(cells, []rune(token), true)
	if start >= 0 {
		paint(styles, start, start+len([]rune(token)), style)
	}
}

func findToken(cells, token []rune, last bool) int {
	match := -1
	for start := 0; start+len(token) <= len(cells); start++ {
		found := true
		for i := range token {
			if cells[start+i] != token[i] {
				found = false
				break
			}
		}
		if found {
			match = start
			if !last {
				return match
			}
		}
	}
	return match
}

func paint(styles []string, start, end int, style string) {
	if start < 0 {
		start = 0
	}
	if end > len(styles) {
		end = len(styles)
	}
	for i := start; i < end; i++ {
		styles[i] = style
	}
}

func encodeStyled(cells []rune, styles []string) string {
	var b strings.Builder
	current := ""
	for i, cell := range cells {
		if styles[i] != current {
			if current != "" {
				b.WriteString(sgrReset)
			}
			if styles[i] != "" {
				b.WriteString(styles[i])
			}
			current = styles[i]
		}
		b.WriteRune(cell)
	}
	if current != "" {
		b.WriteString(sgrReset)
	}
	return b.String()
}

func statusStyle(status protocol.DisplayStatus) string {
	switch status {
	case protocol.DisplayOK:
		return sgrGreen
	case protocol.DisplayWarn, protocol.DisplayStale:
		return sgrYellow
	case protocol.DisplayCrit, protocol.DisplayError:
		return sgrRed
	case protocol.DisplayAuth:
		return sgrMagenta
	case protocol.DisplayDelayed:
		return sgrCyan
	default:
		return sgrWhite
	}
}

func linkStyle(status protocol.LinkStatus) string {
	switch status {
	case protocol.LinkLive:
		return sgrGreen
	case protocol.LinkOffline:
		return sgrYellow
	case protocol.LinkCrit:
		return sgrRed
	default:
		return sgrCyan
	}
}

func alertStyle(vm ViewModel) string {
	if vm.Link == protocol.LinkCrit {
		return sgrRed
	}
	for _, card := range vm.Coding {
		if card.Status == protocol.DisplayCrit || card.Status == protocol.DisplayError {
			return sgrRed
		}
	}
	for _, card := range vm.API {
		if card.Status == protocol.DisplayCrit || card.Status == protocol.DisplayError {
			return sgrRed
		}
	}
	return sgrYellow
}

func footerStyle(vm ViewModel) string {
	switch {
	case vm.SyncedAgo == nil:
		return sgrCyan
	case vm.Link == protocol.LinkOffline || vm.AlertCount > 0:
		return sgrYellow
	default:
		return sgrGreen
	}
}
