//go:build !unix

package main

import (
	"errors"
	"os"
)

// ioctlWinsize has no portable equivalent outside unix. The kiosk target is
// Linux on the Pi, so other platforms report the size as unavailable rather
// than guessing.
func ioctlWinsize(f *os.File) (cols, rows int, err error) {
	return 0, 0, errors.New("terminal size is not available on this platform")
}
