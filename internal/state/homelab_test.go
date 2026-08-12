package state_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/galendai/homepi-mon/internal/protocol"
	"github.com/galendai/homepi-mon/internal/state"
)

func boolValue(v bool) *bool { return &v }
func intValue(v int) *int    { return &v }

func TestHomeLabOfflineNodePreservesValuesAndConnectorFailureDoesNotClaimDown(t *testing.T) {
	now := time.Now().UTC()
	store, err := state.New(state.Options{NodeID: "source", Epoch: "epoch", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyConnectorHomeLab("prom", protocol.HomeLabReport{
		Nodes: []protocol.HomeLabNode{{ID: "nas", Name: "NAS", Online: boolValue(true), CPUPercent: intValue(28), ObservedAt: now, Status: protocol.StatusOK}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyConnectorHomeLab("prom", protocol.HomeLabReport{
		Nodes: []protocol.HomeLabNode{{ID: "nas", Name: "NAS", Online: boolValue(false), ObservedAt: now.Add(time.Minute), Status: protocol.StatusCritical}},
	}); err != nil {
		t.Fatal(err)
	}
	node := store.Snapshot().HomeLabNodes[0]
	if node.CPUPercent == nil || *node.CPUPercent != 28 || node.Online == nil || *node.Online {
		t.Fatalf("offline merge = %+v", node)
	}
	if _, err := store.ApplyConnectorError("prom", protocol.ErrNetwork, "service unavailable"); err != nil {
		t.Fatal(err)
	}
	node = store.Snapshot().HomeLabNodes[0]
	if node.ErrorClass != protocol.ErrNetwork || node.Online == nil || *node.Online {
		t.Fatalf("network annotation = %+v", node)
	}
}

func TestHomeLabSuccessfulReportReplacesOnlyConnectorOwnedEntities(t *testing.T) {
	now := time.Now().UTC()
	store, _ := state.New(state.Options{NodeID: "source", Epoch: "epoch"})
	service := func(id string) protocol.HomeLabService {
		return protocol.HomeLabService{ID: id, Name: id, Kind: id, ObservedAt: now, Status: protocol.StatusOK}
	}
	if err := store.ApplyConnectorHomeLab("a", protocol.HomeLabReport{Services: []protocol.HomeLabService{service("one"), service("two")}}); err != nil {
		t.Fatal(err)
	}
	if !store.HasData() {
		t.Fatal("HomeLab-only state is not publishable")
	}
	if err := store.ApplyConnectorHomeLab("b", protocol.HomeLabReport{Services: []protocol.HomeLabService{service("three")}}); err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyConnectorHomeLab("a", protocol.HomeLabReport{Services: []protocol.HomeLabService{service("two")}}); err != nil {
		t.Fatal(err)
	}
	got := store.Snapshot().HomeLabServices
	if len(got) != 2 || got[0].ID != "three" || got[1].ID != "two" {
		t.Fatalf("services = %+v", got)
	}
}

func TestHomeLabRejectsMergedLimitAndCrossConnectorIDCollision(t *testing.T) {
	store, err := state.New(state.Options{NodeID: "source", Epoch: "epoch"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	nodes := func(prefix string, count int) []protocol.HomeLabNode {
		out := make([]protocol.HomeLabNode, count)
		for i := range out {
			out[i] = protocol.HomeLabNode{
				ID: fmt.Sprintf("%s-%03d", prefix, i), Name: fmt.Sprintf("%s-%03d", prefix, i),
				ObservedAt: now, Status: protocol.StatusOK,
			}
		}
		return out
	}
	if err := store.ApplyConnectorHomeLab("first", protocol.HomeLabReport{Nodes: nodes("first", 60)}); err != nil {
		t.Fatal(err)
	}
	before := store.Snapshot()
	if err := store.ApplyConnectorHomeLab("second", protocol.HomeLabReport{Nodes: nodes("second", 50)}); err == nil {
		t.Fatal("merged node limit was accepted")
	}
	after := store.Snapshot()
	if len(after.HomeLabNodes) != 60 || after.SnapshotVersion != before.SnapshotVersion {
		t.Fatal("rejected merged limit mutated current state")
	}
	collision := protocol.HomeLabReport{Nodes: []protocol.HomeLabNode{{
		ID: "first-000", Name: "collision", ObservedAt: now, Status: protocol.StatusOK,
	}}}
	if err := store.ApplyConnectorHomeLab("second", collision); err == nil {
		t.Fatal("cross-connector node ID collision was accepted")
	}
	after = store.Snapshot()
	if after.HomeLabNodes[0].Name != "first-000" || after.SnapshotVersion != before.SnapshotVersion {
		t.Fatal("rejected ID collision overwrote another connector")
	}
}

func TestHomeLabStateDoesNotShareCallerPointers(t *testing.T) {
	store, err := state.New(state.Options{NodeID: "source", Epoch: "epoch"})
	if err != nil {
		t.Fatal(err)
	}
	cpu := 28
	report := protocol.HomeLabReport{Nodes: []protocol.HomeLabNode{{
		ID: "node", Name: "node", CPUPercent: &cpu, ObservedAt: time.Now().UTC(), Status: protocol.StatusOK,
	}}}
	if err := store.ApplyConnectorHomeLab("prom", report); err != nil {
		t.Fatal(err)
	}
	*report.Nodes[0].CPUPercent = 99
	if got := *store.Snapshot().HomeLabNodes[0].CPUPercent; got != 28 {
		t.Fatalf("stored CPU was mutated through caller pointer: %d", got)
	}
}
