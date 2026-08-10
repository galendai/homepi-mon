package main

import (
	"io"
	"os"
	"strings"
	"sync"

	"github.com/galendai/homepi-mon/internal/ui"
)

// ANSI sequences used by the kiosk. This is the entire set: no colour, no
// mouse reporting, no alternate-screen scrollback and no cursor movement
// beyond homing, which keeps the SPI panel's redraw cost predictable
// (UI-001 10).
const (
	seqHideCursor  = "\x1b[?25l"
	seqShowCursor  = "\x1b[?25h"
	seqEnterAlt    = "\x1b[?1049h"
	seqLeaveAlt    = "\x1b[?1049l"
	seqCursorHome  = "\x1b[H"
	seqClearScreen = "\x1b[2J"
)

// screen writes whole frames to a terminal.
//
// It is output-only: nothing here reads stdin or enables any input mode, so a
// stray key press or touch event cannot reach the program (ADR-008).
type screen struct {
	mu   sync.Mutex
	out  io.Writer
	last string
}

func newScreen(out io.Writer) *screen { return &screen{out: out} }

// enter switches to the alternate screen and hides the cursor.
func (s *screen) enter() {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _ = io.WriteString(s.out, seqEnterAlt+seqHideCursor+seqClearScreen)
}

// restore puts the terminal back the way it was found, including after a
// crash, so an operator arriving over SSH gets a usable console.
func (s *screen) restore() {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _ = io.WriteString(s.out, seqShowCursor+seqLeaveAlt)
}

// draw renders one frame, skipping the write when the frame is unchanged.
//
// Suppressing identical frames is what keeps a slow SPI panel from repainting
// on every timer tick (MOD-002 6).
func (s *screen) draw(vm ui.ViewModel) {
	frame := ui.RenderString(vm)

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

// terminalSize reports the terminal dimensions.
//
// MOD-002 4.1 makes the runtime size authoritative: the 60x20 baseline is
// inferred from a photograph and must never be assumed.
func terminalSize(f *os.File) (cols, rows int, err error) {
	return ioctlWinsize(f)
}
