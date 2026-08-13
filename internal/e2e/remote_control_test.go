package e2e

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/galendai/homepi-mon/internal/commandbus"
	"github.com/galendai/homepi-mon/internal/nodeapi"
	"github.com/galendai/homepi-mon/internal/protocol"
	"github.com/galendai/homepi-mon/internal/remotecontrol"
	"github.com/galendai/homepi-mon/internal/snapstore"
	"github.com/galendai/homepi-mon/internal/state"
	"github.com/galendai/homepi-mon/internal/syncclient"
	"github.com/galendai/homepi-mon/internal/ui"
)

func TestRemoteCommandFlowsAcrossAuthenticatedStreamToRouterAndBack(t *testing.T) {
	now := time.Now().UTC()
	current, err := state.New(state.Options{NodeID: nodeID, NodeLabel: nodeLabel, Epoch: "phase4-e2e"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := current.ApplyConnectorMetrics("mock", 1, []protocol.ProviderMetric{{
		ID: "mock.available", Provider: "mock", AccountLabel: "demo", DisplayName: "Mock",
		MetricKind: protocol.KindAvailability, Unit: "boolean", Window: protocol.WindowInstant,
		ObservedAt: now, Precision: protocol.PrecisionExact, SourceKind: protocol.SourceMock,
		Status: protocol.StatusOK,
	}}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	bus, err := commandbus.Open(filepath.Join(root, "node", "commands.json"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	server, err := nodeapi.New(nodeapi.Options{
		Store: current, Devices: []nodeapi.Device{{ID: deviceID, Token: deviceToken}},
		Commands: bus, ControlToken: "phase4-control-token-with-thirty-two-characters",
		ConnectorIDs: []string{"mock"}, Logger: quiet(),
		PollInterval: 10 * time.Millisecond, Heartbeat: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	displayDir := filepath.Join(root, "display")
	snapshotStore, err := snapstore.New(displayDir, protocol.SourceBinding{NodeID: nodeID})
	if err != nil {
		t.Fatal(err)
	}
	router := ui.NewRouter(ui.DefaultRotationConfig(), now)
	controller, err := remotecontrol.New(remotecontrol.Options{
		DataDir: displayDir, DeviceID: deviceID, SourceNode: nodeID,
		Router: router, Logger: quiet(),
	})
	if err != nil {
		t.Fatal(err)
	}
	client, err := syncclient.New(syncclient.Options{
		BaseURL: httpServer.URL, DeviceID: deviceID, Token: deviceToken,
		Binding: protocol.SourceBinding{NodeID: nodeID}, Store: snapshotStore,
		ClientVersion: "phase4-test", Logger: quiet(), CommandHandler: controller,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go client.Run(ctx)
	waitFor(t, "Phase 4 stream", 3*time.Second, func() bool {
		return client.Connected() && client.Snapshot() != nil
	})

	params, _ := json.Marshal(protocol.PageCommandParams{PageID: "API", DurationSeconds: 30})
	entry, err := bus.Publish(deviceID, protocol.CommandRequest{
		Kind: protocol.CommandShowPage, Params: params,
		Priority: protocol.PriorityNormal, TTLSeconds: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "executed command result", 3*time.Second, func() bool {
		result, resultErr := bus.Get(entry.Command.CommandID)
		return resultErr == nil && result.Result.Status == protocol.CommandExecuted
	})
	if page := router.Update(time.Now(), nil); page != ui.PageAPI {
		t.Fatalf("page=%s, want API", page)
	}

	for i := 0; i < 10; i++ {
		results := controller.Handle(context.Background(), entry.Command)
		if len(results) != 1 || results[0].Status != protocol.CommandExecuted {
			t.Fatalf("replay %d=%+v", i, results)
		}
	}
}

func TestRefreshCommandCompletesOnlyAfterNewSnapshotCrossesStream(t *testing.T) {
	now := time.Now().UTC()
	current, err := state.New(state.Options{NodeID: nodeID, NodeLabel: nodeLabel, Epoch: "phase4-refresh"})
	if err != nil {
		t.Fatal(err)
	}
	metric := protocol.ProviderMetric{
		ID: "mock.available", Provider: "mock", AccountLabel: "demo", DisplayName: "Mock",
		MetricKind: protocol.KindAvailability, Unit: "boolean", Window: protocol.WindowInstant,
		ObservedAt: now, Precision: protocol.PrecisionExact, SourceKind: protocol.SourceMock,
		Status: protocol.StatusOK,
	}
	if _, err := current.ApplyConnectorMetrics("mock", 1, []protocol.ProviderMetric{metric}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	bus, err := commandbus.Open(filepath.Join(root, "node", "commands.json"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	refreshed := make(chan struct{}, 1)
	server, err := nodeapi.New(nodeapi.Options{
		Store: current, Devices: []nodeapi.Device{{ID: deviceID, Token: deviceToken}},
		Commands: bus, ControlToken: "phase4-control-token-with-thirty-two-characters",
		ConnectorIDs: []string{"mock"}, Logger: quiet(),
		PollInterval: 10 * time.Millisecond, Heartbeat: 100 * time.Millisecond,
		Refresh: func(_ context.Context, ids []string) error {
			if len(ids) != 1 || ids[0] != "mock" {
				t.Errorf("refresh ids=%v", ids)
			}
			metric.ObservedAt = time.Now().UTC()
			_, applyErr := current.ApplyConnectorMetrics("mock", 2, []protocol.ProviderMetric{metric})
			if applyErr == nil {
				refreshed <- struct{}{}
			}
			return applyErr
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()
	displayDir := filepath.Join(root, "display")
	snapshotStore, err := snapstore.New(displayDir, protocol.SourceBinding{NodeID: nodeID})
	if err != nil {
		t.Fatal(err)
	}
	controller, err := remotecontrol.New(remotecontrol.Options{
		DataDir: displayDir, DeviceID: deviceID, SourceNode: nodeID,
		Router: ui.NewRouter(ui.DefaultRotationConfig(), now), Logger: quiet(),
	})
	if err != nil {
		t.Fatal(err)
	}
	client, err := syncclient.New(syncclient.Options{
		BaseURL: httpServer.URL, DeviceID: deviceID, Token: deviceToken,
		Binding: protocol.SourceBinding{NodeID: nodeID}, Store: snapshotStore,
		ClientVersion: "phase4-test", Logger: quiet(), CommandHandler: controller,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go client.Run(ctx)
	waitFor(t, "initial snapshot", 3*time.Second, func() bool {
		return client.Snapshot() != nil && client.Snapshot().SnapshotVersion == 1
	})
	entry, err := bus.Publish(deviceID, protocol.CommandRequest{
		Kind: protocol.CommandRefreshData, Params: json.RawMessage(`{"connector_ids":["mock"]}`),
		Priority: protocol.PriorityNormal, TTLSeconds: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "accepted refresh", 3*time.Second, func() bool {
		result, getErr := bus.Get(entry.Command.CommandID)
		return getErr == nil && (result.Result.Status == protocol.CommandAccepted || result.Result.Status == protocol.CommandExecuted)
	})
	select {
	case <-refreshed:
	case <-time.After(3 * time.Second):
		t.Fatal("scheduler refresh callback was not reached")
	}
	waitFor(t, "refreshed snapshot and final result", 3*time.Second, func() bool {
		result, getErr := bus.Get(entry.Command.CommandID)
		snapshot := client.Snapshot()
		return getErr == nil && result.Result.Status == protocol.CommandExecuted &&
			snapshot != nil && snapshot.SnapshotVersion >= 2
	})
}
