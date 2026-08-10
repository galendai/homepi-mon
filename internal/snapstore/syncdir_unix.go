//go:build unix

package snapstore

import "os"

// syncDir persists the rename's directory entry on the Linux kiosk and other
// Unix development targets. Syncing only the file does not make the rename
// durable across a sudden power loss.
func syncDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
