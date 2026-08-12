// Package install lays down a per-user service definition for the
// homepi-node daemon.
//
// Phase 1 targets three platforms:
//
//   - macOS   : LaunchAgent plist under ~/Library/LaunchAgents, loaded
//     with `launchctl load -w`.
//   - Linux   : systemd --user unit under ~/.config/systemd/user, with
//     `loginctl enable-linger` so the daemon survives logout.
//   - Windows : Scheduled Task created via PowerShell Register-ScheduledTask,
//     principal Interactive/RunOnlyWhenLoggedOn.
//
// The package never touches the system service manager: every action
// happens inside the current user's writable directories and never
// requires root or SYSTEM.
package install

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
)

// ErrNotInstalled is returned by Status/Stop when no service definition
// for homepi-node is present. It lets cmd/homepi-node distinguish a
// pristine host from a real failure.
var ErrNotInstalled = errors.New("install: homepi-node service is not installed")

// ErrUnsupported is returned by Install on a build of homepi-node that
// was not compiled for the current GOOS.
var ErrUnsupported = errors.New("install: not supported on " + runtime.GOOS)

// Config carries everything the platform installers need.
type Config struct {
	// BinaryPath is the absolute path of the homepi-node executable.
	BinaryPath string
	// DataDir is the daemon's per-user data directory (the same one
	// serve.go computes via defaultDataDir).
	DataDir string
	// NodeLabel is the human-readable label written into the unit / plist
	// so the user can identify the service in launchctl/systemctl/etc.
	NodeLabel string
}

// Report is the result of a Status call.
type Report struct {
	// Installed is true when the unit file / plist / task is present.
	Installed bool
	// Running is true when the service manager reports the daemon as active.
	Running bool
	// Detail carries any platform-specific extra information (last
	// error, exit status, etc.) that the operator might want to see.
	Detail string
	// BinaryPath is the executable configured by the user service manager.
	// Callers must treat it as local diagnostic data and must not expose it
	// through browser or remote APIs.
	BinaryPath string
}

// UnitType is the platform-specific file type. macOS uses plist, Linux
// uses .service, Windows uses a task XML; the value here is what the
// installer writes so cmd/homepi-node can list the file generically.
type UnitType string

const (
	UnitPlist   UnitType = "plist"
	UnitSystemd UnitType = "systemd"
	UnitTask    UnitType = "scheduled-task"
)

// Install writes the service definition and enables it so a reboot or
// re-login starts the daemon automatically. Implementations are
// responsible for refusing to overwrite an existing definition unless the
// caller explicitly removed it first.
func Install(ctx context.Context, cfg Config) error {
	return platformInstall(ctx, cfg)
}

// Start launches the service via the platform service manager.
func Start(ctx context.Context) error {
	return platformStart(ctx)
}

// Stop terminates the running service.
func Stop(ctx context.Context) error {
	return platformStop(ctx)
}

// Status reports whether the service is installed and running.
func Status(ctx context.Context) (Report, error) {
	return platformStatus(ctx)
}

// Uninstall removes the service definition and stops it.
func Uninstall(ctx context.Context) error {
	return platformUninstall(ctx)
}

// fmtSnippet is a tiny helper used by the platform implementations.
func fmtSnippet(format string, args ...any) string {
	return fmt.Sprintf(format, args...)
}

func powershellExecutable(systemRoot string) string {
	if systemRoot == "" {
		return "powershell.exe"
	}
	return filepath.Join(systemRoot, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
}
