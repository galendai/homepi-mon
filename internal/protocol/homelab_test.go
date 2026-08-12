package protocol_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/galendai/homepi-mon/internal/protocol"
)

func boolPtr(v bool) *bool { return &v }
func intPtr(v int) *int    { return &v }

func TestHomeLabSnapshotValidatesAndClonesPointers(t *testing.T) {
	now := time.Now().UTC()
	s := &protocol.MetricSnapshot{
		SchemaVersion: protocol.SchemaVersion, SourceEpoch: "epoch", SourceNode: "node",
		GeneratedAt: now,
		HomeLabNodes: []protocol.HomeLabNode{{
			ID: "nas", Name: "NAS", Online: boolPtr(true), CPUPercent: intPtr(28),
			ObservedAt: now, Status: protocol.StatusOK,
		}},
		HomeLabServices: []protocol.HomeLabService{{
			ID: "grafana", Name: "Grafana", Kind: "grafana", Healthy: boolPtr(true),
			FiringAlerts: intPtr(0), ObservedAt: now, Status: protocol.StatusOK,
		}},
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	clone := s.Clone()
	*clone.HomeLabNodes[0].CPUPercent = 99
	*clone.HomeLabServices[0].FiringAlerts = 3
	if *s.HomeLabNodes[0].CPUPercent != 28 || *s.HomeLabServices[0].FiringAlerts != 0 {
		t.Fatal("clone shares HomeLab pointers with source")
	}
}

func TestHomeLabSnapshotRejectsUnsafeOrUnboundedData(t *testing.T) {
	now := time.Now().UTC()
	base := protocol.MetricSnapshot{
		SchemaVersion: protocol.SchemaVersion, SourceEpoch: "epoch", SourceNode: "node", GeneratedAt: now,
	}
	tests := []struct {
		name   string
		mutate func(*protocol.MetricSnapshot)
		want   string
	}{
		{"control", func(s *protocol.MetricSnapshot) {
			s.HomeLabNodes = []protocol.HomeLabNode{{ID: "node-1", Name: "bad\x1b", ObservedAt: now, Status: protocol.StatusOK}}
		}, "control character"},
		{"percent", func(s *protocol.MetricSnapshot) {
			s.HomeLabNodes = []protocol.HomeLabNode{{ID: "node-1", Name: "node", CPUPercent: intPtr(101), ObservedAt: now, Status: protocol.StatusOK}}
		}, "0..100"},
		{"negative count", func(s *protocol.MetricSnapshot) {
			s.HomeLabServices = []protocol.HomeLabService{{ID: "p", Name: "P", Kind: "portainer", Stacks: intPtr(-1), ObservedAt: now, Status: protocol.StatusOK}}
		}, "must not be negative"},
		{"duplicate", func(s *protocol.MetricSnapshot) {
			s.HomeLabServices = []protocol.HomeLabService{
				{ID: "p", Name: "P", Kind: "portainer", ObservedAt: now, Status: protocol.StatusOK},
				{ID: "p", Name: "P2", Kind: "portainer", ObservedAt: now, Status: protocol.StatusOK},
			}
		}, "duplicate id"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := base
			tc.mutate(&s)
			if err := s.Validate(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestSchemaOneZeroIgnoresOneOneHomeLabFields(t *testing.T) {
	raw := `{"schema_version":"1.0","source_epoch":"e","snapshot_version":1,"generated_at":"2026-08-12T00:00:00Z","source_node":"n","metrics":[],"connector_health":[],"future_homelab":{"x":1}}`
	var s protocol.MetricSnapshot
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		t.Fatal(err)
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestMaximumHomeLabSnapshotStaysWithinPiBudget(t *testing.T) {
	now := time.Now().UTC()
	s := &protocol.MetricSnapshot{
		SchemaVersion: protocol.SchemaVersion, SourceEpoch: "epoch", SourceNode: "node", GeneratedAt: now,
	}
	for i := 0; i < protocol.MaxHomeLabNodes; i++ {
		s.HomeLabNodes = append(s.HomeLabNodes, protocol.HomeLabNode{
			ID: fmt.Sprintf("node-%03d", i), Name: strings.Repeat("n", 64), Online: boolPtr(true),
			CPUPercent: intPtr(100), MemoryPercent: intPtr(100), DiskPercent: intPtr(100),
			ObservedAt: now, StaleAfter: protocol.Duration(5 * time.Minute), Status: protocol.StatusWarning,
			Message: strings.Repeat("m", 160),
		})
	}
	for i := 0; i < protocol.MaxHomeLabServices; i++ {
		s.HomeLabServices = append(s.HomeLabServices, protocol.HomeLabService{
			ID: fmt.Sprintf("service-%03d", i), Name: strings.Repeat("s", 64), Kind: "portainer",
			Version: strings.Repeat("v", 64), APIGeneration: strings.Repeat("a", 32), Healthy: boolPtr(true),
			FiringAlerts: intPtr(1000), EnvironmentsTotal: intPtr(1000), EnvironmentsOnline: intPtr(1000),
			ContainersRunning: intPtr(1000), ContainersStopped: intPtr(1000), ContainersFailed: intPtr(1000),
			Stacks: intPtr(1000), ObservedAt: now, StaleAfter: protocol.Duration(5 * time.Minute),
			Status: protocol.StatusWarning, Message: strings.Repeat("m", 160),
		})
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	const maxSnapshotBytes = 256 * 1024
	if len(raw) > maxSnapshotBytes {
		t.Fatalf("maximum HomeLab snapshot = %d bytes, want <= %d", len(raw), maxSnapshotBytes)
	}
	t.Logf("maximum HomeLab snapshot = %d bytes", len(raw))
}
