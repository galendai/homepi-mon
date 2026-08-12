//go:build darwin

package install

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestBinaryPathFromPlistReadsServiceExecutable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "homepi-node.plist")
	want := filepath.Join(dir, "homepi-node")
	body := fmt.Sprintf(plistTemplate, plistLabel, want, dir, dir, dir)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := binaryPathFromPlist(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("binary path = %q, want %q", got, want)
	}
}

func TestPlatformStopRunsLaunchctlWithoutCombinedOutputConflict(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	script := filepath.Join(dir, "launchctl")
	body := "#!/bin/sh\nprintf '%s' \"$*\" > \"$HOMEPI_TEST_ARGS\"\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOMEPI_TEST_ARGS", argsPath)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := platformStop(context.Background()); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 {
		t.Fatal("fake launchctl was not executed")
	}
}

func TestPlatformStopTreatsMissingAgentAsNoop(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "launchctl")
	body := "#!/bin/sh\necho 'Could not find service' >&2\nexit 1\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := platformStop(context.Background()); err != nil {
		t.Fatalf("missing LaunchAgent should be a no-op: %v", err)
	}
}

func TestPlatformStopTreatsAlreadyStoppedAgentAsNoop(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "launchctl")
	body := "#!/bin/sh\necho 'No process to signal.' >&2\nexit 3\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := platformStop(context.Background()); err != nil {
		t.Fatalf("stopped LaunchAgent should be a no-op: %v", err)
	}
}
