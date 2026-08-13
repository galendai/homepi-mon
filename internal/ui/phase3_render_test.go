package ui_test

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/galendai/homepi-mon/internal/protocol"
	"github.com/galendai/homepi-mon/internal/ui"
)

func TestPhase3FivePagesStayWithinASCIIAndRichGrid(t *testing.T) {
	online := true
	cpu, memory, disk := 28, 42, 71
	rx, tx := int64(12_000_000), int64(3_000_000)
	alerts, environments, running, stopped := 1, 2, 18, 1
	base := baseModel()
	base.RotationEnabled = true
	base.PageCount, base.DwellSeconds, base.HasSnapshot = 5, 15, true
	base.Nodes = []ui.NodeCard{{
		Name: "NAS-01", Online: &online, CPUPercent: &cpu, MemoryPercent: &memory,
		DiskPercent: &disk, NetworkReceiveBPS: &rx, NetworkTransmitBPS: &tx,
		Status: protocol.DisplayOK,
	}}
	base.Services = []ui.ServiceCard{{
		Name: "Grafana", Kind: "grafana", Version: "12.1.0", Status: protocol.DisplayWarn,
		FiringAlerts: &alerts,
	}, {
		Name: "Portainer", Kind: "portainer", Version: "2.38.0", Status: protocol.DisplayOK,
		EnvironmentsTotal: &environments, EnvironmentsOnline: &environments,
		ContainersRunning: &running, ContainersStopped: &stopped,
	}}
	base.Connectors = []ui.ConnectorCard{{Name: "prometheus-main", Status: protocol.DisplayOK}}

	pages := []struct {
		page  ui.Page
		token string
	}{
		{ui.PageCoding, "CODING PLANS"}, {ui.PageAPI, "API BALANCE"},
		{ui.PageHomeLab, "HOME LAB"}, {ui.PageServices, "SERVICES"}, {ui.PageSystem, "SYSTEM"},
	}
	for index, item := range pages {
		t.Run(string(item.page), func(t *testing.T) {
			vm := base
			vm.Page, vm.PageNumber = string(item.page), index+1
			ascii := ui.RenderStyledString(vm, ui.StyleASCII)
			if !strings.Contains(ascii, item.token) || !strings.Contains(ascii, "PAGE ") || !strings.Contains(ascii, "AUTO 15S") {
				t.Fatalf("missing Phase 3 labels:\n%s", ascii)
			}
			assertPhase3Grid(t, ascii, false)
			rich := ui.RenderStyledString(vm, ui.StyleRich)
			assertPhase3Grid(t, rich, true)
		})
	}
}

func TestPhase3HostileHomeLabTextCannotEscapeGrid(t *testing.T) {
	vm := baseModel()
	vm.RotationEnabled, vm.HasSnapshot = true, true
	vm.Page = string(ui.PageHomeLab)
	vm.Nodes = []ui.NodeCard{{Name: "NAS\x1b[2J\nWITH-A-VERY-LONG-NAME", Status: protocol.DisplayError}}
	out := ui.RenderStyledString(vm, ui.StyleASCII)
	if strings.Contains(out, "\x1b") || strings.Contains(out, "\nWITH") {
		t.Fatalf("control input survived:\n%s", out)
	}
	assertPhase3Grid(t, out, false)
}

func TestPhase3StatusAndEmptyVariantsStayWithinBothGrids(t *testing.T) {
	base := baseModel()
	base.RotationEnabled, base.HasSnapshot = true, true
	variants := []ui.ViewModel{base}
	empty := base
	empty.Coding, empty.API, empty.Nodes, empty.Services, empty.Connectors = nil, nil, nil, nil, nil
	variants = append(variants, empty)
	for _, status := range []protocol.DisplayStatus{
		protocol.DisplayStale, protocol.DisplayAuth, protocol.DisplayCrit,
	} {
		variant := empty
		variant.Nodes = []ui.NodeCard{{Name: "node", Status: status}}
		variant.Services = []ui.ServiceCard{{Name: "service", Kind: "grafana", Status: status}}
		variant.Connectors = []ui.ConnectorCard{{Name: "connector", Status: status}}
		variants = append(variants, variant)
	}
	for _, variant := range variants {
		for _, page := range []ui.Page{ui.PageCoding, ui.PageAPI, ui.PageHomeLab, ui.PageServices, ui.PageSystem} {
			variant.Page = string(page)
			assertPhase3Grid(t, ui.RenderStyledString(variant, ui.StyleASCII), false)
			assertPhase3Grid(t, ui.RenderStyledString(variant, ui.StyleRich), true)
		}
	}
}

func TestRemoteNoticeStaysWithinASCIIAndRichGrid(t *testing.T) {
	vm := baseModel()
	vm.HasSnapshot, vm.RotationEnabled = true, true
	vm.RemoteNotice = &ui.RemoteNotice{
		Text:     "NOTICE\x1b[2J\nWITH A VERY LONG HOSTILE LINE THAT MUST NEVER ESCAPE THE FIXED DISPLAY GRID",
		Severity: "critical", Source: "dev-mac", ReturnIn: 25 * time.Second,
	}
	for _, style := range []ui.Style{ui.StyleASCII, ui.StyleRich} {
		out := ui.RenderStyledString(vm, style)
		plain := out
		if style == ui.StyleRich {
			plain = stripSGR(out)
		}
		if strings.Contains(plain, "\x1b") || !strings.Contains(plain, "REMOTE NOTICE") ||
			!strings.Contains(plain, "RETURN IN 25S") {
			t.Fatalf("unsafe or incomplete remote notice:\n%s", plain)
		}
		assertPhase3Grid(t, out, style == ui.StyleRich)
	}
}

func TestInitialHomeLabConnectorFailureStillProducesServiceCard(t *testing.T) {
	now := at("14:32")
	vm := ui.Build(&protocol.MetricSnapshot{
		SchemaVersion: protocol.SchemaVersion, SourceEpoch: "e", SnapshotVersion: 1,
		GeneratedAt: now, SourceNode: "node",
		ConnectorHealth: []protocol.ConnectorHealth{{
			ConnectorID: "grafana-main", Provider: "grafana", Enabled: true,
			State: protocol.ConnBlockedAuth, ErrorClass: protocol.ErrAuth,
		}},
	}, ui.BuildOptions{Page: string(ui.PageServices), Now: now, Connected: true, RotationEnabled: true})
	if len(vm.Services) != 1 || vm.Services[0].Status != protocol.DisplayAuth {
		t.Fatalf("services=%+v", vm.Services)
	}
	if !strings.Contains(ui.RenderString(vm), "AUTH") {
		t.Fatal("initial connector failure is absent from Services page")
	}
}

func TestPhase3FrameDoesNotChangeEverySecondForSyncAge(t *testing.T) {
	generated := time.Date(2026, 8, 13, 0, 5, 0, 0, time.Local)
	snap := &protocol.MetricSnapshot{
		SchemaVersion: protocol.SchemaVersion, SourceEpoch: "e", SnapshotVersion: 1,
		GeneratedAt: generated, SourceNode: "node",
	}
	build := func(now time.Time) string {
		return ui.RenderString(ui.Build(snap, ui.BuildOptions{
			Page: string(ui.PageHomeLab), Now: now, Connected: true, RotationEnabled: true,
		}))
	}
	first := build(generated.Add(10 * time.Second))
	second := build(generated.Add(40 * time.Second))
	if first != second {
		t.Fatal("unchanged Phase 3 frame was modified by second-level sync age")
	}
	third := build(generated.Add(70 * time.Second))
	if third == second || !strings.Contains(third, "SYNCED 1M AGO") {
		t.Fatal("Phase 3 sync age did not advance on the next minute")
	}
}

func TestPhase3HomeLabWindowsRotateAndCriticalItemsStayOnFirstScreen(t *testing.T) {
	vm := baseModel()
	vm.RotationEnabled, vm.HasSnapshot = true, true
	vm.Page, vm.DwellSeconds = string(ui.PageHomeLab), 15
	vm.Now = time.Unix(0, 0).UTC()
	for _, name := range []string{"node-one", "node-two", "node-three", "node-four", "node-five", "node-six"} {
		vm.Nodes = append(vm.Nodes, ui.NodeCard{Name: name, Status: protocol.DisplayOK})
	}
	first := ui.RenderString(vm)
	if !strings.Contains(first, "HOME LAB 1/2") || !strings.Contains(first, "node-one") || strings.Contains(first, "node-five") {
		t.Fatalf("unexpected first node window:\n%s", first)
	}
	vm.Now = vm.Now.Add(15 * time.Second)
	second := ui.RenderString(vm)
	if !strings.Contains(second, "HOME LAB 2/2") || !strings.Contains(second, "node-five") || strings.Contains(second, "node-one") {
		t.Fatalf("unexpected second node window:\n%s", second)
	}

	vm.Nodes[5].Status = protocol.DisplayCrit
	critical := ui.RenderString(vm)
	if !strings.Contains(critical, "HOME LAB 1/2") || !strings.Contains(critical, "node-six") || !strings.Contains(critical, "CRIT") {
		t.Fatalf("critical node was not pinned to the first window:\n%s", critical)
	}
}

func TestPhase3CriticalServiceIsPinnedAheadOfConfiguredOrder(t *testing.T) {
	vm := baseModel()
	vm.RotationEnabled, vm.HasSnapshot = true, true
	vm.Page, vm.DwellSeconds = string(ui.PageServices), 15
	vm.Now = time.Unix(15, 0).UTC()
	for _, name := range []string{"service-one", "service-two", "service-three", "service-four", "service-five", "service-six"} {
		vm.Services = append(vm.Services, ui.ServiceCard{Name: name, Status: protocol.DisplayOK})
	}
	vm.Services[5].Status = protocol.DisplayCrit
	out := ui.RenderString(vm)
	if !strings.Contains(out, "SERVICES 1/2") || !strings.Contains(out, "service-six") || !strings.Contains(out, "CRIT") {
		t.Fatalf("critical service was not pinned to the first window:\n%s", out)
	}
}

func assertPhase3Grid(t *testing.T, frame string, rich bool) {
	t.Helper()
	if rich {
		frame = stripSGR(frame)
	}
	lines := strings.Split(frame, "\n")
	if len(lines) != ui.Rows {
		t.Fatalf("rows = %d", len(lines))
	}
	for row, line := range lines {
		if rich {
			if utf8.RuneCountInString(line) != ui.Cols {
				t.Fatalf("row %d cells = %d", row, utf8.RuneCountInString(line))
			}
		} else if len(line) != ui.Cols {
			t.Fatalf("row %d bytes = %d", row, len(line))
		}
	}
}
