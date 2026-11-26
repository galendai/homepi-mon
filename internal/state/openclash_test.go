package state_test

import (
	"testing"
	"time"

	"github.com/galendai/homepi-mon/internal/protocol"
	"github.com/galendai/homepi-mon/internal/state"
)

func stateOpenClashReport(observedAt time.Time, delay int) protocol.OpenClashReport {
	return protocol.OpenClashReport{
		ObservedAt: observedAt, StaleAfter: protocol.Duration(15 * time.Second),
		DelayStaleAfter: protocol.Duration(100 * time.Second), SelectedGroup: "🚀 PROXY",
		SelectedNode: "node-a", TotalConnections: 1, Status: protocol.StatusOK,
		Nodes: []protocol.OpenClashNode{{
			Name: "node-a", Alive: new(true), DelayMS: new(delay), CheckedAt: &observedAt, ActiveConnections: 1,
		}},
	}
}

func TestOpenClashStateOwnershipLastKnownGoodAndClone(t *testing.T) {
	now := time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)
	store, err := state.New(state.Options{NodeID: "node", Epoch: "epoch", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if store.HasData() {
		t.Fatal("empty store has data")
	}
	report := stateOpenClashReport(now, 120)
	if err := store.ApplyConnectorOpenClash("clash", 1, report); err != nil {
		t.Fatal(err)
	}
	*report.Nodes[0].DelayMS = 999
	if !store.HasData() || *store.Snapshot().OpenClash.Nodes[0].DelayMS != 120 {
		t.Fatal("state aliases the caller's report")
	}
	changed, err := store.ApplyConnectorError("other", protocol.ErrNetwork, "unrelated")
	if err != nil || changed != 0 {
		t.Fatalf("unrelated error changed=%d err=%v", changed, err)
	}
	changed, err = store.ApplyConnectorError("clash", protocol.ErrAuth, "update the keyring credential")
	if err != nil || changed != 1 {
		t.Fatalf("owned error changed=%d err=%v", changed, err)
	}
	stale := store.Snapshot().OpenClash
	if *stale.Nodes[0].DelayMS != 120 || stale.ErrorClass != protocol.ErrAuth ||
		stale.Message != "update the keyring credential" {
		t.Fatalf("last-known-good report = %+v", stale)
	}
	*stale.Nodes[0].DelayMS = 777
	if *store.Snapshot().OpenClash.Nodes[0].DelayMS != 120 {
		t.Fatal("snapshot aliases current OpenClash state")
	}
	if err := store.ApplyConnectorOpenClash("clash", 2, stateOpenClashReport(now.Add(time.Second), 80)); err != nil {
		t.Fatal(err)
	}
	fresh := store.Snapshot().OpenClash
	if *fresh.Nodes[0].DelayMS != 80 || fresh.ErrorClass != protocol.ErrNone || fresh.Message != "" {
		t.Fatalf("recovered report = %+v", fresh)
	}
}

func TestOpenClashStateRejectsLateResultsAndMarksStale(t *testing.T) {
	now := time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)
	store, _ := state.New(state.Options{NodeID: "node", Epoch: "epoch", Now: func() time.Time { return now }})
	if err := store.ApplyConnectorOpenClash("clash", 2, stateOpenClashReport(now, 100)); err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyConnectorOpenClash("clash", 1, stateOpenClashReport(now, 900)); err != nil {
		t.Fatal(err)
	}
	if *store.Snapshot().OpenClash.Nodes[0].DelayMS != 100 {
		t.Fatal("late result replaced newer OpenClash state")
	}
	now = now.Add(16 * time.Second)
	if got := store.Snapshot().OpenClash.Status; got != protocol.StatusStale {
		t.Fatalf("stale status = %q, want stale", got)
	}
	if err := store.Snapshot().Validate(); err != nil {
		t.Fatal(err)
	}
}
