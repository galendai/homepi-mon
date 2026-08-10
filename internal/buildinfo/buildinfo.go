// Package buildinfo exposes build-time metadata shared by all HomePi binaries.
//
// Values are injected at link time with -ldflags. When a binary is built
// without those flags (for example by "go run" or "go test"), the defaults
// below keep the output well-formed instead of empty.
package buildinfo

import (
	"fmt"
	"runtime"
	"strings"
)

// Injected via -ldflags "-X github.com/galendai/homepi-mon/internal/buildinfo.Version=..."
var (
	// Version is the semantic product version, e.g. "0.1.0".
	Version = "0.1.0-dev"
	// Commit is the short git commit the binary was built from.
	Commit = "unknown"
	// Date is the RFC 3339 UTC build timestamp.
	Date = "unknown"
)

// Platform returns the GOOS/GOARCH pair the binary was compiled for.
func Platform() string {
	p := runtime.GOOS + "/" + runtime.GOARCH
	if runtime.GOARCH == "arm" {
		// GOARM is not readable at runtime; the build matrix pins v7 for the Pi.
		p += "v7"
	}
	return p
}

// Short returns a single-line version string, e.g. "0.1.0 (abc1234)".
func Short() string {
	return fmt.Sprintf("%s (%s)", Version, Commit)
}

// UIVersion returns the version rendered in the 60x20 TUI footer, e.g. "V0.1.0".
// It is uppercased, ASCII-only and truncated so it never breaks the grid.
func UIVersion() string {
	v := Version
	if i := strings.IndexAny(v, "-+"); i > 0 {
		v = v[:i]
	}
	v = "V" + strings.ToUpper(v)
	if len(v) > 10 {
		v = v[:10]
	}
	return v
}

// String returns the multi-line output of the --version flag.
func String(binary string) string {
	return fmt.Sprintf("%s %s\ncommit:   %s\nbuilt:    %s\nplatform: %s\ngo:       %s",
		binary, Version, Commit, Date, Platform(), runtime.Version())
}
