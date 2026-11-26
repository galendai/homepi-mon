package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/galendai/homepi-mon/internal/buildinfo"
	"github.com/galendai/homepi-mon/internal/install"
	"github.com/galendai/homepi-mon/internal/webadmin"
)

type updateFixture struct {
	*nodeRuntime
	target, oldDigest string
	running, newBuild bool
	starts, stops     int
}

func runtimeFixture(t *testing.T, sameLabel bool) *updateFixture {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fixture version commands use POSIX scripts")
	}
	dir := t.TempDir()
	source, target := filepath.Join(dir, "admin"), filepath.Join(dir, "homepi-node")
	build := webadmin.BuildSnapshot{Version: "1.0", Commit: "new", Built: "now", Platform: buildinfo.Platform()}
	script := "#!/bin/sh\nprintf '%s\\n' 'homepi-node 1.0' 'commit: new' 'built: now' 'platform: " + build.Platform + "'\n"
	old := script + "# different executable\n"
	if !sameLabel {
		old = strings.Replace(old, "commit: new", "commit: old", 1)
	}
	for p, contents := range map[string]string{source: script, target: old} {
		if err := os.WriteFile(p, []byte(contents), 0o750); err != nil {
			t.Fatal(err)
		}
		// Let macOS perform its first execution check on each generated
		// fixture before testing the updater's bounded version probe.
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		_, err := inspectBuild(ctx, p)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
	}
	digest, _ := executableDigest(source)
	oldDigest, _ := executableDigest(target)
	n := &nodeRuntime{source: source, digest: digest, build: build, healthTimeout: 50 * time.Millisecond}
	f := &updateFixture{nodeRuntime: n, target: target, oldDigest: oldDigest, running: true}
	n.status = func(context.Context) (install.Report, error) {
		return install.Report{Installed: true, Running: f.running, BinaryPath: target}, nil
	}
	n.suspend = func(context.Context) error { f.stops++; f.running = false; return nil }
	n.resume = func(context.Context) error {
		sum, err := executableDigest(target)
		f.newBuild = sum == n.digest
		f.starts++
		f.running = err == nil
		return err
	}
	n.health = func(context.Context) (serviceHealth, error) {
		if !f.running {
			return serviceHealth{}, errors.New("stopped")
		}
		h := serviceHealth{Status: "ok", Epoch: fmt.Sprint(f.starts)}
		if f.newBuild {
			h.build = buildinfo.ID()
		}
		return h, nil
	}
	return f
}

func TestUpdateInstallsRunningAdminAndIsIdempotent(t *testing.T) {
	for _, sameLabel := range []bool{false, true} {
		f := runtimeFixture(t, sameLabel)
		before := f.snapshot(t.Context())
		if before.UpdateRevision != f.oldDigest {
			t.Fatalf("missing update action: %+v", before)
		}
		for i := 0; i < 2; i++ {
			if err := f.update(t.Context(), before.UpdateRevision); err != nil {
				t.Fatal(err)
			}
		}
		after := f.snapshot(t.Context())
		if after.State != "synced" || after.UpdateRevision != "" || !f.newBuild || f.starts != 1 || f.stops != 1 {
			t.Fatalf("after=%+v starts=%d stops=%d", after, f.starts, f.stops)
		}
		if info, err := os.Stat(f.target); err != nil || info.Mode().Perm() != 0o750 {
			t.Fatalf("installed file permissions %v %v", info, err)
		}
	}
}

func TestUpdateRejectsChangesBeforeStopping(t *testing.T) {
	for _, scenario := range []string{"new revision", "changed admin", "locked", "offline"} {
		t.Run(scenario, func(t *testing.T) {
			f := runtimeFixture(t, false)
			var err error
			switch scenario {
			case "new revision":
				f.oldDigest = strings.Repeat("0", 64)
			case "changed admin":
				err = os.WriteFile(f.source, []byte("different"), 0o750)
			case "locked":
				err = os.Mkdir(filepath.Join(filepath.Dir(f.target), ".homepi-node.update"), 0o700)
			case "offline":
				f.health = func(context.Context) (serviceHealth, error) { return serviceHealth{}, errors.New("offline") }
			}
			if err != nil {
				t.Fatal(err)
			}
			want, _ := executableDigest(f.target)
			updateErr := f.update(t.Context(), f.oldDigest)
			got, _ := executableDigest(f.target)
			if updateErr == nil || got != want || f.stops != 0 || !f.running {
				t.Fatalf("err=%v stopped=%d digest=%s", updateErr, f.stops, got)
			}
		})
	}
}

func TestUpdateChecksNewProcessAndRecoversOldService(t *testing.T) {
	for _, scenario := range []string{"old epoch", "wrong build", "start failed", "recovery failed"} {
		t.Run(scenario, func(t *testing.T) {
			f := runtimeFixture(t, false)
			probe, start, stop := f.health, f.resume, f.suspend
			f.health = func(ctx context.Context) (serviceHealth, error) {
				h, err := probe(ctx)
				if f.newBuild {
					if scenario == "old epoch" {
						h.Epoch = "0"
					} else {
						h.build = "other build"
					}
				}
				return h, err
			}
			f.resume = func(ctx context.Context) error {
				err := start(ctx)
				if f.newBuild && scenario == "start failed" {
					return errors.New("could not start")
				}
				return err
			}
			f.suspend = func(ctx context.Context) error {
				if f.newBuild && scenario == "recovery failed" {
					return errors.New("busy")
				}
				return stop(ctx)
			}
			err := f.update(t.Context(), f.oldDigest)
			if err == nil {
				t.Fatal("unverified update reported success")
			}
			if scenario == "recovery failed" {
				backup := filepath.Join(filepath.Dir(f.target), ".homepi-node.update", "previous")
				if sum, _ := executableDigest(backup); sum != f.oldDigest || !strings.Contains(err.Error(), "recovery copy") {
					t.Fatalf("lost recovery copy: %v", err)
				}
				return
			}
			sum, _ := executableDigest(f.target)
			if sum != f.oldDigest || f.newBuild || !f.running || f.starts != 2 || !strings.Contains(err.Error(), "previous build is running and healthy") {
				t.Fatalf("recovery failed: sum=%s starts=%d err=%v", sum, f.starts, err)
			}
		})
	}
}
