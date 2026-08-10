package secretstore

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateRefAcceptsCommon(t *testing.T) {
	cases := []string{
		"provider-key:deepseek-1",
		"device-token:pi-kiosk",
		"a-b_c.d@e",
		"x",
	}
	for _, ref := range cases {
		if err := ValidateRef(ref); err != nil {
			t.Errorf("ValidateRef(%q) returned %v, want nil", ref, err)
		}
	}
}

func TestValidateRefRejectsIllegal(t *testing.T) {
	cases := []struct {
		ref    string
		reason string
	}{
		{"", "empty"},
		{strings.Repeat("a", 257), "too long"},
		{"with space", "space"},
		{"with\nnewline", "control char"},
		{"semi;colon", "semicolon"},
		{"with?query", "question mark"},
	}
	for _, tc := range cases {
		if err := ValidateRef(tc.ref); err == nil {
			t.Errorf("ValidateRef(%q) accepted %s ref", tc.ref, tc.reason)
		}
	}
}

func TestFileBackendRoundTrip(t *testing.T) {
	dir := t.TempDir()
	b, err := NewFileBackend(dir)
	if err != nil {
		t.Fatalf("NewFileBackend: %v", err)
	}
	ctx := context.Background()

	if _, err := b.Get(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get missing: want ErrNotFound, got %v", err)
	}

	if err := b.Set(ctx, "provider-key:foo", "s3cret-value"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := b.Get(ctx, "provider-key:foo")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "s3cret-value" {
		t.Errorf("Get = %q, want %q", got, "s3cret-value")
	}

	if err := b.Delete(ctx, "provider-key:foo"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := b.Get(ctx, "provider-key:foo"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after Delete: want ErrNotFound, got %v", err)
	}

	// Double-delete reports ErrNotFound but is otherwise harmless.
	if err := b.Delete(ctx, "provider-key:foo"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete missing: want ErrNotFound, got %v", err)
	}
}

func TestFileBackendWarnsOnSetDelete(t *testing.T) {
	dir := t.TempDir()
	b, err := NewFileBackend(dir)
	if err != nil {
		t.Fatalf("NewFileBackend: %v", err)
	}

	// Capture stderr for both Set and Delete.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	oldStderr := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = oldStderr }()

	ctx := context.Background()
	if err := b.Set(ctx, "device-token:a", "v"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := b.Delete(ctx, "device-token:a"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_ = w.Close()

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("read pipe: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "WARNING") || !strings.Contains(out, "file fallback") {
		t.Errorf("expected warning on stderr, got %q", out)
	}
}

func TestFileBackendPermissionIs0600(t *testing.T) {
	dir := t.TempDir()
	b, err := NewFileBackend(dir)
	if err != nil {
		t.Fatalf("NewFileBackend: %v", err)
	}
	if err := b.Set(context.Background(), "k", "v"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(dir, "*.secret"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("Glob: %v, matches=%d", err, len(matches))
	}
	info, err := os.Stat(matches[0])
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("secret file mode = %o, want 0600", perm)
	}
}

func TestFileBackendAtomicReplace(t *testing.T) {
	dir := t.TempDir()
	b, err := NewFileBackend(dir)
	if err != nil {
		t.Fatalf("NewFileBackend: %v", err)
	}
	ctx := context.Background()

	if err := b.Set(ctx, "k", "first"); err != nil {
		t.Fatalf("Set first: %v", err)
	}
	if err := b.Set(ctx, "k", "second"); err != nil {
		t.Fatalf("Set second: %v", err)
	}

	matches, err := filepath.Glob(filepath.Join(dir, "*.secret"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("Glob after overwrite: %v, matches=%d", err, len(matches))
	}
	got, err := b.Get(ctx, "k")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "second" {
		t.Errorf("Get = %q, want %q (atomic overwrite)", got, "second")
	}
}

func TestFileBackendReferenceNamesDoNotCollide(t *testing.T) {
	dir := t.TempDir()
	b, err := NewFileBackend(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := b.Set(ctx, "keyring:a:b", "colon"); err != nil {
		t.Fatal(err)
	}
	if err := b.Set(ctx, "keyring:a_b", "underscore"); err != nil {
		t.Fatal(err)
	}
	colon, err := b.Get(ctx, "keyring:a:b")
	if err != nil {
		t.Fatal(err)
	}
	underscore, err := b.Get(ctx, "keyring:a_b")
	if err != nil {
		t.Fatal(err)
	}
	if colon != "colon" || underscore != "underscore" {
		t.Fatalf("secrets collided: colon=%q underscore=%q", colon, underscore)
	}
	matches, err := filepath.Glob(filepath.Join(dir, "*.secret"))
	if err != nil || len(matches) != 2 {
		t.Fatalf("secret files = %d, err=%v", len(matches), err)
	}
}

func TestFileBackendRejectsInvalidRef(t *testing.T) {
	dir := t.TempDir()
	b, err := NewFileBackend(dir)
	if err != nil {
		t.Fatalf("NewFileBackend: %v", err)
	}
	ctx := context.Background()
	if err := b.Set(ctx, "has space", "v"); err == nil {
		t.Error("Set with invalid ref accepted")
	}
	if _, err := b.Get(ctx, ""); err == nil {
		t.Error("Get with empty ref accepted")
	}
}

func TestFileBackendRefusesEmptyDir(t *testing.T) {
	if _, err := NewFileBackend(""); err == nil {
		t.Error("NewFileBackend(\"\") accepted")
	}
}
