package protocol_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/galendai/homepi-mon/internal/protocol"
)

func openClashReport() protocol.OpenClashReport {
	checkedAt := ts("2026-11-25T10:00:00Z")
	return protocol.OpenClashReport{
		ObservedAt: checkedAt, StaleAfter: protocol.Duration(15 * time.Second),
		DelayStaleAfter: protocol.Duration(100 * time.Second), SelectedGroup: "🚀 PROXY",
		SelectedNode: "香港 01", TotalConnections: 3, Status: protocol.StatusOK,
		Nodes: []protocol.OpenClashNode{{
			Name: "香港 01", Alive: new(true), DelayMS: new(123), CheckedAt: &checkedAt, ActiveConnections: 3,
		}},
	}
}

func TestOpenClashSnapshotRoundTripAndClone(t *testing.T) {
	if protocol.SchemaVersion != "1.3" {
		t.Fatalf("SchemaVersion = %q, want 1.3", protocol.SchemaVersion)
	}
	snapshot := sampleSnapshot()
	report := openClashReport()
	snapshot.OpenClash = &report
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := protocol.DecodeSnapshot(raw)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.OpenClash == nil || decoded.OpenClash.SelectedNode != "香港 01" ||
		*decoded.OpenClash.Nodes[0].DelayMS != 123 {
		t.Fatalf("OpenClash round trip = %+v", decoded.OpenClash)
	}
	clone := decoded.Clone()
	*clone.OpenClash.Nodes[0].DelayMS = 999
	*clone.OpenClash.Nodes[0].Alive = false
	*clone.OpenClash.Nodes[0].CheckedAt = clone.OpenClash.Nodes[0].CheckedAt.Add(time.Hour)
	clone.OpenClash.Nodes[0].Name = "changed"
	if *decoded.OpenClash.Nodes[0].DelayMS != 123 || !*decoded.OpenClash.Nodes[0].Alive ||
		decoded.OpenClash.Nodes[0].Name != "香港 01" ||
		!decoded.OpenClash.Nodes[0].CheckedAt.Equal(ts("2026-11-25T10:00:00Z")) {
		t.Fatal("OpenClash clone aliases the source snapshot")
	}
}

func TestOpenClashValidationBoundsAndDeduplicates(t *testing.T) {
	valid := openClashReport()
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(*protocol.OpenClashReport)
	}{
		{"too many nodes", func(r *protocol.OpenClashReport) {
			r.Nodes = make([]protocol.OpenClashNode, protocol.MaxOpenClashNodes+1)
		}},
		{"duplicate names", func(r *protocol.OpenClashReport) {
			r.Nodes = append(r.Nodes, r.Nodes[0])
		}},
		{"unsafe name", func(r *protocol.OpenClashReport) { r.Nodes[0].Name = "bad\nname" }},
		{"long group", func(r *protocol.OpenClashReport) { r.SelectedGroup = strings.Repeat("x", 129) }},
		{"negative connections", func(r *protocol.OpenClashReport) { r.TotalConnections = -1 }},
		{"negative node connections", func(r *protocol.OpenClashReport) { r.Nodes[0].ActiveConnections = -1 }},
		{"negative delay", func(r *protocol.OpenClashReport) { r.Nodes[0].DelayMS = new(-1) }},
		{"alive without measurement time", func(r *protocol.OpenClashReport) { r.Nodes[0].CheckedAt = nil }},
		{"delay without alive state", func(r *protocol.OpenClashReport) { r.Nodes[0].Alive = nil }},
		{"delay on failed node", func(r *protocol.OpenClashReport) { r.Nodes[0].Alive = new(false) }},
		{"missing observed time", func(r *protocol.OpenClashReport) { r.ObservedAt = time.Time{} }},
		{"invalid status", func(r *protocol.OpenClashReport) { r.Status = protocol.MetricStatus("bad") }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			report := openClashReport()
			test.mutate(&report)
			if err := report.Validate(); err == nil {
				t.Fatal("invalid OpenClash report accepted")
			}
		})
	}
}
