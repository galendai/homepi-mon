package connector_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/galendai/homepi-mon/internal/config"
	"github.com/galendai/homepi-mon/internal/connector"
	_ "github.com/galendai/homepi-mon/internal/connector/mock"
	"github.com/galendai/homepi-mon/internal/protocol"
	"github.com/galendai/homepi-mon/internal/secretstore"
)

func TestKnownTypesIncludesMock(t *testing.T) {
	got := connector.KnownTypes()
	sort.Strings(got)
	want := []string{
		"codex_usage", "deepseek_api", "kimi_api", "kimi_coding",
		"minimax_coding", "mock",
	}
	if len(got) != len(want) {
		t.Fatalf("KnownTypes = %v, want %v", got, want)
	}
	for i, id := range want {
		if got[i] != id {
			t.Errorf("KnownTypes[%d] = %q, want %q", i, got[i], id)
		}
	}
}

func TestHasType(t *testing.T) {
	if !connector.HasType("mock") {
		t.Error("HasType(mock) = false")
	}
	if connector.HasType("not-a-type") {
		t.Error("HasType(not-a-type) = true")
	}
}

func TestBuildRejectsUnknownType(t *testing.T) {
	spec := config.ProviderConfig{ID: "p", Type: "no-such-type", AccountLabel: "x"}
	if _, err := connector.Build(spec, nil); err == nil {
		t.Error("Build accepted unknown type")
	}
	spec.Type = ""
	if _, err := connector.Build(spec, nil); err == nil {
		t.Error("Build accepted empty type")
	}
}

func TestBuildMockConnector(t *testing.T) {
	dir := t.TempDir()
	fx := writeFixture(t, dir)
	spec := config.ProviderConfig{
		ID: "mock-1", Type: "mock", AccountLabel: "demo",
		MockFixture: fx,
	}
	c, err := connector.Build(spec, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if c.ID() != "mock-1" {
		t.Errorf("ID() = %q", c.ID())
	}
	if c.Provider() != "mock" {
		t.Errorf("Provider() = %q", c.Provider())
	}
}

func TestBuildUnimplementedReturnsConnector(t *testing.T) {
	spec := config.ProviderConfig{ID: "k1", Type: "minimax_coding", AccountLabel: "demo"}
	c, err := connector.Build(spec, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	_, err = c.Collect(context.Background())
	if err == nil {
		t.Fatal("unimplemented connector Collect succeeded")
	}
	var ce *connector.Error
	if !errors.As(err, &ce) {
		t.Fatalf("Collect returned non-classified error: %v", err)
	}
	if ce.Class != protocol.ErrUnsupported {
		t.Errorf("Class = %q, want unsupported", ce.Class)
	}
}

func writeFixture(t *testing.T, dir string) string {
	t.Helper()
	body := `{"metrics":[],"connectors":[]}`
	path := filepath.Join(dir, "fx.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// Compile-time check that secretstore.Store satisfies the expected role.
var _ = func() secretstore.Store { return nil }
