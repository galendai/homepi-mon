package ui_test

import (
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/galendai/homepi-mon/internal/protocol"
	"github.com/galendai/homepi-mon/internal/ui"
)

var sgrPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripSGR(value string) string { return sgrPattern.ReplaceAllString(value, "") }

func TestRichGridAndGlyphWhitelist(t *testing.T) {
	models := map[string]ui.ViewModel{
		"live":    baseModel(),
		"offline": offlineModel(),
		"auth":    authModel(),
		"empty":   emptyModel(),
		"crit":    critModel(),
		"hostile": hostileModel(),
	}
	allowedGlyphs := "┌─┐│├┤└┘█░"
	for name, vm := range models {
		t.Run(name, func(t *testing.T) {
			rich := ui.RenderStyledString(vm, ui.StyleRich)
			plain := stripSGR(rich)
			if strings.Contains(plain, "\x1b") {
				t.Fatal("non-SGR escape survived rich rendering")
			}
			lines := strings.Split(plain, "\n")
			if len(lines) != ui.Rows {
				t.Fatalf("rows = %d, want %d", len(lines), ui.Rows)
			}
			for row, line := range lines {
				if got := utf8.RuneCountInString(line); got != ui.Cols {
					t.Errorf("row %d cells = %d, want %d: %q", row+1, got, ui.Cols, line)
				}
				for _, r := range line {
					if r >= 0x20 && r <= 0x7e {
						continue
					}
					if !strings.ContainsRune(allowedGlyphs, r) {
						t.Errorf("row %d contains unapproved glyph %q", row+1, r)
					}
				}
			}
			if !strings.Contains(plain, "┌") || !strings.Contains(plain, "┘") {
				t.Error("rich frame does not use box-drawing corners")
			}
			if len(vm.Coding) > 0 &&
				(!strings.Contains(plain, "█") || !strings.Contains(plain, "░")) {
				t.Error("rich quota does not use both progress glyphs")
			}
		})
	}
}

func TestRichUsesOnlyApprovedSGRAndResetsEveryLine(t *testing.T) {
	rich := ui.RenderStyledString(hostileModel(), ui.StyleRich)
	approved := map[string]bool{
		"\x1b[0m": true, "\x1b[0;37m": true,
		"\x1b[1;31m": true, "\x1b[1;32m": true, "\x1b[1;33m": true,
		"\x1b[1;35m": true, "\x1b[1;36m": true,
		"\x1b[1;37m": true,
	}
	for _, match := range sgrPattern.FindAllString(rich, -1) {
		if !approved[match] {
			t.Errorf("unapproved SGR sequence %q", match)
		}
	}
	if residue := sgrPattern.ReplaceAllString(rich, ""); strings.ContainsRune(residue, '\x1b') {
		t.Fatal("escape outside approved SGR grammar survived")
	}
	for row, line := range strings.Split(rich, "\n") {
		if !strings.HasSuffix(line, "\x1b[0m") {
			t.Errorf("row %d does not reset style", row+1)
		}
	}
}

func TestRichBrightBorderAndCyanYellowProgress(t *testing.T) {
	cases := []struct {
		name      string
		status    protocol.DisplayStatus
		statusSGR string
	}{
		{"ok", protocol.DisplayOK, "\x1b[1;32m"},
		{"warn", protocol.DisplayWarn, "\x1b[1;33m"},
		{"crit", protocol.DisplayCrit, "\x1b[1;31m"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vm := baseModel()
			vm.Coding[0].Status = tc.status
			lines := strings.Split(ui.RenderStyledString(vm, ui.StyleRich), "\n")
			if got := activeSGRAt(lines[0], "┌"); got != "\x1b[1;36m" {
				t.Errorf("border style = %q, want bright cyan", got)
			}
			for _, token := range []string{"[", "█"} {
				if got := activeSGRAt(lines[4], token); got != "\x1b[1;36m" {
					t.Errorf("progress token %q style = %q, want bright cyan", token, got)
				}
			}
			if got := activeSGRAt(lines[4], "░"); got != "\x1b[1;33m" {
				t.Errorf("consumed progress style = %q, want bright yellow", got)
			}
			if got := activeSGRAt(lines[5], string(tc.status)); got != tc.statusSGR {
				t.Errorf("status %s style = %q, want %q", tc.status, got, tc.statusSGR)
			}
		})
	}
}

func TestRichStatusPaletteKeepsText(t *testing.T) {
	cases := []struct {
		status protocol.DisplayStatus
		sgr    string
	}{
		{protocol.DisplayOK, "\x1b[1;32m"},
		{protocol.DisplayWarn, "\x1b[1;33m"},
		{protocol.DisplayCrit, "\x1b[1;31m"},
		{protocol.DisplayDelayed, "\x1b[1;36m"},
		{protocol.DisplayStale, "\x1b[1;33m"},
		{protocol.DisplayAuth, "\x1b[1;35m"},
		{protocol.DisplayNA, "\x1b[1;37m"},
		{protocol.DisplayError, "\x1b[1;31m"},
	}
	for _, tc := range cases {
		t.Run(string(tc.status), func(t *testing.T) {
			vm := baseModel()
			vm.API[1].Status = tc.status
			lines := strings.Split(ui.RenderStyledString(vm, ui.StyleRich), "\n")
			line := lines[13]
			if !strings.Contains(stripSGR(line), string(tc.status)) {
				t.Fatalf("status text %q missing from %q", tc.status, stripSGR(line))
			}
			if got := activeSGRAt(line, string(tc.status)); got != tc.sgr {
				t.Errorf("status %s style = %q, want %q", tc.status, got, tc.sgr)
			}
		})
	}
}

func TestRichLinkPalette(t *testing.T) {
	cases := []struct {
		status protocol.LinkStatus
		sgr    string
	}{
		{protocol.LinkLive, "\x1b[1;32m"},
		{protocol.LinkOffline, "\x1b[1;33m"},
		{protocol.LinkWait, "\x1b[1;36m"},
		{protocol.LinkCrit, "\x1b[1;31m"},
	}
	for _, tc := range cases {
		vm := baseModel()
		vm.Link = tc.status
		line := strings.Split(ui.RenderStyledString(vm, ui.StyleRich), "\n")[1]
		if got := activeSGRAt(line, string(tc.status)); got != tc.sgr {
			t.Errorf("link %s style = %q, want %q", tc.status, got, tc.sgr)
		}
	}
}

func TestASCIIStyleMatchesExistingGoldenRenderer(t *testing.T) {
	for _, vm := range []ui.ViewModel{baseModel(), offlineModel(), authModel(), emptyModel()} {
		got := ui.RenderStyledString(vm, ui.StyleASCII)
		if got != ui.RenderString(vm) {
			t.Fatal("ASCII style differs from the existing renderer")
		}
		if strings.Contains(got, "\x1b") {
			t.Fatal("ASCII style emitted an escape")
		}
	}
}

func TestParseStyleAndStyledRenderingAreDeterministic(t *testing.T) {
	for input, want := range map[string]ui.Style{
		"": ui.StyleRich, "rich": ui.StyleRich, " RICH ": ui.StyleRich,
		"ascii": ui.StyleASCII, "ASCII": ui.StyleASCII,
	} {
		got, err := ui.ParseStyle(input)
		if err != nil || got != want {
			t.Errorf("ParseStyle(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	if _, err := ui.ParseStyle("rainbow"); err == nil {
		t.Fatal("unknown style was accepted")
	}

	vm := baseModel()
	for _, style := range []ui.Style{ui.StyleRich, ui.StyleASCII} {
		first := ui.RenderStyledString(vm, style)
		for i := 0; i < 20; i++ {
			if got := ui.RenderStyledString(vm, style); got != first {
				t.Fatalf("style %s is not deterministic", style)
			}
		}
	}
}

func activeSGRAt(line, token string) string {
	position := strings.LastIndex(line, token)
	if position < 0 {
		return ""
	}
	active := ""
	for _, match := range sgrPattern.FindAllString(line[:position], -1) {
		if match == "\x1b[0m" {
			active = ""
		} else {
			active = match
		}
	}
	return active
}
