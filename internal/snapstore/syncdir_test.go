package snapstore

import "testing"

// U-N008 (Test-Module-002 U020): the platform implementation can durably sync
// the snapshot directory used by Save after an atomic rename.
func TestSyncDir(t *testing.T) {
	if err := syncDir(t.TempDir()); err != nil {
		t.Fatalf("sync directory: %v", err)
	}
}
