package ui

import (
	"strconv"
	"strings"
	"time"
)

// sanitize strips every character that is not printable 7-bit ASCII.
//
// UI-001 2 restricts the screen to 7-bit ASCII and UI-001 9 requires control
// characters to be removed from all configurable text. Doing it at the single
// point where a line enters the frame means no caller can bypass it, and an
// ANSI escape in a provider name or remote message cannot reach the terminal.
func sanitize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 0x20 && c <= 0x7e {
			b.WriteByte(c)
		}
	}
	return b.String()
}

// fit pads with spaces or truncates so the result is exactly width cells.
func fit(s string, width int) string {
	if len(s) > width {
		return s[:width]
	}
	return s + strings.Repeat(" ", width-len(s))
}

// padTo pads s with spaces up to width. A string already at or past width is
// returned unchanged, so callers that overflow one column still get a
// well-formed line, and the final fit() clips it to the grid.
func padTo(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}

// padLeft right-aligns s inside width cells (UI-001 2: numbers align right).
func padLeft(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return strings.Repeat(" ", width-len(s)) + s
}

// clamp truncates s to at most n characters.
func clamp(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// truncName shortens an over-long provider name to n cells, marking the cut
// with a trailing '~' so the user can tell it was truncated (UI-001 3).
func truncName(s string, n int) string {
	s = sanitize(s)
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return clamp(s, n)
	}
	return s[:n-1] + "~"
}

// center horizontally centres s inside the inner content width.
func center(s string) string {
	s = sanitize(s)
	if len(s) >= inner {
		return clamp(s, inner)
	}
	left := (inner - len(s)) / 2
	return strings.Repeat(" ", left) + s
}

// bar renders the fixed 16-cell progress bar where '#' is remaining and '-' is
// consumed (UI-001 4.2).
//
// A nil percentage means the upstream gave no limit, so the bar is drawn empty
// and the caller shows "--" rather than a fabricated value.
func bar(percentLeft *int) string {
	if percentLeft == nil {
		return "[" + strings.Repeat("-", barCells) + "]"
	}
	p := *percentLeft
	if p < 0 {
		p = 0
	}
	if p > 100 {
		p = 100
	}
	// Round to the nearest cell.
	filled := (p*barCells + 50) / 100
	if filled > barCells {
		filled = barCells
	}
	return "[" + strings.Repeat("#", filled) + strings.Repeat("-", barCells-filled) + "]"
}

// percentLabel formats a percentage per UI-001 9: integers, "<1%" below one
// percent, "--" when unknown.
func percentLabel(p *int) string {
	if p == nil {
		return "--"
	}
	v := *p
	switch {
	case v < 0:
		return "--"
	case v == 0:
		return "0%"
	case v < 1:
		return "<1%"
	default:
		return strconv.Itoa(v) + "%"
	}
}

// shortDuration formats an elapsed age as "38S", "12M" or "2H" (UI-001 9).
// Ages truncate: something 119 seconds old is "1M ago", not "2M ago".
func shortDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return strconv.Itoa(int(d.Seconds())) + "S"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "M"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d.Hours())) + "H"
	default:
		return strconv.Itoa(int(d.Hours()/24)) + "D"
	}
}

// countdown formats a remaining duration, rounding to the nearest unit.
//
// A window that resets in one second under two hours reads "2H", which is what
// the user means by "resets in about two hours"; truncating it to "1H" would
// understate the wait on every single tick.
func countdown(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return strconv.Itoa(int(d.Round(time.Second).Seconds())) + "S"
	case d < time.Hour:
		return strconv.Itoa(int(d.Round(time.Minute).Minutes())) + "M"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d.Round(time.Hour).Hours())) + "H"
	default:
		return strconv.Itoa(int(d.Round(24*time.Hour).Hours()/24)) + "D"
	}
}

// ResetLabel renders the reset column: a countdown when the upstream gives a
// reset time, "ROLLING" for a rolling window with no known reset (UI-001 9).
func ResetLabel(resetsAt *time.Time, now time.Time, rolling bool) string {
	if resetsAt == nil {
		if rolling {
			return "ROLLING"
		}
		return ""
	}
	d := resetsAt.Sub(now)
	if d <= 0 {
		return "RESET NOW"
	}
	return "RESET " + countdown(d)
}

func itoa(i int) string { return strconv.Itoa(i) }

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
