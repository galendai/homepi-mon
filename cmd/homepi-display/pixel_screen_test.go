package main

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"testing"
	"time"

	"github.com/galendai/homepi-mon/internal/guirender"
	"github.com/galendai/homepi-mon/internal/ui"
)

type fakePixelRenderer struct {
	homeFrames   []guirender.HomeFrame
	legacyFrames []string
}

func (f *fakePixelRenderer) DrawHome(frame guirender.HomeFrame) error {
	f.homeFrames = append(f.homeFrames, frame)
	return nil
}

func (f *fakePixelRenderer) DrawLegacy(frame string) error {
	f.legacyFrames = append(f.legacyFrames, frame)
	return nil
}

func (f *fakePixelRenderer) Close() error { return nil }

func TestPixelScreenFallsBackToFb1(t *testing.T) {
	original := rendererFactory
	defer func() { rendererFactory = original }()
	var attempts []string
	rendererFactory = func(device string) (guirender.Renderer, error) {
		attempts = append(attempts, device)
		if device == "/dev/fb0" {
			return nil, errors.New("missing")
		}
		return &fakePixelRenderer{}, nil
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	screen, err := newPixelScreen(io.Discard, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer screen.renderer.Close()
	if want := []string{"/dev/fb0", "/dev/fb1"}; !reflect.DeepEqual(attempts, want) {
		t.Fatalf("framebuffer attempts = %v, want %v", attempts, want)
	}
}

func TestPixelScreenReportsAllFramebufferFailures(t *testing.T) {
	original := rendererFactory
	defer func() { rendererFactory = original }()
	rendererFactory = func(device string) (guirender.Renderer, error) {
		return nil, errors.New("unavailable")
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if _, err := newPixelScreen(io.Discard, logger); err == nil ||
		!bytes.Contains([]byte(err.Error()), []byte("/dev/fb1")) {
		t.Fatalf("missing framebuffer error = %v", err)
	}
}

func TestPixelScreenSkipsUnchangedFramesAndPreservesRemoteNotice(t *testing.T) {
	renderer := &fakePixelRenderer{}
	screen := &pixelScreen{out: &bytes.Buffer{}, renderer: renderer}
	now := time.Date(2026, time.November, 24, 14, 32, 0, 0, time.Local)
	vm := ui.ViewModel{Page: string(ui.PageHome), NodeLabel: "GALEN-MAC", Now: now, PageNumber: 1, PageCount: 6}
	if err := screen.draw(vm); err != nil {
		t.Fatal(err)
	}
	vm.Now = now.Add(time.Second)
	if err := screen.draw(vm); err != nil {
		t.Fatal(err)
	}
	if len(renderer.homeFrames) != 1 {
		t.Fatalf("unchanged home drew %d frames", len(renderer.homeFrames))
	}
	vm.Now = now.Add(time.Minute)
	if err := screen.draw(vm); err != nil {
		t.Fatal(err)
	}
	if len(renderer.homeFrames) != 2 || renderer.homeFrames[1].TimeText != "14:33" {
		t.Fatalf("minute change did not reach home: %+v", renderer.homeFrames)
	}
	vm.RemoteNotice = &ui.RemoteNotice{Text: "MAINTENANCE", Severity: "warning", Source: "dev-mac", ReturnIn: 30 * time.Second}
	if err := screen.draw(vm); err != nil {
		t.Fatal(err)
	}
	if len(renderer.legacyFrames) != 1 || !bytes.Contains([]byte(renderer.legacyFrames[0]), []byte("MAINTENANCE")) {
		t.Fatalf("remote notice did not replace home: %+v", renderer.legacyFrames)
	}
	if err := screen.draw(vm); err != nil {
		t.Fatal(err)
	}
	if len(renderer.legacyFrames) != 1 {
		t.Fatal("unchanged remote notice was redrawn")
	}
}
