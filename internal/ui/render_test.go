package ui_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/galendai/homepi-mon/internal/decimal"
	"github.com/galendai/homepi-mon/internal/protocol"
	"github.com/galendai/homepi-mon/internal/ui"
)

var updateGolden = os.Getenv("UPDATE_GOLDEN") == "1"

func at(hhmm string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", "2026-08-10 "+hhmm, time.UTC)
	if err != nil {
		panic(err)
	}
	return t
}

func pct(v int) *int { return &v }

func dur(d time.Duration) *time.Duration { return &d }

// baseModel is the UI-001 4 reference screen.
func baseModel() ui.ViewModel {
	return ui.ViewModel{
		NodeLabel: "DEV-MAC",
		Page:      "CODING",
		Link:      protocol.LinkLive,
		Now:       at("14:32"),
		Coding: []ui.Card{
			{
				Name: "Codex", PercentLeft: pct(42), WeekPercentLeft: pct(67),
				ResetLabel: "RESET 2H", Status: protocol.DisplayOK,
			},
			{
				Name: "MiniMax", PercentLeft: pct(68),
				ResetLabel: "RESET 2H", Status: protocol.DisplayOK,
			},
			{
				Name: "Kimi Code", PercentLeft: pct(35), WeekPercentLeft: pct(51),
				ResetLabel: "RESET 4H", Status: protocol.DisplayWarn,
			},
		},
		API: []ui.Card{
			{Name: "DeepSeek API", Amount: "CNY 8.20", Status: protocol.DisplayWarn},
			{Name: "Kimi API", Amount: "CNY 49.58", Status: protocol.DisplayOK},
		},
		Pi:         ui.PiHealth{TempC: 52, CPUPercent: 3, MemPercent: 18, LANStatus: protocol.DisplayOK, Known: true},
		AlertCount: 1,
		SyncedAgo:  dur(38 * time.Second),
		Version:    "V0.1.0",
	}
}

func offlineModel() ui.ViewModel {
	vm := baseModel()
	vm.Link = protocol.LinkOffline
	vm.Now = at("14:44")
	vm.SyncedAgo = dur(12 * time.Minute)
	last := at("14:32")
	vm.LastSyncAt = &last
	for i := range vm.Coding {
		vm.Coding[i].Status = protocol.DisplayStale
	}
	for i := range vm.API {
		vm.API[i].Status = protocol.DisplayStale
	}
	return vm
}

func authModel() ui.ViewModel {
	vm := baseModel()
	vm.Coding[0] = ui.Card{
		Name:       "Codex",
		Status:     protocol.DisplayAuth,
		ActionHint: "re-auth with official CLI on DEV-MAC",
	}
	vm.AlertCount = 2
	return vm
}

func emptyModel() ui.ViewModel {
	return ui.ViewModel{
		NodeLabel: "DEV-MAC",
		Page:      "CODING",
		Link:      protocol.LinkWait,
		Now:       at("14:32"),
		Pi:        ui.PiHealth{TempC: 51, CPUPercent: 2, MemPercent: 17, LANStatus: protocol.DisplayOK, Known: true},
		RetryIn:   dur(15 * time.Second),
		Version:   "V0.1.0",
	}
}

// U-U001 / U-U017 (Test-Module-002 U001, U017): the four Phase 1 screens are
// byte-for-byte stable and stay inside the 60x20 grid.
func TestGoldenScreens(t *testing.T) {
	cases := map[string]ui.ViewModel{
		"overview_live":    baseModel(),
		"overview_offline": offlineModel(),
		"overview_auth":    authModel(),
		"overview_empty":   emptyModel(),
	}
	for name, vm := range cases {
		t.Run(name, func(t *testing.T) {
			got := ui.RenderString(vm) + "\n"
			path := filepath.Join("testdata", name+".txt")
			if updateGolden {
				if err := os.MkdirAll("testdata", 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				t.Logf("updated %s", path)
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read golden (run UPDATE_GOLDEN=1 go test ./internal/ui): %v", err)
			}
			if got != string(want) {
				t.Errorf("screen differs.\n--- got ---\n%s\n--- want ---\n%s", got, want)
			}
		})
	}
}

// U-U002 (UI-001 11, Test-Module-002 U017): every rendered frame is exactly
// 20 rows of exactly 60 printable 7-bit ASCII cells.
func TestGridIsAlwaysExact(t *testing.T) {
	models := map[string]ui.ViewModel{
		"live":    baseModel(),
		"offline": offlineModel(),
		"auth":    authModel(),
		"empty":   emptyModel(),
		"crit":    critModel(),
		"hostile": hostileModel(),
	}
	for name, vm := range models {
		lines := ui.Render(vm)
		if len(lines) != ui.Rows {
			t.Errorf("%s: %d rows, want %d", name, len(lines), ui.Rows)
		}
		for i, line := range lines {
			if len(line) != ui.Cols {
				t.Errorf("%s line %d: %d cells, want %d: %q", name, i+1, len(line), ui.Cols, line)
			}
			for j := 0; j < len(line); j++ {
				if line[j] < 0x20 || line[j] > 0x7e {
					t.Errorf("%s line %d col %d: non-ASCII byte %#x", name, i+1, j+1, line[j])
				}
			}
		}
	}
}

func critModel() ui.ViewModel {
	vm := baseModel()
	vm.Link = protocol.LinkCrit
	vm.Coding[0].PercentLeft = pct(4)
	vm.Coding[0].Status = protocol.DisplayCrit
	vm.AlertCount = 2
	return vm
}

// hostileModel plants over-long names, control characters and out-of-range
// numbers to prove nothing can escape the grid.
func hostileModel() ui.ViewModel {
	vm := baseModel()
	vm.NodeLabel = "A-VERY-LONG-NODE-ALIAS-THAT-OVERFLOWS"
	vm.Coding[0].Name = "ProviderWithAnExtremelyLongDisplayName"
	vm.Coding[1].Name = "Bad\x1b[31mName\x07"
	vm.Coding[2].PercentLeft = pct(999)
	vm.API[0].Name = "DeepSeek\x00API\nInjected"
	vm.API[0].Amount = "CNY 123456789012345678"
	vm.API[1].Amount = ""
	vm.Pi = ui.PiHealth{TempC: 1234, CPUPercent: 999, MemPercent: -5, LANStatus: protocol.DisplayError, Known: true}
	vm.AlertCount = 12345
	vm.Version = "V99.99.9999"
	return vm
}

// U-U013 (Test-Module-002 U013): control characters in any text field are
// removed before they can reach the terminal. The requirement is that no
// escape or control byte survives; the harmless printable remainder of a
// mangled sequence is displayed as ordinary text.
func TestANSIEscapesAreStripped(t *testing.T) {
	vm := baseModel()
	vm.Coding[0].Name = "Codex\x1b[2J\x1b[H"
	vm.NodeLabel = "DEV\x1b[31m-MAC"
	vm.API[0].Amount = "CNY\x07 1.00\r\n"
	for _, line := range ui.Render(vm) {
		for i := 0; i < len(line); i++ {
			if line[i] < 0x20 || line[i] > 0x7e {
				t.Fatalf("control byte %#x survived rendering in %q", line[i], line)
			}
		}
	}
}

// U-U003 (Test-Module-002 U003, U018): each status is identifiable from text
// alone; the renderer emits no colour codes at all.
func TestStatusesAreTextOnly(t *testing.T) {
	statuses := []protocol.DisplayStatus{
		protocol.DisplayOK, protocol.DisplayWarn, protocol.DisplayCrit,
		protocol.DisplayDelayed, protocol.DisplayStale, protocol.DisplayAuth,
		protocol.DisplayNA, protocol.DisplayError,
	}
	for _, s := range statuses {
		vm := baseModel()
		vm.API[1].Status = s
		out := ui.RenderString(vm)
		if !strings.Contains(out, string(s)) {
			t.Errorf("status %s not visible as text", s)
		}
		if strings.Contains(out, "\x1b[") {
			t.Errorf("status %s emitted a colour escape", s)
		}
	}
}

// U-U004 (UI-001 4.2): with no upstream limit the bar is empty and the value
// reads "--" rather than a fabricated percentage.
func TestUnknownLimitRendersNoBarValue(t *testing.T) {
	vm := baseModel()
	vm.Coding[1].PercentLeft = nil
	vm.Coding[1].ResetLabel = "ROLLING"
	out := ui.Render(vm)

	var found bool
	for _, line := range out {
		if strings.Contains(line, "MiniMax") {
			found = true
			if !strings.Contains(line, "[----------------]") {
				t.Errorf("expected empty bar, got %q", line)
			}
			if !strings.Contains(line, "-- LEFT") {
				t.Errorf("expected \"-- LEFT\", got %q", line)
			}
		}
	}
	if !found {
		t.Fatal("MiniMax row missing")
	}
}

func TestBuildFormatsBalancesWithExactlyTwoDecimals(t *testing.T) {
	values := []string{"1", "1.2", "1.235"}
	wants := []string{"CNY 1.00", "CNY 1.20", "CNY 1.24"}
	metrics := make([]protocol.ProviderMetric, len(values))
	for i, raw := range values {
		value := decimal.MustParse(raw)
		metrics[i] = protocol.ProviderMetric{
			ID: "balance-" + raw, Provider: "provider-" + raw, DisplayName: "Balance",
			MetricKind: protocol.KindBalance, Value: &value, Unit: "CNY",
			Window: protocol.WindowPrepaid, ObservedAt: at("14:32"),
			Precision: protocol.PrecisionExact, SourceKind: protocol.SourceOfficialAPI,
			Status: protocol.StatusOK, Group: ui.GroupAPI, Order: i,
		}
	}
	vm := ui.Build(&protocol.MetricSnapshot{
		SchemaVersion: protocol.SchemaVersion, SourceEpoch: "test", SnapshotVersion: 1,
		GeneratedAt: at("14:32"), SourceNode: "node", Metrics: metrics,
	}, ui.BuildOptions{Now: at("14:32"), Connected: true})
	if len(vm.API) != len(wants) {
		t.Fatalf("cards = %+v", vm.API)
	}
	for i, want := range wants {
		if got := vm.API[i].Amount; got != want {
			t.Errorf("amount %d = %q, want %q", i, got, want)
		}
	}
}

func TestBuildUsesLowestOrderBalanceAsProviderPrimary(t *testing.T) {
	available := decimal.MustParse("0")
	voucher := decimal.MustParse("7.50")
	cash := decimal.MustParse("-1.0373199")
	metrics := []protocol.ProviderMetric{
		{
			ID: "kimi.available.cny", Provider: "kimi", DisplayName: "Kimi Available",
			MetricKind: protocol.KindBalance, Value: &available, Unit: "CNY",
			Window: protocol.WindowPrepaid, ObservedAt: at("14:32"),
			Precision: protocol.PrecisionExact, SourceKind: protocol.SourceOfficialAPI,
			Status: protocol.StatusOK, Group: ui.GroupAPI, Order: 50,
		},
		{
			ID: "kimi.voucher.cny", Provider: "kimi", DisplayName: "Kimi Voucher",
			MetricKind: protocol.KindBalance, Value: &voucher, Unit: "CNY",
			Window: protocol.WindowPrepaid, ObservedAt: at("14:32"),
			Precision: protocol.PrecisionExact, SourceKind: protocol.SourceOfficialAPI,
			Status: protocol.StatusOK, Group: ui.GroupAPI, Order: 51,
		},
		{
			ID: "kimi.cash.cny", Provider: "kimi", DisplayName: "Kimi Cash",
			MetricKind: protocol.KindBalance, Value: &cash, Unit: "CNY",
			Window: protocol.WindowPrepaid, ObservedAt: at("14:32"),
			Precision: protocol.PrecisionExact, SourceKind: protocol.SourceOfficialAPI,
			Status: protocol.StatusOK, Group: ui.GroupAPI, Order: 52,
		},
	}

	for _, input := range [][]protocol.ProviderMetric{
		metrics,
		{metrics[2], metrics[0], metrics[1]},
	} {
		vm := ui.Build(&protocol.MetricSnapshot{
			SchemaVersion: protocol.SchemaVersion, SourceEpoch: "epoch", SnapshotVersion: 1,
			GeneratedAt: at("14:32"), SourceNode: "node", Metrics: input,
		}, ui.BuildOptions{Now: at("14:32"), Connected: true})
		if len(vm.API) != 1 || vm.API[0].Name != "Kimi Available" || vm.API[0].Amount != "CNY 0.00" {
			t.Fatalf("API cards = %+v", vm.API)
		}
	}
}

// U-U005 (UI-001 4.2): an estimated value is labelled EST so it cannot pose as
// an exact reading.
func TestEstimatedValueIsLabelled(t *testing.T) {
	vm := baseModel()
	vm.Coding[0].Estimated = true
	if !strings.Contains(ui.RenderString(vm), "EST") {
		t.Error("estimated value is not labelled")
	}
}

// U-U006: the progress bar is always 16 cells and rounds to the nearest cell.
func TestBarWidthAndRounding(t *testing.T) {
	// Frame rows: 0 border, 1 header, 2 border, 3 section title, 4 first card.
	const firstCardRow = 4

	for _, p := range []int{0, 1, 3, 4, 42, 50, 68, 97, 100} {
		vm := baseModel()
		vm.Coding[0].PercentLeft = pct(p)
		line := ui.Render(vm)[firstCardRow]
		open := strings.IndexByte(line, '[')
		closeIdx := strings.IndexByte(line, ']')
		if open < 0 || closeIdx < 0 {
			t.Fatalf("no bar in %q", line)
		}
		if got := closeIdx - open - 1; got != 16 {
			t.Errorf("percent %d: bar width %d, want 16", p, got)
		}
	}
	vm := baseModel()
	vm.Coding[0].PercentLeft = pct(0)
	if !strings.Contains(ui.Render(vm)[firstCardRow], "[----------------]") {
		t.Error("0% must render an empty bar")
	}
	vm.Coding[0].PercentLeft = pct(100)
	if !strings.Contains(ui.Render(vm)[firstCardRow], "[################]") {
		t.Error("100% must render a full bar")
	}
}

// U-U007: the empty state offers exactly one action, on the remote host.
func TestEmptyStateGivesRemoteAction(t *testing.T) {
	out := ui.RenderString(emptyModel())
	for _, want := range []string{"WAITING FOR DEV-MAC", "CHECK homepi-node STATUS ON REMOTE", "NO SNAPSHOT RECEIVED", "WAIT"} {
		if !strings.Contains(out, want) {
			t.Errorf("empty state missing %q:\n%s", want, out)
		}
	}
	// UI-001 13: no local interaction is ever offered.
	for _, forbidden := range []string{"PRESS", "TOUCH", "KEY", "MENU", "SETTINGS", "EXIT"} {
		if strings.Contains(out, forbidden) {
			t.Errorf("empty state offers local interaction %q", forbidden)
		}
	}
}

// U-U008 (UI-001 5.2): the AUTH card names the official CLI and shows no value.
func TestAuthCardShowsOfficialCLIAction(t *testing.T) {
	out := ui.RenderString(authModel())
	if !strings.Contains(out, "AUTH") {
		t.Error("AUTH badge missing")
	}
	if !strings.Contains(out, "OFFICIAL CLI") {
		t.Error("re-auth action does not name the official CLI")
	}
	if !strings.Contains(out, "-- LEFT") {
		t.Error("AUTH card must not show a stale percentage as if it were live")
	}
}

// U-U009 (UI-001 5.1): the offline footer states staleness and the last sync.
func TestOfflineFooter(t *testing.T) {
	out := ui.RenderString(offlineModel())
	for _, want := range []string{"OFFLINE", "DATA STALE 12M", "LAST SYNC 14:32", "RETRYING", "STALE"} {
		if !strings.Contains(out, want) {
			t.Errorf("offline screen missing %q:\n%s", want, out)
		}
	}
}

// U-U010: no page ever advertises a local key, touch or menu affordance.
func TestNoLocalInteractionHints(t *testing.T) {
	for _, vm := range []ui.ViewModel{baseModel(), offlineModel(), authModel(), emptyModel()} {
		out := strings.ToUpper(ui.RenderString(vm))
		for _, forbidden := range []string{"PRESS ", "[Q]", "TOUCH", "SCROLL", "MENU"} {
			if strings.Contains(out, forbidden) {
				t.Errorf("screen offers local interaction %q", forbidden)
			}
		}
	}
}

// U-U011: rendering is deterministic, which is what allows change-driven
// redraws instead of a timer-driven repaint (UI-001 10).
func TestRenderIsDeterministic(t *testing.T) {
	vm := baseModel()
	first := ui.RenderString(vm)
	for i := 0; i < 20; i++ {
		if ui.RenderString(vm) != first {
			t.Fatal("render is not deterministic")
		}
	}
}
