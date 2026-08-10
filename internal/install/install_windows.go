//go:build windows

package install

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// taskName is the basename shown by `Get-ScheduledTask`.
const taskName = "HomePi Monitor"

// powershellPath expands SystemRoot before exec.CommandContext, which does
// not perform shell-style environment expansion.
func powershellPath() string {
	return powershellExecutable(os.Getenv("SystemRoot"))
}

// taskScript returns the PowerShell snippet that creates the scheduled
// task. It is inlined rather than stored as a file so the binary stays
// self-contained.
func taskScript(cfg Config) string {
	return fmt.Sprintf(`
$ErrorActionPreference = "Stop"
$taskName = "%s"
$existing = Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
if ($existing) {
  Unregister-ScheduledTask -TaskName $taskName -Confirm:$false
}
$action = New-ScheduledTaskAction -Execute "%s" -Argument "serve"
$trigger = New-ScheduledTaskTrigger -AtLogOn
$principal = New-ScheduledTaskPrincipal -User "$env:USERNAME" -LogonType Interactive -RunLevel Limited
$settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -RestartCount 3 -RestartInterval (New-TimeSpan -Minutes 1)
Register-ScheduledTask -TaskName $taskName -Action $action -Trigger $trigger -Principal $principal -Settings $settings -Description "%s"
`, taskName, strings.ReplaceAll(cfg.BinaryPath, `"`, `\"`), cfg.NodeLabel)
}

func platformInstall(ctx context.Context, cfg Config) error {
	cmd := exec.CommandContext(ctx, powershellPath(), "-NoProfile", "-NonInteractive",
		"-Command", taskScript(cfg))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("install: register scheduled task: %w", err)
	}
	return nil
}

func platformStart(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, powershellPath(), "-NoProfile", "-NonInteractive",
		"-Command", fmt.Sprintf(`Start-ScheduledTask -TaskName "%s"`, taskName))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("install: start scheduled task: %w", err)
	}
	return nil
}

func platformStop(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, powershellPath(), "-NoProfile", "-NonInteractive",
		"-Command", fmt.Sprintf(`Stop-ScheduledTask -TaskName "%s"`, taskName))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("install: stop scheduled task: %w", err)
	}
	return nil
}

func platformStatus(ctx context.Context) (Report, error) {
	cmd := exec.CommandContext(ctx, powershellPath(), "-NoProfile", "-NonInteractive",
		"-Command",
		fmt.Sprintf(`$t = Get-ScheduledTask -TaskName "%s" -ErrorAction SilentlyContinue
if ($null -eq $t) { exit 1 }
$info = (Get-ScheduledTask -TaskName "%s").State
Write-Output $info`, taskName, taskName))
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	if err != nil {
		return Report{Installed: false, Detail: out.String()}, nil
	}
	state := strings.TrimSpace(out.String())
	return Report{
		Installed: true,
		Running:   strings.EqualFold(state, "Running"),
		Detail:    state,
	}, nil
}

func platformUninstall(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, powershellPath(), "-NoProfile", "-NonInteractive",
		"-Command",
		fmt.Sprintf(`$t = Get-ScheduledTask -TaskName "%s" -ErrorAction SilentlyContinue
if ($null -eq $t) { exit 1 }
Unregister-ScheduledTask -TaskName "%s" -Confirm:$false`, taskName, taskName))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("install: unregister scheduled task: %w", err)
	}
	return nil
}
