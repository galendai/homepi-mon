package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/galendai/homepi-mon/internal/buildinfo"
	"github.com/galendai/homepi-mon/internal/config"
	"github.com/galendai/homepi-mon/internal/install"
	"github.com/galendai/homepi-mon/internal/nodeapi"
	"github.com/galendai/homepi-mon/internal/webadmin"
)

type serviceHealth struct {
	Status string `json:"status"`
	Epoch  string `json:"epoch"`
	build  string
}

type nodeRuntime struct {
	source, digest string
	build          webadmin.BuildSnapshot
	status         func(context.Context) (install.Report, error)
	suspend        func(context.Context) error
	resume         func(context.Context) error
	health         func(context.Context) (serviceHealth, error)
	healthTimeout  time.Duration
}

func newNodeRuntime(configPath, dataDir string) *nodeRuntime {
	source, _ := os.Executable()
	digest, _ := executableDigest(source)
	return &nodeRuntime{
		source: source, digest: digest,
		build: webadmin.BuildSnapshot{Version: buildinfo.Version, Commit: buildinfo.Commit,
			Built: buildinfo.Date, Platform: buildinfo.Platform()},
		status: install.Status, suspend: install.SuspendForUpdate, resume: install.ResumeAfterUpdate,
		healthTimeout: 15 * time.Second,
		health: func(ctx context.Context) (serviceHealth, error) {
			return readServiceHealth(ctx, configPath, dataDir)
		},
	}
}

func readServiceHealth(ctx context.Context, configPath, dataDir string) (serviceHealth, error) {
	var health serviceHealth
	cfg, err := config.Load(configPath)
	if err != nil {
		return health, err
	}
	base, err := localControlURL(cfg.Listen.Addr, false)
	if err != nil {
		return health, err
	}
	client, err := remoteHTTPClient(cfg, dataDir, base, false)
	if err != nil {
		return health, err
	}
	defer client.CloseIdleConnections()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/healthz", nil)
	if err != nil {
		return health, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return health, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&health) != nil ||
		health.Status != "ok" || health.Epoch == "" {
		return health, errors.New("invalid node health response")
	}
	health.build = resp.Header.Get(nodeapi.HealthBuildHeader)
	return health, nil
}

func streamExecutable(path string, dest io.Writer) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return "", errors.New("expected a regular executable")
	}
	hash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(hash, dest), f); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func executableDigest(path string) (string, error) {
	return streamExecutable(path, io.Discard)
}

func stageExecutable(source, dest, expected string, mode os.FileMode) error {
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	digest, err := streamExecutable(source, f)
	if err == nil && digest != expected {
		err = errors.New("executable changed")
	}
	if err == nil {
		err = f.Chmod(mode.Perm())
	}
	if err == nil {
		err = f.Sync()
	}
	return errors.Join(err, f.Close())
}

// update uses only the running Admin and the registered user service. The
// private directory is both the cross-process lock and the recovery copy.
func (n *nodeRuntime) update(ctx context.Context, expected string) error {
	report, err := n.status(ctx)
	if err != nil || !report.Installed || !report.Running || !filepath.IsAbs(report.BinaryPath) || n.digest == "" {
		return errors.New("A running, installed local Node service is required to update.")
	}
	target, err := filepath.EvalSymlinks(report.BinaryPath)
	if err != nil {
		return errors.New("The installed service executable could not be located.")
	}
	stage := filepath.Join(filepath.Dir(target), "."+filepath.Base(target)+".update")
	if err := os.Mkdir(stage, 0o700); err != nil {
		if errors.Is(err, os.ErrExist) {
			return errors.New("An update is in progress or its recovery copy is still present. Check the service before retrying.")
		}
		return errors.New("The service executable directory is not writable by this user.")
	}
	keepRecovery := false
	defer func() {
		if !keepRecovery {
			_ = os.RemoveAll(stage)
		}
	}()

	current := n.snapshot(ctx)
	if current.State == "synced" {
		return nil
	}
	if current.UpdateRevision == "" || current.UpdateRevision != expected {
		return errors.New("The service changed since this page was loaded. Review the current versions before retrying.")
	}
	info, err := os.Stat(target)
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("The installed service executable could not be read.")
	}
	suffix := filepath.Ext(target)
	next, previous := filepath.Join(stage, "candidate"+suffix), filepath.Join(stage, "previous"+suffix)
	if stageExecutable(target, previous, expected, info.Mode()) != nil ||
		stageExecutable(n.source, next, n.digest, info.Mode()) != nil {
		return errors.New("The service or Admin executable changed or could not be copied. Reopen Web Admin and retry.")
	}
	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	candidate, err := inspectBuild(probeCtx, next)
	cancel()
	if err != nil || candidate != n.build {
		return errors.New("The staged executable did not match the running Web Admin build.")
	}
	before, err := n.health(ctx)
	if err != nil {
		return errors.New("The installed Node did not answer its local HTTPS health check. Check its connection before updating.")
	}
	latest, err := n.status(ctx)
	path, pathErr := filepath.EvalSymlinks(latest.BinaryPath)
	latestDigest, digestErr := executableDigest(target)
	if err != nil || !latest.Running || pathErr != nil || path != target || digestErr != nil || latestDigest != expected {
		return errors.New("The installed service changed while preparing the update. Review its current version and retry.")
	}

	replaced := false
	var failure string
	switch {
	case n.suspend(ctx) != nil:
		failure = "The service could not be stopped."
	case os.Rename(next, target) != nil:
		failure = "The service executable could not be replaced."
	default:
		replaced = true
		if n.resume(ctx) != nil {
			failure = "The updated service could not start."
		} else if n.waitHealthy(ctx, report.BinaryPath, before.Epoch, buildinfo.ID()) {
			return nil
		} else {
			failure = "The updated service failed its build or health check."
		}
	}

	recovery, cancel := context.WithTimeout(context.WithoutCancel(ctx), 25*time.Second)
	defer cancel()
	if replaced {
		installed, _ := executableDigest(target)
		backup, _ := executableDigest(previous)
		if installed != n.digest || backup != expected || n.suspend(recovery) != nil || os.Rename(previous, target) != nil {
			keepRecovery = true
			return errors.New("Update failed and the previous executable could not be restored. Check the recovery copy in the service update directory.")
		}
	}
	// With the original file back in place, an already-running original
	// process is also healthy when suspension failed before replacement.
	previousEpoch := ""
	if replaced {
		previousEpoch = before.Epoch
	}
	_ = n.resume(recovery)
	if !n.waitHealthy(recovery, report.BinaryPath, previousEpoch, before.build) {
		return errors.New("Update failed. The previous executable is in place, but its service health could not be confirmed. Check the local Node service.")
	}
	return errors.New(failure + " The previous build is running and healthy. You can retry the update.")
}

func (n *nodeRuntime) waitHealthy(ctx context.Context, registered, previousEpoch, expectedBuild string) bool {
	ctx, cancel := context.WithTimeout(ctx, n.healthTimeout)
	defer cancel()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		report, err := n.status(ctx)
		if err == nil && report.Running && report.BinaryPath == registered {
			if h, err := n.health(ctx); err == nil && h.Epoch != "" && h.Epoch != previousEpoch && h.build == expectedBuild {
				return true
			}
		}
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
		}
	}
}
