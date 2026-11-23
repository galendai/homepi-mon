package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/galendai/homepi-mon/internal/buildinfo"
	"github.com/galendai/homepi-mon/internal/protocol"
	"github.com/galendai/homepi-mon/internal/ui"
)

// preview replays a local redacted snapshot without starting the kiosk,
// opening a connection, reading credentials or changing terminal state.
func preview(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("preview", flag.ContinueOnError)
	fs.SetOutput(out)
	snapshotPath := fs.String("snapshot", "", "local redacted snapshot JSON (required)")
	layout := fs.String("layout", "compact", "preview layout: compact or current")
	styleName := fs.String("style", "rich", "display style: rich or ascii")
	at := fs.String("at", "", "replay time in RFC3339; default current local time")
	connected := fs.Bool("connected", false, "assume an online stream for this offline replay only")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if *snapshotPath == "" || fs.NArg() != 0 {
		return fmt.Errorf("preview requires --snapshot PATH and no positional arguments")
	}
	if *layout != "compact" && *layout != "current" {
		return fmt.Errorf("preview layout must be compact or current")
	}
	style, err := ui.ParseStyle(*styleName)
	if err != nil {
		return err
	}
	now := time.Now()
	if *at != "" {
		now, err = time.Parse(time.RFC3339, *at)
		if err != nil {
			return fmt.Errorf("preview --at requires an RFC3339 timestamp")
		}
	}
	file, err := os.Open(*snapshotPath)
	if err != nil {
		return fmt.Errorf("preview: open snapshot: %w", err)
	}
	defer file.Close()
	const maximumSnapshotBytes = 1 << 20
	raw, err := io.ReadAll(io.LimitReader(file, maximumSnapshotBytes+1))
	if err != nil {
		return fmt.Errorf("preview: read snapshot: %w", err)
	}
	if len(raw) > maximumSnapshotBytes {
		return fmt.Errorf("preview snapshot exceeds 1 MiB")
	}
	snapshot, err := protocol.DecodeSnapshot(raw)
	if err != nil {
		return fmt.Errorf("preview: %w", err)
	}
	vm := ui.Build(snapshot, ui.BuildOptions{
		Page: "CODING", Now: now, Connected: *connected,
		Thresholds: protocol.DefaultThresholds(), Version: buildinfo.UIVersion(),
		Pi:                ui.PiHealth{LANStatus: protocol.DisplayNA},
		FallbackNodeLabel: snapshot.SourceNodeLabel,
		RotationEnabled:   true, PageNumber: 1, PageCount: 5, DwellSeconds: 15,
	})
	var frame string
	if *layout == "compact" {
		frame, err = ui.RenderCompactOverviewString(vm, style)
		if err != nil {
			return err
		}
	} else {
		frame = ui.RenderStyledString(vm, style)
	}
	_, err = fmt.Fprintln(out, frame)
	return err
}
