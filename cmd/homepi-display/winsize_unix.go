//go:build unix

package main

import (
	"os"

	"golang.org/x/sys/unix"
)

// ioctlWinsize asks the kernel for the terminal's real dimensions via
// TIOCGWINSZ, which is the authoritative source MOD-002 4.1 requires.
func ioctlWinsize(f *os.File) (cols, rows int, err error) {
	ws, err := unix.IoctlGetWinsize(int(f.Fd()), unix.TIOCGWINSZ)
	if err != nil {
		return 0, 0, err
	}
	return int(ws.Col), int(ws.Row), nil
}
