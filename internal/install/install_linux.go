//go:build linux

package install

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// unitLabel is the basename used for both the unit file and the systemd
// unit name. The reverse-DNS prefix matches the macOS label so a single
// dashboard identifies the daemon on every platform.
const unitLabel = "homepi-node.service"

// unitTemplate is the per-user systemd unit. The user slice ensures the
// unit runs without root and inherits the user's environment.
const unitTemplate = `[Unit]
Description=HomePi Monitor daemon (%s)
After=network-online.target

[Service]
Type=simple
ExecStart=%s serve
Environment=HOMEPI_NODE_DATA_DIR=%s
Restart=on-failure
RestartSec=10

[Install]
WantedBy=default.target
`

func platformInstall(ctx context.Context, cfg Config) error {
	dir, err := userSystemdDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("install: mkdir %s: %w", dir, err)
	}
	path := filepath.Join(dir, unitLabel)
	body := fmt.Sprintf(unitTemplate, cfg.NodeLabel, cfg.BinaryPath, cfg.DataDir)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return fmt.Errorf("install: write unit: %w", err)
	}
	// Best effort: loginctl may require polkit or root on some distros,
	// but on most it succeeds silently when the user already has a
	// session.
	_ = exec.CommandContext(ctx, "loginctl", "enable-linger").Run()
	cmd := exec.CommandContext(ctx, "systemctl", "--user", "enable", "--now", unitLabel)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("install: systemctl enable --now: %w", err)
	}
	return nil
}

func platformStart(ctx context.Context) error {
	return runSystemctl(ctx, "start")
}

func platformStop(ctx context.Context) error {
	if err := runSystemctl(ctx, "stop"); err != nil {
		// systemctl stop exits non-zero when the unit is not loaded,
		// which is a normal state for a freshly installed daemon before
		// the first start. Treat it as a successful no-op.
		return nil
	}
	return nil
}

func platformStatus(ctx context.Context) (Report, error) {
	path, err := unitPath()
	if err != nil {
		return Report{}, err
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return Report{Installed: false}, nil
		}
		return Report{}, err
	}
	binaryPath, _ := binaryPathFromSystemdUnit(path)
	cmd := exec.CommandContext(ctx, "systemctl", "--user", "is-active", unitLabel)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err = cmd.Run()
	running := strings.TrimSpace(out.String()) == "active"
	if err != nil {
		return Report{Installed: true, Running: false, Detail: out.String(), BinaryPath: binaryPath}, nil
	}
	return Report{Installed: true, Running: running, Detail: out.String(), BinaryPath: binaryPath}, nil
}

func binaryPathFromSystemdUnit(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		value, ok := strings.CutPrefix(strings.TrimSpace(line), "ExecStart=")
		if !ok {
			continue
		}
		value = strings.TrimSpace(strings.TrimSuffix(value, " serve"))
		value = strings.Trim(value, `"`)
		if value == "" {
			break
		}
		return value, nil
	}
	return "", errors.New("install: systemd unit has no ExecStart executable")
}

func platformUninstall(ctx context.Context) error {
	path, err := unitPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return ErrNotInstalled
		}
		return err
	}
	_ = runSystemctl(ctx, "disable")
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("install: remove unit: %w", err)
	}
	return nil
}

func runSystemctl(ctx context.Context, action string) error {
	cmd := exec.CommandContext(ctx, "systemctl", "--user", action, unitLabel)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("install: systemctl %s: %w", action, err)
	}
	return nil
}

func userSystemdDir() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("install: locate HOME: %w", err)
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "systemd", "user"), nil
}

func unitPath() (string, error) {
	dir, err := userSystemdDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, unitLabel), nil
}
