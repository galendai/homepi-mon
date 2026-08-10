package install

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPowerShellExecutableExpandsSystemRoot(t *testing.T) {
	root := filepath.Join("C:", "Windows")
	got := powershellExecutable(root)
	want := filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	if got != want {
		t.Fatalf("powershell executable = %q, want %q", got, want)
	}
	if strings.Contains(got, "%SystemRoot%") {
		t.Fatalf("powershell executable was not expanded: %q", got)
	}
	if got := powershellExecutable(""); got != "powershell.exe" {
		t.Fatalf("empty SystemRoot fallback = %q", got)
	}
}
