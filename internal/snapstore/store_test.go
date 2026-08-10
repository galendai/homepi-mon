package snapstore_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/galendai/homepi-mon/internal/decimal"
	"github.com/galendai/homepi-mon/internal/protocol"
	"github.com/galendai/homepi-mon/internal/snapstore"
)

func snapshot(version uint64, node string) *protocol.MetricSnapshot {
	v := decimal.MustParse("68")
	l := decimal.MustParse("100")
	now := time.Date(2026, 8, 10, 6, 32, 0, 0, time.UTC)
	return &protocol.MetricSnapshot{
		SchemaVersion:   protocol.SchemaVersion,
		SourceEpoch:     "epoch-a",
		SnapshotVersion: version,
		GeneratedAt:     now,
		SourceNode:      node,
		SourceNodeLabel: strings.ToUpper(node),
		Metrics: []protocol.ProviderMetric{{
			ID: "m1", Provider: "minimax", DisplayName: "MiniMax",
			MetricKind: protocol.KindQuota, Value: &v, Limit: &l,
			Unit: "percent", Window: protocol.WindowRolling5h,
			ObservedAt: now, Precision: protocol.PrecisionExact,
			SourceKind: protocol.SourceMock, Status: protocol.StatusOK,
		}},
	}
}

func newStore(t *testing.T) (*snapstore.Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := snapstore.New(dir, protocol.SourceBinding{NodeID: "dev-mac"})
	if err != nil {
		t.Fatal(err)
	}
	return s, dir
}

// U-N001: an empty store reports no snapshot rather than an error the caller
// must special-case.
func TestLoadWithoutFile(t *testing.T) {
	s, _ := newStore(t)
	if _, err := s.Load(); !errors.Is(err, snapstore.ErrNoSnapshot) {
		t.Fatalf("want ErrNoSnapshot, got %v", err)
	}
}

// U-N002: save then load round-trips.
func TestSaveLoadRoundTrip(t *testing.T) {
	s, _ := newStore(t)
	if err := s.Save(snapshot(7, "dev-mac")); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.SnapshotVersion != 7 || got.SourceNode != "dev-mac" {
		t.Fatalf("round trip lost data: %+v", got)
	}
}

// U-N003 (Test-Module-002 U009 / E010): repeated saves leave exactly one
// snapshot file and no temp or history files behind.
func TestOnlyOneSnapshotFileEverExists(t *testing.T) {
	s, dir := newStore(t)
	for v := uint64(1); v <= 100; v++ {
		if err := s.Save(snapshot(v, "dev-mac")); err != nil {
			t.Fatalf("save %d: %v", v, err)
		}
	}
	files, err := s.Files()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0] != snapstore.FileName {
		t.Fatalf("store contains %v, want only %s", files, snapstore.FileName)
	}
	for _, f := range files {
		if strings.Contains(f, ".db") || strings.Contains(f, ".sqlite") || strings.Contains(f, "tmp") {
			t.Errorf("unexpected file %q in %s", f, dir)
		}
	}
	got, err := s.Load()
	if err != nil || got.SnapshotVersion != 100 {
		t.Fatalf("last write not visible: %+v %v", got, err)
	}
}

// U-N004 (Test-Module-002 U009): a corrupt file is quarantined and the caller
// gets the empty state instead of a crash.
func TestCorruptSnapshotIsQuarantined(t *testing.T) {
	s, dir := newStore(t)
	if err := os.WriteFile(s.Path(), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := s.Load()
	if !errors.Is(err, snapstore.ErrNoSnapshot) {
		t.Fatalf("want ErrNoSnapshot, got %v", err)
	}
	if _, statErr := os.Stat(s.Path()); !os.IsNotExist(statErr) {
		t.Error("corrupt file was not moved aside")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "last-known-good.corrupt")); statErr != nil {
		t.Errorf("quarantine file missing: %v", statErr)
	}

	// A subsequent good save must recover the store completely.
	if err := s.Save(snapshot(1, "dev-mac")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load(); err != nil {
		t.Fatalf("store did not recover: %v", err)
	}
}

// U-N005 (Test-Module-002 U008): a snapshot from an incompatible major schema
// is quarantined rather than partially loaded.
func TestIncompatibleSchemaIsQuarantined(t *testing.T) {
	s, _ := newStore(t)
	snap := snapshot(1, "dev-mac")
	snap.SchemaVersion = "9.0"
	raw, _ := json.Marshal(snap)
	if err := os.WriteFile(s.Path(), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load(); !errors.Is(err, snapstore.ErrNoSnapshot) {
		t.Fatalf("want ErrNoSnapshot, got %v", err)
	}
}

// U-N006 (Test-Module-002 U016): a snapshot from an unbound node is never
// stored and never loaded.
func TestForeignNodeIsRejected(t *testing.T) {
	s, _ := newStore(t)

	if err := s.Save(snapshot(1, "other-pc")); err == nil {
		t.Fatal("saving a foreign-node snapshot must fail")
	}

	raw, _ := json.Marshal(snapshot(1, "other-pc"))
	if err := os.WriteFile(s.Path(), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load(); !errors.Is(err, snapstore.ErrNoSnapshot) {
		t.Fatalf("foreign-node file must be quarantined, got %v", err)
	}
}

// U-N007: an invalid snapshot never overwrites a good one.
func TestInvalidSaveDoesNotDestroyGoodSnapshot(t *testing.T) {
	s, _ := newStore(t)
	if err := s.Save(snapshot(5, "dev-mac")); err != nil {
		t.Fatal(err)
	}

	bad := snapshot(6, "dev-mac")
	bad.Metrics[0].MetricKind = "vibes"
	if err := s.Save(bad); err == nil {
		t.Fatal("expected validation failure")
	}

	got, err := s.Load()
	if err != nil || got.SnapshotVersion != 5 {
		t.Fatalf("good snapshot lost: %+v %v", got, err)
	}
	files, _ := s.Files()
	if len(files) != 1 {
		t.Fatalf("temp file leaked: %v", files)
	}
}

// U-N008: the snapshot file is written with owner-only permissions.
func TestSnapshotFileIsPrivate(t *testing.T) {
	s, _ := newStore(t)
	if err := s.Save(snapshot(1, "dev-mac")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("snapshot mode = %o, want 600", perm)
	}
}

// S-N001: the persisted file contains no credential-shaped field.
func TestPersistedFileCarriesNoSecrets(t *testing.T) {
	s, _ := newStore(t)
	if err := s.Save(snapshot(1, "dev-mac")); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	if hits := protocol.AuditJSON(raw, nil); len(hits) != 0 {
		t.Fatalf("secret audit found: %v", hits)
	}
}
