package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/galendai/homepi-mon/internal/ui"
)

// Terminal lifecycle sequences used by the kiosk. The rich renderer adds only
// SGR foreground styles; neither layer enables mouse reporting or local input.
const (
	seqHideCursor  = "\x1b[?25l"
	seqShowCursor  = "\x1b[?25h"
	seqEnterAlt    = "\x1b[?1049h"
	seqLeaveAlt    = "\x1b[?1049l"
	seqCursorHome  = "\x1b[H"
	seqClearScreen = "\x1b[2J"
	seqResetStyle  = "\x1b[0m"
)

// screen writes whole frames to a terminal.
//
// It is output-only: nothing here reads stdin or enables any input mode, so a
// stray key press or touch event cannot reach the program (ADR-008).
type screen struct {
	mu    sync.Mutex
	out   io.Writer
	style ui.Style
	last  string
}

func newScreen(out io.Writer, style ui.Style) *screen {
	return &screen{out: out, style: style}
}

// enter switches to the alternate screen and hides the cursor.
func (s *screen) enter() {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _ = io.WriteString(s.out, seqEnterAlt+seqResetStyle+seqHideCursor+seqClearScreen)
}

// restore puts the terminal back the way it was found, including after a
// crash, so an operator arriving over SSH gets a usable console.
func (s *screen) restore() {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _ = io.WriteString(s.out, seqResetStyle+seqShowCursor+seqLeaveAlt)
}

// draw renders one frame, skipping the write when the frame is unchanged.
//
// Suppressing identical frames is what keeps a slow SPI panel from repainting
// on every timer tick (MOD-002 6).
func (s *screen) draw(vm ui.ViewModel) {
	s.drawRaw(ui.RenderStyledString(vm, s.style))
}

func (s *screen) drawRaw(frame string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if frame == s.last {
		return
	}
	s.last = frame

	// Home plus a full frame of fixed-width lines overwrites the previous
	// frame exactly, so no clear is needed and no tearing gap appears.
	var b strings.Builder
	b.Grow(len(frame) + 16)
	b.WriteString(seqCursorHome)
	b.WriteString(frame)
	_, _ = io.WriteString(s.out, b.String())
}

func sizeDiagnostic(cols, rows int) string {
	messages := []string{
		"HOMEPI DISPLAY",
		"TERMINAL TOO SMALL",
		fmt.Sprintf("HAVE %dX%d  NEED %dX%d", cols, rows, ui.Cols, ui.Rows),
		"ADJUST FONT OR ROTATION",
	}
	lines := make([]string, rows)
	for i := range lines {
		text := ""
		if i < len(messages) {
			text = messages[i]
		}
		if len(text) > cols {
			text = text[:cols]
		}
		lines[i] = text + strings.Repeat(" ", cols-len(text))
	}
	return strings.Join(lines, "\n")
}

// terminalSize reports the terminal dimensions.
//
// MOD-002 4.1 makes the runtime size authoritative: the 60x20 baseline is
// inferred from a photograph and must never be assumed.
func terminalSize(f *os.File) (cols, rows int, err error) {
	return ioctlWinsize(f)
}
