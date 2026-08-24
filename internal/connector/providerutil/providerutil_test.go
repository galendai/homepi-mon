package providerutil

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/galendai/homepi-mon/internal/config"
)

func TestRemainingUsesExplicitOrExactDerivedValue(t *testing.T) {
	explicit, limit, derived, err := Remaining(json.Number("100"), json.Number("32"), json.Number("70"))
	if err != nil || explicit.String() != "70" || limit.String() != "100" || len(derived) != 0 {
		t.Fatalf("explicit remaining = %s/%s %v err=%v", explicit.String(), limit.String(), derived, err)
	}
	calculated, limit, derived, err := Remaining("110.00", "10.5", nil)
	if err != nil || calculated.String() != "99.50" || limit.String() != "110.00" || len(derived) != 1 {
		t.Fatalf("derived remaining = %s/%s %v err=%v", calculated.String(), limit.String(), derived, err)
	}
	if _, _, _, err := Remaining("100", "101", nil); err == nil {
		t.Fatal("remaining accepted used greater than limit")
	}
}

func TestResetTimeSupportsAbsoluteAndRelativeValues(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	if got := ResetTime(json.Number("1800003600"), nil, now); got == nil || got.Unix() != 1_800_003_600 {
		t.Fatalf("absolute reset = %v", got)
	}
	if got := ResetTime(nil, json.Number("90"), now); got == nil || got.Sub(now) != 90*time.Second {
		t.Fatalf("relative reset = %v", got)
	}
	if got := ResetTime("not-a-time", nil, now); got != nil {
		t.Fatalf("invalid reset = %v", got)
	}
}

func TestBaseURLAndAuthFileValidation(t *testing.T) {
	spec := config.ProviderConfig{Region: "global"}
	if got, err := BaseURL(spec, "https://global.example", "https://cn.example"); err != nil || got != "https://global.example" {
		t.Fatalf("global base = %q, err=%v", got, err)
	}
	spec.Region, spec.BaseURL = "custom", "https://custom.example/root/"
	if got, err := BaseURL(spec, "", ""); err != nil || got != "https://custom.example/root" {
		t.Fatalf("custom base = %q, err=%v", got, err)
	}
	abs := filepath.Join(string(filepath.Separator), "tmp", "auth.json")
	if got, err := ExpandAuthFile(abs); err != nil || got != filepath.Clean(abs) {
		t.Fatalf("auth file = %q, err=%v", got, err)
	}
	if _, err := ExpandAuthFile("relative/auth.json"); err == nil {
		t.Fatal("relative auth file was accepted")
	}
	if got, err := ExpandGrokAuthFile(""); err != nil || filepath.Base(got) != "auth.json" || filepath.Base(filepath.Dir(got)) != ".grok" {
		t.Fatalf("Grok default auth file = %q, err=%v", got, err)
	}
	if got, err := ExpandGrokAuthFile("~/custom/grok-auth.json"); err != nil || filepath.Base(got) != "grok-auth.json" {
		t.Fatalf("Grok custom auth file = %q, err=%v", got, err)
	}
	if _, err := ExpandGrokAuthFile("relative/auth.json"); err == nil {
		t.Fatal("relative Grok auth file was accepted")
	}
}
