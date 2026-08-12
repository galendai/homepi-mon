package providermeta

import (
	"testing"
	"time"
)

func TestRegisterAndLookup(t *testing.T) {
	// We register a sentinel type to exercise the registry. The package
	// cannot use t.Parallel because the registry is process-wide.
	meta := TypeMeta{
		TypeID:           "test_sentinel",
		Label:            "Test Sentinel",
		Provider:         "test",
		RequiresSecret:   true,
		SecretFieldLabel: "Test Key",
		MinInterval:      10 * time.Second,
		MinStaleAfter:    20 * time.Second,
		SupportedRegions: []string{"global", "cn"},
		DefaultRegion:    "global",
	}
	t.Cleanup(func() {
		registryMu.Lock()
		delete(registry, meta.TypeID)
		registryMu.Unlock()
	})
	Register(meta)
	got, ok := Lookup("test_sentinel")
	if !ok {
		t.Fatal("Lookup returned ok=false for registered type")
	}
	if got.Label != meta.Label {
		t.Errorf("Label = %q, want %q", got.Label, meta.Label)
	}
	if got.SecretMax() != DefaultSecretMax {
		t.Errorf("SecretMax fallback = %d, want %d", got.SecretMax(), DefaultSecretMax)
	}
	if !got.HasRegion("cn") {
		t.Error("HasRegion(cn) = false, want true")
	}
	if got.HasRegion("mars") {
		t.Error("HasRegion(mars) = true, want false")
	}
}

func TestRegisterRejectsEmptyFields(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("Register with empty TypeID did not panic")
		}
	}()
	Register(TypeMeta{Label: "x"})
}

func TestRegisterRejectsDuplicate(t *testing.T) {
	meta := TypeMeta{TypeID: "test_dup", Label: "Test Dup"}
	t.Cleanup(func() {
		registryMu.Lock()
		delete(registry, meta.TypeID)
		registryMu.Unlock()
	})
	Register(meta)
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("Register duplicate did not panic")
		}
	}()
	Register(meta)
}

func TestValidateRegion(t *testing.T) {
	meta := TypeMeta{
		TypeID:           "test_region",
		Label:            "Region Test",
		SupportedRegions: []string{"global", "cn"},
		DefaultRegion:    "global",
	}
	if err := meta.ValidateRegion("cn"); err != nil {
		t.Errorf("ValidateRegion(cn) = %v, want nil", err)
	}
	if err := meta.ValidateRegion(""); err != nil {
		t.Errorf("ValidateRegion(empty) = %v, want nil", err)
	}
	if err := meta.ValidateRegion("mars"); err == nil {
		t.Error("ValidateRegion(mars) = nil, want error")
	}
}

func TestEffectiveMins(t *testing.T) {
	meta := TypeMeta{}
	if got := meta.EffectiveMinInterval(); got != 5*time.Second {
		t.Errorf("default MinInterval = %s, want 5s", got)
	}
	meta.MinInterval = 30 * time.Second
	if got := meta.EffectiveMinInterval(); got != 30*time.Second {
		t.Errorf("override MinInterval = %s, want 30s", got)
	}
}

func TestKnownAndKnownIDs(t *testing.T) {
	meta := TypeMeta{TypeID: "test_known", Label: "Known"}
	t.Cleanup(func() {
		registryMu.Lock()
		delete(registry, meta.TypeID)
		registryMu.Unlock()
	})
	Register(meta)
	ids := KnownIDs()
	found := false
	for _, id := range ids {
		if id == "test_known" {
			found = true
		}
	}
	if !found {
		t.Errorf("KnownIDs = %v, want contains test_known", ids)
	}
	if len(Known()) == 0 {
		t.Error("Known() = empty, want at least 1 entry")
	}
}
