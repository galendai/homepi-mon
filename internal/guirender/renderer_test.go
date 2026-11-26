package guirender

import "testing"

// TestOpenClashCardGridCapacity locks the native OPENCLASH page to the shared
// 2-column x 5-row card grid described by the display contract.
func TestOpenClashCardGridCapacity(t *testing.T) {
	if OpenClashRowCapacity != 10 {
		t.Fatalf("OpenClashRowCapacity = %d, want 10 (2x5 card grid)", OpenClashRowCapacity)
	}
}

// TestOpenClashFrameShape verifies the frame keeps the header, summary, and
// card-row fields the native bridge consumes, and stays comparable so the
// display can suppress unchanged redraws.
func TestOpenClashFrameShape(t *testing.T) {
	frame := OpenClashFrame{
		TitleText:    "OPENCLASH",
		HeaderStatus: "UP",
		FooterStatus: "DELAY 42MS",
		PageText:     "1/2",
		SubpageText:  "OPENCLASH 1/2",
		Empty:        "NO NODES",
		OverflowText: "",
		RowCount:     OpenClashRowCapacity,
	}
	if len(frame.Rows) != 10 {
		t.Fatalf("OpenClashFrame.Rows length = %d, want 10", len(frame.Rows))
	}
	if cap(frame.Rows) != OpenClashRowCapacity {
		t.Fatalf("OpenClashFrame.Rows capacity = %d, want %d", cap(frame.Rows), OpenClashRowCapacity)
	}
	for index := range frame.Rows {
		frame.Rows[index] = OpenClashRowFrame{
			Name:     "NODE",
			Delay:    "42ms",
			Selected: index == 0,
			Tone:     TokenPlanToneOK,
		}
	}
	redrawn := frame
	if frame != redrawn {
		t.Fatal("OpenClashFrame must stay comparable for redraw suppression")
	}
	if !frame.Rows[0].Selected || frame.Rows[1].Selected {
		t.Fatal("OpenClashRowFrame.Selected must persist per active card")
	}
	if frame.Rows[2].Name != "NODE" || frame.Rows[2].Delay != "42ms" {
		t.Fatal("OpenClashRowFrame must carry per-card name and ping")
	}
}
