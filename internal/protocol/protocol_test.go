package protocol_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/galendai/homepi-mon/internal/decimal"
	"github.com/galendai/homepi-mon/internal/protocol"
)

func ts(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func dec(s string) *decimal.Decimal {
	d := decimal.MustParse(s)
	return &d
}

func sampleSnapshot() *protocol.MetricSnapshot {
	observed := ts("2026-08-10T06:32:00Z")
	reset := ts("2026-08-10T08:32:00Z")
	return &protocol.MetricSnapshot{
		SchemaVersion:   protocol.SchemaVersion,
		SourceEpoch:     "0d3c7a1e-8f2b-4c11-9b6d-5a2e0f7c9d41",
		SnapshotVersion: 7,
		GeneratedAt:     ts("2026-08-10T06:32:05Z"),
		SourceNode:      "dev-mac",
		SourceNodeLabel: "DEV-MAC",
		Metrics: []protocol.ProviderMetric{
			{
				ID:           "minimax.coding.5h",
				Provider:     "minimax",
				AccountLabel: "primary",
				DisplayName:  "MiniMax",
				MetricKind:   protocol.KindQuota,
				Value:        dec("68"),
				Limit:        dec("100"),
				Unit:         "percent",
				Window:       protocol.WindowRolling5h,
				ResetsAt:     &reset,
				ObservedAt:   observed,
				Precision:    protocol.PrecisionExact,
				SourceKind:   protocol.SourceOfficialAPI,
				Status:       protocol.StatusOK,
				Derived:      []string{"remaining", "percent"},
				Group:        "coding",
			},
			{
				ID:           "deepseek.api.balance",
				Provider:     "deepseek",
				AccountLabel: "primary",
				DisplayName:  "DeepSeek API",
				MetricKind:   protocol.KindBalance,
				Value:        dec("8.20"),
				Unit:         "CNY",
				Window:       protocol.WindowPrepaid,
				ObservedAt:   observed,
				Precision:    protocol.PrecisionExact,
				SourceKind:   protocol.SourceOfficialAPI,
				Status:       protocol.StatusWarning,
				Group:        "api",
			},
		},
		ConnectorHealth: []protocol.ConnectorHealth{
			{
				ConnectorID: "minimax-coding",
				Provider:    "minimax",
				Enabled:     true,
				State:       protocol.ConnOK,
			},
		},
	}
}

// U-P001: serialisation round-trip keeps every field, including exact decimals.
func TestSnapshotJSONRoundTrip(t *testing.T) {
	in := sampleSnapshot()
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out, err := protocol.DecodeSnapshot(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.SnapshotVersion != in.SnapshotVersion || out.SourceEpoch != in.SourceEpoch {
		t.Fatalf("epoch/version lost: %+v", out)
	}
	if len(out.Metrics) != 2 {
		t.Fatalf("want 2 metrics, got %d", len(out.Metrics))
	}
	if got := out.Metrics[1].Value.String(); got != "8.20" {
		t.Errorf("balance = %q, want 8.20 (scale preserved)", got)
	}
	if !out.Metrics[0].ResetsAt.Equal(*in.Metrics[0].ResetsAt) {
		t.Error("resets_at lost")
	}
}

func TestMonetaryMetricsMarshalWithExactlyTwoDecimals(t *testing.T) {
	tests := []struct {
		name      string
		kind      protocol.MetricKind
		value     string
		limit     string
		wantValue string
		wantLimit string
	}{
		{name: "balance rounds", kind: protocol.KindBalance, value: "12.345", wantValue: `"12.35"`},
		{name: "cost pads and rounds limit", kind: protocol.KindCost, value: "7", limit: "9.999", wantValue: `"7.00"`, wantLimit: `"10.00"`},
		{name: "quota keeps precision", kind: protocol.KindQuota, value: "12.345", limit: "100", wantValue: `"12.345"`, wantLimit: `"100"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			metric := protocol.ProviderMetric{MetricKind: tc.kind, Value: dec(tc.value)}
			if tc.limit != "" {
				metric.Limit = dec(tc.limit)
			}
			raw, err := json.Marshal(metric)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(raw, &fields); err != nil {
				t.Fatal(err)
			}
			if got := string(fields["value"]); got != tc.wantValue {
				t.Errorf("value = %s, want %s", got, tc.wantValue)
			}
			if tc.wantLimit == "" {
				if _, ok := fields["limit"]; ok {
					t.Error("limit must remain omitted")
				}
			} else if got := string(fields["limit"]); got != tc.wantLimit {
				t.Errorf("limit = %s, want %s", got, tc.wantLimit)
			}
			if got := metric.Value.String(); got != tc.value {
				t.Errorf("marshal mutated exact value to %s", got)
			}
		})
	}
}

// U-P002: unknown optional fields from a newer minor schema are ignored, not
// fatal (HL-Spec 6.3).
func TestDecodeIgnoresUnknownFields(t *testing.T) {
	raw, err := json.Marshal(sampleSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatal(err)
	}
	generic["future_field"] = "hello"
	metrics := generic["metrics"].([]any)
	metrics[0].(map[string]any)["future_metric_field"] = 42
	patched, err := json.Marshal(generic)
	if err != nil {
		t.Fatal(err)
	}

	out, err := protocol.DecodeSnapshot(patched)
	if err != nil {
		t.Fatalf("decode with unknown fields: %v", err)
	}
	if len(out.Metrics) != 2 {
		t.Errorf("metrics dropped: %d", len(out.Metrics))
	}
}

// U-P003: an unsupported major schema version is refused outright.
func TestDecodeRejectsIncompatibleMajor(t *testing.T) {
	snap := sampleSnapshot()
	snap.SchemaVersion = "2.0"
	raw, _ := json.Marshal(snap)

	_, err := protocol.DecodeSnapshot(raw)
	if !errors.Is(err, protocol.ErrSchemaMajor) {
		t.Fatalf("want ErrSchemaMajor, got %v", err)
	}
}

// U-P004 (Test-Module-001 U010): within one epoch an older version must never
// overwrite a newer one.
func TestSupersedesRejectsVersionRegression(t *testing.T) {
	v11 := sampleSnapshot()
	v11.SnapshotVersion = 11
	v10 := sampleSnapshot()
	v10.SnapshotVersion = 10

	if ok, err := protocol.Supersedes(v11, v10); ok || !errors.Is(err, protocol.ErrVersionRegressed) {
		t.Fatalf("v10 must not supersede v11: ok=%v err=%v", ok, err)
	}
	if ok, err := protocol.Supersedes(v10, v11); !ok || err != nil {
		t.Fatalf("v11 must supersede v10: ok=%v err=%v", ok, err)
	}
	if ok, err := protocol.Supersedes(v11, v11); ok || err == nil {
		t.Fatal("equal versions must not supersede")
	}
}

// U-P005 (Test-Module-001 U018): a daemon restart yields a new epoch whose
// version legitimately restarts at zero.
func TestSupersedesAcceptsNewEpochFromZero(t *testing.T) {
	old := sampleSnapshot()
	old.SnapshotVersion = 42

	restarted := sampleSnapshot()
	restarted.SourceEpoch = "9f1b2c3d-4e5f-4a6b-8c9d-0e1f2a3b4c5d"
	restarted.SnapshotVersion = 0

	ok, err := protocol.Supersedes(old, restarted)
	if !ok || err != nil {
		t.Fatalf("new epoch must be accepted: ok=%v err=%v", ok, err)
	}
}

// U-P006 (Test-Module-001 U020, MOD-002 U016): a snapshot from an unbound node
// is rejected instead of merged.
func TestSourceBindingRejectsForeignNode(t *testing.T) {
	bound := protocol.SourceBinding{NodeID: "dev-mac"}
	if err := bound.Accept(sampleSnapshot()); err != nil {
		t.Fatalf("bound node rejected: %v", err)
	}

	foreign := sampleSnapshot()
	foreign.SourceNode = "other-pc"
	err := bound.Accept(foreign)
	if !errors.Is(err, protocol.ErrWrongSource) {
		t.Fatalf("want ErrWrongSource, got %v", err)
	}
}

// U-P007 (Test-Module-001 U005): with no upstream limit there is no percentage.
func TestPercentUndefinedWithoutLimit(t *testing.T) {
	m := sampleSnapshot().Metrics[0]
	m.Limit = nil
	if _, ok := m.Percent(); ok {
		t.Fatal("percent must be undefined when limit is unknown")
	}
	if got := protocol.DefaultThresholds().Severity(m); got != protocol.StatusUnknown {
		t.Fatalf("severity without limit = %v, want unknown", got)
	}
}

// U-P008 (Test-Module-001 U006): times are stored as UTC and survive a
// round-trip so the display can localise them.
func TestTimesRoundTripAsUTC(t *testing.T) {
	snap := sampleSnapshot()
	raw, _ := json.Marshal(snap)
	out, err := protocol.DecodeSnapshot(raw)
	if err != nil {
		t.Fatal(err)
	}
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	got := out.Metrics[0].ResetsAt.In(shanghai).Format("15:04")
	if got != "16:32" {
		t.Errorf("08:32Z in Asia/Shanghai = %s, want 16:32", got)
	}
}

// U-P009: structural validation names the offending field and rejects control
// characters and unknown enum members.
func TestValidateRejectsBadPayloads(t *testing.T) {
	cases := map[string]func(*protocol.MetricSnapshot){
		"empty source node":     func(s *protocol.MetricSnapshot) { s.SourceNode = "" },
		"unknown metric kind":   func(s *protocol.MetricSnapshot) { s.Metrics[0].MetricKind = "vibes" },
		"unknown precision":     func(s *protocol.MetricSnapshot) { s.Metrics[0].Precision = "probably" },
		"unknown window":        func(s *protocol.MetricSnapshot) { s.Metrics[0].Window = "fortnight" },
		"control char in name":  func(s *protocol.MetricSnapshot) { s.Metrics[0].DisplayName = "Kimi\x1b[31m" },
		"control char in label": func(s *protocol.MetricSnapshot) { s.SourceNodeLabel = "DEV\x07MAC" },
		"duplicate metric id":   func(s *protocol.MetricSnapshot) { s.Metrics[1].ID = s.Metrics[0].ID },
		"missing observed_at":   func(s *protocol.MetricSnapshot) { s.Metrics[0].ObservedAt = time.Time{} },
		"missing unit":          func(s *protocol.MetricSnapshot) { s.Metrics[0].Unit = "" },
		"bad connector state":   func(s *protocol.MetricSnapshot) { s.ConnectorHealth[0].State = "vibing" },
	}
	for name, mutate := range cases {
		snap := sampleSnapshot()
		mutate(snap)
		if err := snap.Validate(); err == nil {
			t.Errorf("%s: expected validation error", name)
		}
	}
}

// S-P001 (Test-Module-001 S001): the serialised protocol carries no credential
// field and no planted secret value.
func TestSerialisedSnapshotCarriesNoSecrets(t *testing.T) {
	const plantedKey = "sk-kimi-THIS-MUST-NEVER-APPEAR"

	snap := sampleSnapshot()
	// Simulate a connector that tried to surface a raw upstream error.
	snap.ConnectorHealth[0].Message = "re-authenticate with the official CLI on dev-mac"
	snap.Metrics[0].Message = "rolling window"

	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	if hits := protocol.AuditJSON(raw, []string{plantedKey, "bearer "}); len(hits) != 0 {
		t.Fatalf("redaction audit found: %v", hits)
	}
}

// S-P002: the audit itself must actually catch a leak, otherwise S-P001 proves
// nothing.
func TestAuditJSONDetectsLeaks(t *testing.T) {
	leaky := []byte(`{"metrics":[{"id":"x","authorization":"Bearer abc"}],"note":"key sk-live-123"}`)
	hits := protocol.AuditJSON(leaky, []string{"sk-live-123"})
	if len(hits) != 2 {
		t.Fatalf("want 2 hits, got %v", hits)
	}
}
