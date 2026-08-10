package deviceacl

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func TestHashTokenDeterministic(t *testing.T) {
	a := HashToken("alpha")
	b := HashToken("alpha")
	if a != b {
		t.Errorf("HashToken not deterministic: %q vs %q", a, b)
	}
	if HashToken("alpha") == HashToken("beta") {
		t.Error("HashToken collision between distinct tokens")
	}
	if len(a) != 64 {
		t.Errorf("HashToken length = %d, want 64", len(a))
	}
}

func TestAddAndLookup(t *testing.T) {
	a := New()
	if a.IsRevoked("x") {
		t.Fatal("empty ACL already contains x")
	}
	a.Add("x")
	if !a.IsRevoked("x") {
		t.Error("Add did not register x")
	}
	if a.IsRevoked("y") {
		t.Error("ACL reports y revoked")
	}
	if a.Len() != 1 {
		t.Errorf("Len = %d, want 1", a.Len())
	}
}

func TestLoadMissingYieldsEmpty(t *testing.T) {
	dir := t.TempDir()
	a, err := Load(filepath.Join(dir, "revoked.json"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if a.Len() != 0 {
		t.Errorf("Len = %d, want 0", a.Len())
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "revoked.json")
	a := New()
	a.Add("device-token:pi-kiosk-a")
	a.Add("device-token:pi-kiosk-b")
	if err := a.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("permission = %o, want 0600", info.Mode().Perm())
	}
	b, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if b.Len() != 2 {
		t.Errorf("Len = %d, want 2", b.Len())
	}
	if !b.IsRevoked("device-token:pi-kiosk-a") {
		t.Error("pi-kiosk-a missing after round trip")
	}
	if b.IsRevoked("device-token:pi-kiosk-c") {
		t.Error("extra entry found after round trip")
	}
}

func TestLoadRejectsMalformedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "revoked.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Error("Load accepted non-JSON file")
	}
}

func TestLoadRejectsBadHashes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "revoked.json")
	body := `{"schema_version":1,"revoked_hashes":["not-hex"]}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Error("Load accepted file with malformed hash")
	}
}

func TestSnapshotIsDeterministicAndSorted(t *testing.T) {
	a := New()
	for _, s := range []string{"c", "a", "b", "d"} {
		a.Add(s)
	}
	got := a.Snapshot()
	want := []string{
		HashToken("a"), HashToken("b"), HashToken("c"), HashToken("d"),
	}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Snapshot = %v, want %v", got, want)
	}
}

func TestSaveRefusesEmptyPath(t *testing.T) {
	a := New()
	if err := a.Save(""); !errors.Is(err, errors.New("")) && err == nil {
		t.Error("Save accepted empty path")
	}
}
