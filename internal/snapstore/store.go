// Package snapstore persists the single last-known-good snapshot on the
// display device.
//
// MOD-002 7 allows exactly one file: no history, no SQLite, no rolling
// samples. Writes go through a temp file plus fsync plus atomic rename, so a
// power cut during a write can never leave a torn snapshot behind, and a
// corrupt file is quarantined rather than crashing the kiosk.
package snapstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/galendai/homepi-mon/internal/protocol"
)

// FileName is the only snapshot file the display keeps.
const FileName = "last-known-good.json"

// quarantineName holds the most recent unreadable snapshot for diagnosis.
// It is a single fixed name, so a repeated failure cannot fill the SD card.
const quarantineName = "last-known-good.corrupt"

// MaxBytes bounds what the store will read or write. HL-Spec 9 targets a
// snapshot of 256 KB; four times that is a generous ceiling that still stops a
// malformed or hostile payload from exhausting the Pi's memory.
const MaxBytes = 1 << 20

// ErrNoSnapshot means no usable snapshot exists yet.
var ErrNoSnapshot = errors.New("snapstore: no snapshot")

// Store reads and writes the last-known-good snapshot in one directory.
type Store struct {
	dir     string
	binding protocol.SourceBinding
}

// New builds a Store rooted at dir, creating the directory if needed.
// binding pins the store to a single source node (ADR-014).
func New(dir string, binding protocol.SourceBinding) (*Store, error) {
	if dir == "" {
		return nil, errors.New("snapstore: dir is required")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("snapstore: create dir: %w", err)
	}
	return &Store{dir: dir, binding: binding}, nil
}

// Path is the snapshot file location.
func (s *Store) Path() string { return filepath.Join(s.dir, FileName) }

// Load returns the stored snapshot.
//
// A missing file returns ErrNoSnapshot. A corrupt, schema-incompatible or
// wrong-source file is moved aside and ErrNoSnapshot is returned with the
// quarantine reason wrapped, so the caller shows the empty state instead of
// exiting (MOD-002 7).
func (s *Store) Load() (*protocol.MetricSnapshot, error) {
	raw, err := s.readBounded(s.Path())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoSnapshot
		}
		return nil, fmt.Errorf("snapstore: read: %w", err)
	}

	snap, err := protocol.DecodeSnapshot(raw)
	if err != nil {
		return nil, s.quarantine(fmt.Errorf("unreadable snapshot: %w", err))
	}
	if err := s.binding.Accept(snap); err != nil {
		return nil, s.quarantine(err)
	}
	return snap, nil
}

// Save atomically replaces the stored snapshot.
//
// The snapshot is validated and source-checked first, so an invalid payload can
// never overwrite a good one.
func (s *Store) Save(snap *protocol.MetricSnapshot) error {
	if err := snap.Validate(); err != nil {
		return fmt.Errorf("snapstore: refusing to save invalid snapshot: %w", err)
	}
	if err := s.binding.Accept(snap); err != nil {
		return fmt.Errorf("snapstore: %w", err)
	}

	raw, err := json.Marshal(snap)
	if err != nil {
		return fmt.Errorf("snapstore: marshal: %w", err)
	}
	if len(raw) > MaxBytes {
		return fmt.Errorf("snapstore: snapshot is %d bytes, over the %d byte limit", len(raw), MaxBytes)
	}

	tmp, err := os.CreateTemp(s.dir, FileName+".tmp-*")
	if err != nil {
		return fmt.Errorf("snapstore: temp file: %w", err)
	}
	tmpName := tmp.Name()
	// Any failure below must not leave the temp file behind.
	defer os.Remove(tmpName)

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("snapstore: chmod: %w", err)
	}
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return fmt.Errorf("snapstore: write: %w", err)
	}
	// fsync before rename: without it a crash can leave a renamed but empty
	// file, which is exactly the torn state the atomic replace is meant to
	// prevent.
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("snapstore: sync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("snapstore: close: %w", err)
	}
	if err := os.Rename(tmpName, s.Path()); err != nil {
		return fmt.Errorf("snapstore: rename: %w", err)
	}
	if err := syncDir(s.dir); err != nil {
		return fmt.Errorf("snapstore: sync directory: %w", err)
	}
	return nil
}

// Files lists the entries in the store directory, used by tests and by doctor
// to prove no metric history accumulates (MOD-002 11).
func (s *Store) Files() ([]string, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names, nil
}

func (s *Store) readBounded(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > MaxBytes {
		return nil, fmt.Errorf("snapshot file is %d bytes, over the %d byte limit", info.Size(), MaxBytes)
	}
	buf := make([]byte, info.Size())
	if _, err := f.Read(buf); err != nil && info.Size() > 0 {
		return nil, err
	}
	return buf, nil
}

// quarantine moves the bad file aside and returns ErrNoSnapshot wrapping why.
func (s *Store) quarantine(reason error) error {
	dst := filepath.Join(s.dir, quarantineName)
	if err := os.Rename(s.Path(), dst); err != nil {
		// If it cannot be moved, remove it: leaving it in place would make the
		// display fail identically on every boot.
		_ = os.Remove(s.Path())
	}
	return fmt.Errorf("%w (quarantined: %v)", ErrNoSnapshot, reason)
}
