//go:build darwin

package install

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// launchctlDomain is the per-user LaunchAgent domain that launchctl uses
// when addressing the agent by basename.
const launchctlDomain = "gui/%d"

// plistLabel is the reverse-DNS label the LaunchAgent registers under.
// Reversing the import path keeps it unique without inventing a UUID.
const plistLabel = "com.galendai.homepi-node"

// plistTemplate is the LaunchAgent definition. RunAtLoad makes the
// daemon start as soon as `launchctl load -w` finishes; KeepAlive ties
// the agent's lifetime to the user's session.
const plistTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>serve</string>
  </array>
  <key>EnvironmentVariables</key>
  <dict>
    <key>HOMEPI_NODE_DATA_DIR</key>
    <string>%s</string>
  </dict>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <dict>
    <key>SuccessfulExit</key>
    <false/>
  </dict>
  <key>ProcessType</key>
  <string>Background</string>
  <key>StandardOutPath</key>
  <string>%s/homepi-node.out.log</string>
  <key>StandardErrorPath</key>
  <string>%s/homepi-node.err.log</string>
</dict>
</plist>
`

func platformInstall(ctx context.Context, cfg Config) error {
	dir, err := launchAgentsDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("install: mkdir %s: %w", dir, err)
	}
	path := filepath.Join(dir, plistLabel+".plist")
	body := fmt.Sprintf(plistTemplate,
		plistLabel, cfg.BinaryPath, cfg.DataDir, cfg.DataDir, cfg.DataDir)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return fmt.Errorf("install: write %s: %w", path, err)
	}
	cmd := exec.CommandContext(ctx, "launchctl", "load", "-w", path)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("install: launchctl load: %w", err)
	}
	return nil
}

func platformStart(ctx context.Context) error {
	uid, err := currentUID()
	if err != nil {
		return err
	}
	target := fmt.Sprintf("%s/%s", fmt.Sprintf(launchctlDomain, uid), plistLabel)
	cmd := exec.CommandContext(ctx, "launchctl", "kickstart", "-k", target)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("install: launchctl kickstart: %w", err)
	}
	return nil
}

func platformStop(ctx context.Context) error {
	uid, err := currentUID()
	if err != nil {
		return err
	}
	target := fmt.Sprintf("%s/%s", fmt.Sprintf(launchctlDomain, uid), plistLabel)
	cmd := exec.CommandContext(ctx, "launchctl", "kill", "SIGTERM", target)
	if out, err := cmd.CombinedOutput(); err != nil {
		// "kill" returns non-zero when the agent is not loaded, which we
		// treat as a successful no-op.
		if strings.Contains(string(out), "Could not find") ||
			strings.Contains(string(out), "No process to signal") {
			return nil
		}
		return fmt.Errorf("install: launchctl kill: %w: %s", err, out)
	}
	return nil
}

func platformStatus(ctx context.Context) (Report, error) {
	path, err := plistPath()
	if err != nil {
		return Report{}, err
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return Report{Installed: false}, nil
		}
		return Report{}, err
	}
	uid, err := currentUID()
	if err != nil {
		return Report{Installed: true}, err
	}
	target := fmt.Sprintf("%s/%s", fmt.Sprintf(launchctlDomain, uid), plistLabel)
	cmd := exec.CommandContext(ctx, "launchctl", "print", target)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		// A failed print usually means the agent is not loaded, which we
		// surface as "installed but not running".
		return Report{Installed: true, Running: false, Detail: out.String()}, nil
	}
	running := strings.Contains(out.String(), "state = running")
	return Report{Installed: true, Running: running, Detail: out.String()}, nil
}

func platformUninstall(ctx context.Context) error {
	path, err := plistPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return ErrNotInstalled
		}
		return err
	}
	cmd := exec.CommandContext(ctx, "launchctl", "unload", path)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("install: launchctl unload: %w", err)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("install: remove plist: %w", err)
	}
	return nil
}

func launchAgentsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("install: locate HOME: %w", err)
	}
	return filepath.Join(home, "Library", "LaunchAgents"), nil
}

func plistPath() (string, error) {
	dir, err := launchAgentsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, plistLabel+".plist"), nil
}

func currentUID() (int, error) {
	out, err := exec.Command("id", "-u").Output()
	if err != nil {
		return 0, fmt.Errorf("install: id -u: %w", err)
	}
	var uid int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(out)), "%d", &uid); err != nil {
		return 0, fmt.Errorf("install: parse uid: %w", err)
	}
	return uid, nil
}
