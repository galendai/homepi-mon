package ui_test

import (
	"strings"
	"testing"
	"time"

	"github.com/galendai/homepi-mon/internal/decimal"
	"github.com/galendai/homepi-mon/internal/protocol"
	"github.com/galendai/homepi-mon/internal/ui"
)

func TestHomeRouterPrependsLandingPageWithoutChangingStoredConfig(t *testing.T) {
	config := ui.DefaultRotationConfig()
	start := time.Unix(0, 0)
	router := ui.NewHomeRouter(config, start)
	if len(config.Order) != 5 {
		t.Fatalf("stored page order mutated: %v", config.Order)
	}
	if page := router.Update(start); page != ui.PageHome {
		t.Fatalf("initial page = %s, want HOME", page)
	}
	if position, count := router.Position(); position != 1 || count != 6 {
		t.Fatalf("home position = %d/%d, want 1/6", position, count)
	}
	if page := router.Update(start.Add(ui.HomeDwell)); page != ui.PageCoding {
		t.Fatalf("page after home dwell = %s, want CODING", page)
	}
	router.ShowPage(ui.PageHome, 10*time.Second, start.Add(31*time.Second))
	if page := router.Update(start.Add(32 * time.Second)); page != ui.PageHome {
		t.Fatalf("remote HOME page = %s", page)
	}
}

func TestHomeOnlyPersistsAndAllowsTemporaryRemotePages(t *testing.T) {
	start := time.Unix(0, 0)
	router := ui.NewHomeRouter(ui.DefaultRotationConfig(), start)
	router.SetHomeOnly(start)
	for _, elapsed := range []time.Duration{0, time.Minute, time.Hour} {
		if page := router.Update(start.Add(elapsed)); page != ui.PageHome {
			t.Fatalf("home-only page after %s = %s", elapsed, page)
		}
	}
	if router.RotationEnabled() {
		t.Fatal("home-only left automatic rotation enabled")
	}
	router.ShowPage(ui.PageCoding, 10*time.Second, start.Add(time.Hour))
	if page := router.Update(start.Add(time.Hour + time.Second)); page != ui.PageCoding {
		t.Fatalf("temporary remote page = %s, want CODING", page)
	}
	if page := router.Update(start.Add(time.Hour + 11*time.Second)); page != ui.PageHome {
		t.Fatalf("expired remote page = %s, want HOME", page)
	}
	router.SetRotation(true, 5*time.Second, 20*time.Second, start.Add(time.Hour+12*time.Second))
	if page := router.Update(start.Add(time.Hour + 17*time.Second)); page != ui.PageCoding {
		t.Fatalf("explicit rotation restore = %s, want CODING", page)
	}
}

func TestPageOnlyHoldsCodingAndRestoresAfterRemotePages(t *testing.T) {
	start := time.Unix(0, 0)
	router := ui.NewHomeRouter(ui.DefaultRotationConfig(), start)
	router.SetPageOnly(ui.PageCoding, start)
	for _, elapsed := range []time.Duration{0, ui.HomeDwell, time.Hour} {
		if page := router.Update(start.Add(elapsed)); page != ui.PageCoding {
			t.Fatalf("page-only page after %s = %s", elapsed, page)
		}
	}
	if position, count := router.Position(); position != 2 || count != 6 {
		t.Fatalf("page-only position = %d/%d, want 2/6", position, count)
	}
	if router.RotationEnabled() {
		t.Fatal("page-only left automatic rotation enabled")
	}
	router.ShowPage(ui.PageAPI, 10*time.Second, start.Add(time.Hour))
	if page := router.Update(start.Add(time.Hour + time.Second)); page != ui.PageAPI {
		t.Fatalf("temporary remote page = %s, want API", page)
	}
	if page := router.Update(start.Add(time.Hour + 11*time.Second)); page != ui.PageCoding {
		t.Fatalf("expired remote page = %s, want CODING", page)
	}
	router.SetRotation(true, 5*time.Second, 20*time.Second, start.Add(time.Hour+12*time.Second))
	if page := router.Update(start.Add(time.Hour + 17*time.Second)); page != ui.PageAPI {
		t.Fatalf("explicit rotation restore = %s, want API", page)
	}
}

func TestRotationPageSubsetRotatesCodingAndAPI(t *testing.T) {
	persistent := ui.DefaultRotationConfig()
	config, err := ui.SelectRotationPages(persistent, "CODING,API")
	if err != nil {
		t.Fatal(err)
	}
	if len(persistent.Order) != 5 || len(config.Order) != 2 {
		t.Fatalf("persistent=%v selected=%v", persistent.Order, config.Order)
	}
	start := time.Unix(0, 0)
	router := ui.NewSubsetRouter(config, start)
	for _, step := range []struct {
		at       time.Duration
		page     ui.Page
		position int
	}{
		{0, ui.PageCoding, 1}, {15 * time.Second, ui.PageAPI, 2}, {30 * time.Second, ui.PageCoding, 1},
	} {
		if page := router.Update(start.Add(step.at)); page != step.page {
			t.Fatalf("page at %s = %s, want %s", step.at, page, step.page)
		}
		if position, count := router.Position(); position != step.position || count != 2 {
			t.Fatalf("position at %s = %d/%d, want %d/2", step.at, position, count, step.position)
		}
	}
}

func TestRotationSubsetRotatesHomeCodingAndAPI(t *testing.T) {
	config, err := ui.SelectRotationPages(ui.DefaultRotationConfig(), "HOME,CODING,API")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Unix(0, 0)
	router := ui.NewSubsetRouter(config, start)
	for _, step := range []struct {
		at       time.Duration
		page     ui.Page
		position int
	}{
		{0, ui.PageHome, 1}, {ui.HomeDwell, ui.PageCoding, 2},
		{ui.HomeDwell + 15*time.Second, ui.PageAPI, 3}, {ui.HomeDwell + 30*time.Second, ui.PageHome, 1},
	} {
		if page := router.Update(start.Add(step.at)); page != step.page {
			t.Fatalf("page at %s = %s, want %s", step.at, page, step.page)
		}
		if position, count := router.Position(); position != step.position || count != 3 {
			t.Fatalf("position at %s = %d/%d, want %d/3", step.at, position, count, step.position)
		}
	}
}

func TestRotationSubsetWithoutCodingStartsAtSelectedPage(t *testing.T) {
	for _, selection := range []string{"API,SYSTEM", "API"} {
		config, err := ui.SelectRotationPages(ui.DefaultRotationConfig(), selection)
		if err != nil {
			t.Fatal(err)
		}
		start := time.Unix(0, 0)
		router := ui.NewSubsetRouter(config, start)
		if page := router.Update(start); page != ui.PageAPI {
			t.Fatalf("%s initial page = %s, want API", selection, page)
		}
		if position, _ := router.Position(); position != 1 {
			t.Fatalf("%s initial position = %d, want 1", selection, position)
		}
		router.ShowPage(ui.PageSystem, 10*time.Second, start)
		if page := router.Update(start.Add(time.Second)); page != ui.PageSystem {
			t.Fatalf("%s remote page = %s, want SYSTEM", selection, page)
		}
		if page := router.Update(start.Add(11 * time.Second)); page != ui.PageAPI {
			t.Fatalf("%s did not recover selected page: %s", selection, page)
		}
	}
}

func TestHomeRendersRealWeatherAndFixedGrid(t *testing.T) {
	now := time.Date(2026, time.November, 24, 14, 32, 0, 0, time.Local)
	temperature, high, low := 22, 26, 18
	humidity, wind := 42, 8
	age := 30 * time.Second
	vm := ui.ViewModel{
		NodeLabel: "GALEN-MAC", Page: string(ui.PageHome), Now: now,
		Link: protocol.LinkLive, HasSnapshot: true, SyncedAgo: &age,
		PageNumber: 1, PageCount: 6,
		Weather: &protocol.WeatherSummary{
			Location: "BEIJING", TemperatureC: &temperature,
			HighTemperatureC: &high, LowTemperatureC: &low,
			HumidityPercent: &humidity, WindSpeedKPH: &wind,
			Condition: "CLEAR", ObservedAt: now.Add(-time.Minute), Status: protocol.StatusOK,
		},
	}
	model := ui.BuildHomeModel(vm)
	if model.TemperatureText != "22°C" || model.HighText != "H 26°C" || model.LowText != "L 18°C" {
		t.Fatalf("temperature model = %+v", model)
	}
	frame := ui.RenderString(vm)
	for _, token := range []string{"14:32", "TUE, NOV 24", "22C", "CLEAR", "H 26C", "HUMIDITY 42%", "WIND 8 KM/H", "BEIJING"} {
		if !strings.Contains(frame, token) {
			t.Errorf("home frame missing %q:\n%s", token, frame)
		}
	}
	lines := strings.Split(frame, "\n")
	if len(lines) != ui.Rows {
		t.Fatalf("home rows = %d, want %d", len(lines), ui.Rows)
	}
	for _, line := range lines {
		if len(line) != ui.Cols {
			t.Fatalf("home line width = %d, want %d: %q", len(line), ui.Cols, line)
		}
	}
}

func TestHomeWeatherMissingAndStaleAreExplicit(t *testing.T) {
	now := time.Unix(3600, 0)
	missing := ui.RenderString(ui.ViewModel{Page: string(ui.PageHome), Now: now})
	if !strings.Contains(missing, "UNAVAILABLE") || !strings.Contains(missing, "NO WEATHER DATA") {
		t.Fatalf("missing weather was not explicit:\n%s", missing)
	}
	temperature := 22
	stale := ui.BuildHomeModel(ui.ViewModel{
		Page: string(ui.PageHome), Now: now,
		Weather: &protocol.WeatherSummary{
			TemperatureC: &temperature, Condition: "CLEAR", ObservedAt: now.Add(-16 * time.Minute),
			StaleAfter: protocol.Duration(15 * time.Minute), Status: protocol.StatusOK,
		},
	})
	if stale.TemperatureText != "22°C" || !strings.HasPrefix(stale.ConditionText, "STALE") {
		t.Fatalf("stale weather model = %+v", stale)
	}
}

func TestHomeTitleDoesNotUseNodeLabel(t *testing.T) {
	model := ui.BuildHomeModel(ui.ViewModel{NodeLabel: "主力电脑"})
	if model.TitleText != "HOME" {
		t.Fatalf("HOME title = %q", model.TitleText)
	}
}

func TestHomeWeatherDetailOnlyShowsFreshMeasurements(t *testing.T) {
	now := at("12:00")
	for _, tt := range []struct {
		name           string
		humidity, wind *int
		age            time.Duration
		status         protocol.MetricStatus
		err            protocol.ErrorClass
		want           string
	}{
		{"both", pct(56), pct(8), time.Minute, protocol.StatusOK, "", "RH 56%    WIND 8 km/h"},
		{"calm", nil, pct(0), 0, protocol.StatusOK, "", "WIND 0 km/h"},
		{"dry", pct(0), nil, 0, protocol.StatusOK, "", "RH 0%"},
		{"missing", nil, nil, 0, protocol.StatusOK, "", ""},
		{"expired", pct(56), pct(8), time.Minute + time.Second, protocol.StatusOK, "", ""},
		{"marked stale", pct(56), pct(8), 0, protocol.StatusStale, "", ""},
		{"collection error", pct(56), pct(8), 0, protocol.StatusOK, protocol.ErrTimeout, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			w := &protocol.WeatherSummary{HumidityPercent: tt.humidity, WindSpeedKPH: tt.wind,
				ObservedAt: now.Add(-tt.age), StaleAfter: protocol.Duration(time.Minute), Status: tt.status, ErrorClass: tt.err}
			if got := ui.BuildHomeModel(ui.ViewModel{Now: now, Weather: w}).WeatherDetail; got != tt.want {
				t.Fatalf("weather detail = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHomeMonitorUsesActualAlertWindowAndRecoversToSelectedProxy(t *testing.T) {
	now := at("12:00")
	on := true
	limit, low, fine := decimal.MustParse("100"), decimal.MustParse("8"), decimal.MustParse("95")
	main := protocol.ProviderMetric{Provider: "openai", DisplayName: "Codex", Group: ui.GroupCoding,
		Window: protocol.WindowRolling5h, MetricKind: protocol.KindQuota, Value: &fine, Limit: &limit,
		ObservedAt: now, Precision: protocol.PrecisionVerified}
	weekly := main
	weekly.Window, weekly.Value = protocol.WindowWeekly, &low
	nodes := make([]protocol.OpenClashNode, ui.OpenClashCardLimit+1)
	nodes[0] = protocol.OpenClashNode{Name: "other", CheckedAt: &now, Alive: &on, DelayMS: pct(1)}
	nodes[len(nodes)-1] = protocol.OpenClashNode{Name: "selected", CheckedAt: &now, Alive: &on, DelayMS: pct(1042)}
	snap := &protocol.MetricSnapshot{GeneratedAt: now, Metrics: []protocol.ProviderMetric{main, weekly},
		OpenClash: &protocol.OpenClashReport{ObservedAt: now, Status: protocol.StatusOK, SelectedNode: "selected",
			DelayStaleAfter: protocol.Duration(time.Minute), Nodes: nodes}}
	opts := ui.BuildOptions{Connected: true, Now: now, Thresholds: protocol.DefaultThresholds()}
	check := func(name, text string, status protocol.DisplayStatus) {
		t.Helper()
		got := ui.BuildHomeModel(ui.Build(snap, opts)).Monitor
		if got.Source != name || got.Detail != text || got.Status != status {
			t.Fatalf("monitor = %+v, want %q %q %s", got, name, text, status)
		}
	}
	check("CODEX", "WEEK   8% LEFT", protocol.DisplayCrit)
	snap.Metrics[1].Precision = protocol.PrecisionEstimated
	check("CODEX", "WEEK   ~8% LEFT", protocol.DisplayWarn)
	snap.Metrics[1].ErrorClass = protocol.ErrAuth
	check("CODEX", "AUTH", protocol.DisplayAuth)
	snap.Metrics[1].ErrorClass = protocol.ErrNone
	snap.Metrics[1].Value = &fine
	check("PROXY", "1042 ms", protocol.DisplayOK)
	opts.Now = now.Add(time.Minute + time.Second)
	check("PROXY", "STALE", protocol.DisplayNA)
	opts.Now = now
	on = false
	check("PROXY", "FAIL", protocol.DisplayCrit)
	snap.OpenClash.ErrorClass = protocol.ErrAuth
	check("PROXY", "AUTH", protocol.DisplayAuth)
	snap.OpenClash.ErrorClass = protocol.ErrNone
	snap.OpenClash.SelectedNode = "absent"
	check("PROXY", "--", protocol.DisplayNA)
	opts.Connected = false
	check("", "", "")
}

func TestHomeMonitorChoosesCriticalServiceOverBalanceWarning(t *testing.T) {
	now := at("12:00")
	balance, threshold := decimal.MustParse("8.2"), decimal.MustParse("10")
	snap := &protocol.MetricSnapshot{GeneratedAt: now, Metrics: []protocol.ProviderMetric{{
		Provider: "deepseek", DisplayName: "DeepSeek", Group: ui.GroupAPI, MetricKind: protocol.KindBalance,
		Value: &balance, Unit: "CNY", Precision: protocol.PrecisionVerified, ObservedAt: now,
	}}, HomeLabServices: []protocol.HomeLabService{{Name: "Prometheus", Status: protocol.StatusCritical, FiringAlerts: pct(2), ObservedAt: now}}}
	opts := ui.BuildOptions{Connected: true, Now: now, Thresholds: protocol.Thresholds{WarnAmount: &threshold}}
	if got := ui.BuildHomeModel(ui.Build(snap, opts)).Monitor; got.Detail != "2 FIRING" || got.Source != "PROMETHEUS" {
		t.Fatalf("critical service monitor = %+v", got)
	}
	snap.HomeLabServices = nil
	if got := ui.BuildHomeModel(ui.Build(snap, opts)).Monitor; got.Detail != "CNY 8.20" || got.Status != protocol.DisplayWarn {
		t.Fatalf("balance monitor = %+v", got)
	}
}
