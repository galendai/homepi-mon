package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/galendai/homepi-mon/internal/buildinfo"
	"github.com/galendai/homepi-mon/internal/config"
	"github.com/galendai/homepi-mon/internal/configtx"
	"github.com/galendai/homepi-mon/internal/displaydeploy"
	"github.com/galendai/homepi-mon/internal/install"
	"github.com/galendai/homepi-mon/internal/secretstore"
	"github.com/galendai/homepi-mon/internal/webadmin"
)

// runConfigure starts the loopback Web Admin. The command is the
// single entry point to the Phase 2 configuration flow. Startup only
// loads the baseline and probes the configured secret backend; explicit
// Apply performs the real service transaction.
func runConfigure(args []string) error {
	fs := flag.NewFlagSet("configure", flag.ContinueOnError)
	path := fs.String("config", config.DefaultPath(),
		"path to the config.json the Web Admin will edit")
	addr := fs.String("addr", "",
		"optional loopback bind address (e.g. 127.0.0.1:8765); empty selects a random port")
	noBrowser := fs.Bool("no-browser", false,
		"do not attempt to open the default browser")
	idle := fs.Duration("idle-timeout", time.Hour,
		"close the listener after this duration of no requests")
	if err := fs.Parse(args); err != nil {
		return err
	}
	secretDir := os.Getenv("HOMEPI_SECRET_DIR")
	if secretDir == "" {
		base, err := defaultDataDir()
		if err != nil {
			return err
		}
		secretDir = base + "/secrets"
	}
	dataDir := os.Getenv("HOMEPI_DATA_DIR")
	if dataDir == "" {
		base, err := defaultDataDir()
		if err != nil {
			return err
		}
		dataDir = base
	}
	svc, err := configtx.New(context.Background(), configtx.Options{
		ConfigPath:   *path,
		SecretDir:    secretDir,
		DataDir:      dataDir,
		ServiceLabel: binaryName,
		Now:          time.Now,
	})
	if err != nil {
		return err
	}
	secrets, err := secretstore.Open(context.Background(), secretstore.Options{FileDir: secretDir, DataDir: dataDir})
	if err != nil {
		return err
	}
	displayManager, err := displaydeploy.New(*path, dataDir, secrets, nil)
	if err != nil {
		return err
	}
	cfg := webadmin.Config{
		Service:     svc,
		Addr:        *addr,
		IdleTimeout: *idle,
		Logger:      newLogger(true),
		Restart:     restartNodeService,
		HealthCheck: func(ctx context.Context) error {
			return waitForNodeService(ctx, *path)
		},
		DisplayManager: displayManager,
		RuntimeStatus:  detectRuntimeStatus,
	}
	if !*noBrowser {
		cfg.OpenBrowser = openBrowser
	}
	srv, err := webadmin.New(cfg)
	if err != nil {
		return err
	}
	bound, err := srv.Listen()
	if err != nil {
		return err
	}
	fmt.Printf("HomePi Web Admin: %s\n", urlFor(bound))
	fmt.Println("Press Ctrl-C to stop.")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return srv.Run(ctx)
}

func detectRuntimeStatus(ctx context.Context) webadmin.RuntimeSnapshot {
	admin := webadmin.BuildSnapshot{
		Version:  buildinfo.Version,
		Commit:   buildinfo.Commit,
		Built:    buildinfo.Date,
		Platform: buildinfo.Platform(),
	}
	result := webadmin.RuntimeSnapshot{Admin: admin}
	report, err := install.Status(ctx)
	if err != nil {
		result.State = "unavailable"
		result.Message = "The installed service version could not be checked."
		return result
	}
	result.ServiceInstalled = report.Installed
	result.ServiceRunning = report.Running
	if !report.Installed {
		result.State = "not_installed"
		result.Message = "The HomePi user service is not installed."
		return result
	}

	if report.BinaryPath != "" {
		versionCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		service, inspectErr := inspectBuild(versionCtx, report.BinaryPath)
		cancel()
		if inspectErr == nil {
			result.Service = service
		}
	}
	if !report.Running {
		result.State = "service_stopped"
		result.Message = "The HomePi user service is installed but not running."
		return result
	}
	if result.Service.Version == "" || result.Service.Commit == "" {
		result.State = "unavailable"
		result.Message = "The installed service version could not be checked."
		return result
	}

	currentExecutable, _ := os.Executable()
	sameBinary := sameExecutable(currentExecutable, report.BinaryPath)
	return classifyRuntime(admin, result.Service, sameBinary, result)
}

func inspectBuild(ctx context.Context, binaryPath string) (webadmin.BuildSnapshot, error) {
	if binaryPath == "" || !filepath.IsAbs(binaryPath) {
		return webadmin.BuildSnapshot{}, errors.New("invalid service executable")
	}
	raw, err := exec.CommandContext(ctx, binaryPath, "--version").Output()
	if err != nil {
		return webadmin.BuildSnapshot{}, errors.New("service version command failed")
	}
	if len(raw) > 16*1024 {
		return webadmin.BuildSnapshot{}, errors.New("service version output is too large")
	}
	return parseBuild(raw)
}

func parseBuild(raw []byte) (webadmin.BuildSnapshot, error) {
	var result webadmin.BuildSnapshot
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) == 0 {
		return result, errors.New("empty service version output")
	}
	heading := strings.Fields(lines[0])
	if len(heading) != 2 || heading[0] != binaryName {
		return result, errors.New("invalid service version heading")
	}
	result.Version = safeBuildField(heading[1])
	for _, line := range lines[1:] {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = safeBuildField(strings.TrimSpace(value))
		switch strings.TrimSpace(key) {
		case "commit":
			result.Commit = value
		case "built":
			result.Built = value
		case "platform":
			result.Platform = value
		}
	}
	if result.Version == "" || result.Commit == "" {
		return webadmin.BuildSnapshot{}, errors.New("incomplete service version output")
	}
	return result, nil
}

func safeBuildField(value string) string {
	if len(value) > 128 {
		return ""
	}
	for _, r := range value {
		if r < 0x20 || r > 0x7e {
			return ""
		}
	}
	return value
}

func sameExecutable(left, right string) bool {
	if left == "" || right == "" {
		return false
	}
	leftInfo, leftErr := os.Stat(left)
	rightInfo, rightErr := os.Stat(right)
	if leftErr == nil && rightErr == nil {
		return os.SameFile(leftInfo, rightInfo)
	}
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	return leftErr == nil && rightErr == nil && filepath.Clean(leftAbs) == filepath.Clean(rightAbs)
}

func classifyRuntime(admin, service webadmin.BuildSnapshot, sameBinary bool,
	result webadmin.RuntimeSnapshot) webadmin.RuntimeSnapshot {
	buildDateMismatch := admin.Built != "" && admin.Built != "unknown" &&
		service.Built != "" && service.Built != "unknown" && admin.Built != service.Built
	if admin.Version != service.Version || admin.Commit != service.Commit || buildDateMismatch {
		result.State = "version_mismatch"
		result.Message = "The installed service uses a different build. Install the current build before Apply."
		return result
	}
	if !sameBinary {
		result.State = "different_binary"
		result.Message = "Web Admin was started from a different binary than the installed service."
		return result
	}
	result.State = "synced"
	result.Message = "Web Admin and the installed service use the same build."
	return result
}

func restartNodeService(ctx context.Context) error {
	report, err := install.Status(ctx)
	if err != nil {
		return fmt.Errorf("service status before restart: %w", err)
	}
	if !report.Installed {
		return errors.New("homepi-node user service is not installed")
	}
	if err := install.Stop(ctx); err != nil {
		return fmt.Errorf("stop homepi-node service: %w", err)
	}
	if err := install.Start(ctx); err != nil {
		return fmt.Errorf("start homepi-node service: %w", err)
	}
	return nil
}

func waitForNodeService(ctx context.Context, configPath string) error {
	if _, err := config.Load(configPath); err != nil {
		return fmt.Errorf("reload applied config: %w", err)
	}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		report, err := install.Status(ctx)
		if err == nil && report.Installed && report.Running {
			return nil
		}
		select {
		case <-ctx.Done():
			if err != nil {
				return fmt.Errorf("service health check: %w", err)
			}
			return fmt.Errorf("service health check: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func urlFor(addr fmt.Stringer) string {
	return "http://" + addr.String() + "/"
}

// openBrowser tries to launch the operator's default browser on the
// given URL. It only attempts best-effort invocations; the Web Admin
// works without the auto-open.
func openBrowser(target string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", target)
	case "linux":
		cmd = exec.Command("xdg-open", target)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		return
	}
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		// Best-effort: failure here is non-fatal; the operator
		// can copy the URL from stdout.
		return
	}
	_ = cmd.Process.Release()
}
