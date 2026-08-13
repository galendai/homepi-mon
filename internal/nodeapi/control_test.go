package nodeapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/galendai/homepi-mon/internal/commandbus"
	"github.com/galendai/homepi-mon/internal/nodeapi"
	"github.com/galendai/homepi-mon/internal/protocol"
	"github.com/galendai/homepi-mon/internal/state"
)

const (
	controlToken      = "control-token-with-at-least-thirty-two-characters"
	phase4DeviceToken = "device-token-with-at-least-sixteen"
)

func TestControlAPIRequiresLocalIndependentCredential(t *testing.T) {
	server, _, _ := newCommandServer(t, nil)
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()
	body := commandBody(t, protocol.CommandShowPage, protocol.PageCommandParams{PageID: "API"})

	request := func(token, origin string) *http.Response {
		req, _ := http.NewRequest(http.MethodPost,
			httpServer.URL+"/v1/control/devices/display-1/commands", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	resp := request(phase4DeviceToken, "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("device token status=%d", resp.StatusCode)
	}
	resp.Body.Close()
	resp = request(controlToken, "https://example.test")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("browser origin status=%d", resp.StatusCode)
	}
	resp.Body.Close()
	resp = request(controlToken, "")
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("control status=%d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestCommandPublishesOverStreamAndRecordsResults(t *testing.T) {
	server, bus, _ := newCommandServer(t, nil)
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	body := commandBody(t, protocol.CommandShowPage,
		protocol.PageCommandParams{PageID: "API", DurationSeconds: 30})
	req, _ := http.NewRequest(http.MethodPost,
		httpServer.URL+"/v1/control/devices/display-1/commands", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+controlToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var published struct {
		CommandID string `json:"command_id"`
		Sequence  uint64 `json:"sequence"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&published); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	header := http.Header{"Authorization": []string{"Bearer " + phase4DeviceToken}}
	conn, _, err := websocket.Dial(ctx, "ws"+httpServer.URL[len("http"):]+"/v1/devices/display-1/stream",
		&websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	hello, _ := protocol.Encode(protocol.MsgHello, time.Now(), protocol.Hello{
		DeviceID: "display-1", SchemaMajor: protocol.SchemaMajor,
	})
	if err := conn.Write(ctx, websocket.MessageText, hello); err != nil {
		t.Fatal(err)
	}
	_, raw, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := protocol.DecodeEnvelope(raw)
	if err != nil || envelope.Type != protocol.MsgDisplayCommand {
		t.Fatalf("envelope=%+v err=%v", envelope, err)
	}
	command, err := protocol.DecodeDisplayCommand(envelope.Payload, time.Now())
	if err != nil || command.CommandID != published.CommandID {
		t.Fatalf("command=%+v err=%v", command, err)
	}
	now := time.Now().UTC()
	accepted := protocol.CommandResult{
		CommandID: command.CommandID, Sequence: command.Sequence,
		Status: protocol.CommandAccepted, ReceivedAt: now,
	}
	sendResult(t, ctx, conn, accepted)
	completed := now.Add(time.Millisecond)
	accepted.Status, accepted.CompletedAt = protocol.CommandExecuted, &completed
	sendResult(t, ctx, conn, accepted)

	deadline := time.Now().Add(time.Second)
	for {
		entry, err := bus.Get(command.CommandID)
		if err == nil && entry.Result.Status == protocol.CommandExecuted {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("result was not recorded: %+v err=%v", entry, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestRefreshAcceptedTriggersAllowlistedCallback(t *testing.T) {
	refreshed := make(chan []string, 1)
	server, bus, _ := newCommandServer(t, func(_ context.Context, ids []string) error {
		refreshed <- ids
		return nil
	})
	entry, err := bus.Publish("display-1", protocol.CommandRequest{
		Kind: protocol.CommandRefreshData, Params: json.RawMessage(`{"connector_ids":["mock"]}`),
		Priority: protocol.PriorityNormal, TTLSeconds: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	header := http.Header{"Authorization": []string{"Bearer " + phase4DeviceToken}}
	conn, _, err := websocket.Dial(ctx, "ws"+httpServer.URL[len("http"):]+"/v1/devices/display-1/stream",
		&websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	hello, _ := protocol.Encode(protocol.MsgHello, time.Now(), protocol.Hello{
		DeviceID: "display-1", SchemaMajor: protocol.SchemaMajor,
	})
	if err := conn.Write(ctx, websocket.MessageText, hello); err != nil {
		t.Fatal(err)
	}
	_, raw, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := protocol.DecodeEnvelope(raw)
	if err != nil || envelope.Type != protocol.MsgDisplayCommand {
		t.Fatalf("envelope=%+v err=%v", envelope, err)
	}
	command, err := protocol.DecodeDisplayCommand(envelope.Payload, time.Now())
	if err != nil || command.CommandID != entry.Command.CommandID {
		t.Fatalf("command=%+v err=%v", command, err)
	}
	now := time.Now().UTC()
	sendResult(t, ctx, conn, protocol.CommandResult{
		CommandID: command.CommandID, Sequence: command.Sequence,
		Status: protocol.CommandAccepted, ReceivedAt: now,
	})
	select {
	case ids := <-refreshed:
		if len(ids) != 1 || ids[0] != "mock" {
			t.Fatalf("refreshed=%v", ids)
		}
	case <-ctx.Done():
		t.Fatal("refresh callback was not invoked")
	}

	body := commandBody(t, protocol.CommandRefreshData,
		protocol.RefreshCommandParams{ConnectorIDs: []string{"missing"}})
	req, _ := http.NewRequest(http.MethodPost,
		httpServer.URL+"/v1/control/devices/display-1/commands", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+controlToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown connector status=%d", resp.StatusCode)
	}
}

func newCommandServer(t *testing.T, refresh func(context.Context, []string) error) (*nodeapi.Server, *commandbus.Bus, *state.Current) {
	t.Helper()
	store, err := state.New(state.Options{NodeID: "node", Epoch: "epoch"})
	if err != nil {
		t.Fatal(err)
	}
	bus, err := commandbus.Open(filepath.Join(t.TempDir(), "commands.json"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	server, err := nodeapi.New(nodeapi.Options{
		Store: store, Devices: []nodeapi.Device{{ID: "display-1", Token: phase4DeviceToken}},
		Commands: bus, ControlToken: controlToken, Refresh: refresh,
		ConnectorIDs: []string{"mock"}, PollInterval: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	return server, bus, store
}

func commandBody(t *testing.T, kind protocol.CommandKind, params any) []byte {
	t.Helper()
	raw, _ := json.Marshal(params)
	body, err := json.Marshal(protocol.CommandRequest{
		Kind: kind, Params: raw, Priority: protocol.PriorityNormal, TTLSeconds: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func sendResult(t *testing.T, ctx context.Context, conn *websocket.Conn, result protocol.CommandResult) {
	t.Helper()
	raw, err := protocol.Encode(protocol.MsgCommandResult, time.Now(), result)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Write(ctx, websocket.MessageText, raw); err != nil {
		t.Fatal(err)
	}
}
