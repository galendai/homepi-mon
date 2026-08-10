package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"

	"github.com/galendai/homepi-mon/internal/install"
)

// runService dispatches install/start/stop/status/uninstall to the
// internal/install package. The implementations themselves are
// platform-specific and live in install_<goos>.go files.
func runService(args []string) error {
	if len(args) == 0 {
		return errors.New("expected install|start|stop|status|uninstall")
	}
	ctx := context.Background()

	dataDir, err := defaultDataDir()
	if err != nil {
		return err
	}
	binary, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate homepi-node binary: %w", err)
	}
	cfg := install.Config{
		BinaryPath: binary,
		DataDir:    dataDir,
		NodeLabel:  fmt.Sprintf("homepi-node (%s)", runtime.GOOS),
	}

	switch args[0] {
	case "install":
		return install.Install(ctx, cfg)
	case "start":
		return install.Start(ctx)
	case "stop":
		return install.Stop(ctx)
	case "status":
		rep, err := install.Status(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("service: installed=%t running=%t detail=%s\n",
			rep.Installed, rep.Running, rep.Detail)
		return nil
	case "uninstall":
		if err := install.Stop(ctx); err != nil && !errors.Is(err, install.ErrNotInstalled) {
			return err
		}
		return install.Uninstall(ctx)
	}
	return fmt.Errorf("service: unknown subcommand %q", args[0])
}
