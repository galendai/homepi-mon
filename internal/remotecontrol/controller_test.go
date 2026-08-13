package remotecontrol_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/galendai/homepi-mon/internal/protocol"
	"github.com/galendai/homepi-mon/internal/remotecontrol"
	"github.com/galendai/homepi-mon/internal/ui"
)

func TestControllerExecutesPersistsAndDeduplicatesPageCommand(t *testing.T) {
	now := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	router := ui.NewRouter(ui.DefaultRotationConfig(), now)
	controller := newController(t, dir, router, &now)
	command := displayCommand(t, protocol.CommandShowPage,
		protocol.PageCommandParams{PageID: "API", DurationSeconds: 30}, 10, now)

	results := controller.Handle(context.Background(), command)
	if len(results) != 2 || results[0].Status != protocol.CommandAccepted || results[1].Status != protocol.CommandExecuted {
		t.Fatalf("results=%+v", results)
	}
	if page := router.Update(now.Add(time.Second), nil); page != ui.PageAPI {
		t.Fatalf("page=%s", page)
	}
	for i := 0; i < 10; i++ {
		duplicate := controller.Handle(context.Background(), command)
		if len(duplicate) != 1 || duplicate[0].Status != protocol.CommandExecuted {
			t.Fatalf("duplicate %d=%+v", i, duplicate)
		}
	}
	now = now.Add(31 * time.Second)
	if page := router.Update(now, nil); page != ui.PageCoding {
		t.Fatalf("page after expiry=%s", page)
	}

	restarted := newController(t, dir, ui.NewRouter(ui.DefaultRotationConfig(), now), &now)
	duplicate := restarted.Handle(context.Background(), command)
	if len(duplicate) != 1 || duplicate[0].Status != protocol.CommandExecuted {
		t.Fatalf("restart duplicate=%+v", duplicate)
	}
	info, err := os.Stat(filepath.Join(dir, "command-results.json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("state mode=%v err=%v", info.Mode(), err)
	}
}

func TestControllerRefreshWaitsForNewSnapshot(t *testing.T) {
	now := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)
	controller := newController(t, t.TempDir(), ui.NewRouter(ui.DefaultRotationConfig(), now), &now)
	controller.SnapshotApplied(&protocol.MetricSnapshot{SourceEpoch: "epoch", SnapshotVersion: 4})
	command := displayCommand(t, protocol.CommandRefreshData,
		protocol.RefreshCommandParams{ConnectorIDs: []string{"mock"}}, 11, now)
	results := controller.Handle(context.Background(), command)
	if len(results) != 1 || results[0].Status != protocol.CommandAccepted {
		t.Fatalf("results=%+v", results)
	}
	if got := controller.SnapshotApplied(&protocol.MetricSnapshot{SourceEpoch: "epoch", SnapshotVersion: 4}); len(got) != 0 {
		t.Fatalf("unchanged snapshot completed refresh: %+v", got)
	}
	now = now.Add(time.Second)
	completed := controller.SnapshotApplied(&protocol.MetricSnapshot{SourceEpoch: "epoch", SnapshotVersion: 5})
	if len(completed) != 1 || completed[0].Status != protocol.CommandExecuted {
		t.Fatalf("completed=%+v", completed)
	}
}

func TestControllerMessageNoticeAndBrightnessFailure(t *testing.T) {
	now := time.Date(2026, 8, 13, 14, 40, 0, 0, time.UTC)
	router := ui.NewRouter(ui.DefaultRotationConfig(), now)
	controller := newController(t, t.TempDir(), router, &now)
	message := displayCommand(t, protocol.CommandShowMessage,
		protocol.MessageCommandParams{Text: "MAINTENANCE STARTS SOON", Severity: "warning", DurationSeconds: 30}, 1, now)
	message.Priority = protocol.PriorityHigh
	if got := controller.Handle(context.Background(), message); len(got) != 2 || got[1].Status != protocol.CommandExecuted {
		t.Fatalf("message=%+v", got)
	}
	notice := controller.Notice(now.Add(5 * time.Second))
	frame := ui.RenderStyledString(ui.ViewModel{
		HasSnapshot: true, RotationEnabled: true, Page: "CODING", Now: now,
		RemoteNotice: notice,
	}, ui.StyleASCII)
	if !strings.Contains(frame, "REMOTE NOTICE") || !strings.Contains(frame, "RETURN IN 25S") || strings.Contains(frame, "\x1b") {
		t.Fatalf("frame:\n%s", frame)
	}
	if controller.Notice(now.Add(31*time.Second)) != nil {
		t.Fatal("expired message is still visible")
	}

	brightness := displayCommand(t, protocol.CommandSetBrightness,
		protocol.BrightnessCommandParams{Level: 50}, 2, now)
	result := controller.Handle(context.Background(), brightness)
	if len(result) != 2 || result[1].Status != protocol.CommandFailed || result[1].Code != "unsupported_capability" {
		t.Fatalf("brightness=%+v", result)
	}
}

func TestControllerAuditLogsHashButNeverMessageText(t *testing.T) {
	now := time.Date(2026, 8, 13, 14, 40, 0, 0, time.UTC)
	var output bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&output, nil))
	controller, err := remotecontrol.New(remotecontrol.Options{
		DataDir: t.TempDir(), DeviceID: "display-1", SourceNode: "dev-mac",
		Router: ui.NewRouter(ui.DefaultRotationConfig(), now), Logger: logger,
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	const messageText = "PRIVATE MAINTENANCE NOTICE"
	command := displayCommand(t, protocol.CommandShowMessage,
		protocol.MessageCommandParams{Text: messageText, Severity: "warning", DurationSeconds: 30}, 4, now)
	command.Priority = protocol.PriorityHigh
	controller.Handle(context.Background(), command)
	logOutput := output.String()
	if strings.Contains(logOutput, messageText) {
		t.Fatalf("audit log retained message text: %s", logOutput)
	}
	if !strings.Contains(logOutput, "message_length=26") || !strings.Contains(logOutput, "message_sha256=") {
		t.Fatalf("audit log omitted redacted metadata: %s", logOutput)
	}
}

func TestControllerRejectsWrongTargetAndOutOfOrder(t *testing.T) {
	now := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)
	controller := newController(t, t.TempDir(), ui.NewRouter(ui.DefaultRotationConfig(), now), &now)
	first := displayCommand(t, protocol.CommandNextPage, protocol.StepPageCommandParams{DurationSeconds: 30}, 10, now)
	controller.Handle(context.Background(), first)
	old := displayCommand(t, protocol.CommandNextPage, protocol.StepPageCommandParams{DurationSeconds: 30}, 9, now)
	if got := controller.Handle(context.Background(), old); len(got) != 1 || got[0].Code != "out_of_order" {
		t.Fatalf("out of order=%+v", got)
	}
	wrong := displayCommand(t, protocol.CommandNextPage, protocol.StepPageCommandParams{DurationSeconds: 30}, 11, now)
	wrong.DeviceID = "other"
	if got := controller.Handle(context.Background(), wrong); len(got) != 1 || got[0].Code != "wrong_target" {
		t.Fatalf("wrong target=%+v", got)
	}
}

func TestResultLedgerRetainsOnlyNewest256Records(t *testing.T) {
	now := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	controller := newController(t, dir, ui.NewRouter(ui.DefaultRotationConfig(), now), &now)
	for sequence := uint64(1); sequence <= 257; sequence++ {
		command := displayCommand(t, protocol.CommandNextPage,
			protocol.StepPageCommandParams{DurationSeconds: 5}, sequence, now)
		results := controller.Handle(context.Background(), command)
		if len(results) != 2 || results[1].Status != protocol.CommandExecuted {
			t.Fatalf("sequence %d results=%+v", sequence, results)
		}
	}
	raw, err := os.ReadFile(filepath.Join(dir, "command-results.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state struct {
		HighSequence uint64            `json:"high_sequence"`
		Records      []json.RawMessage `json:"records"`
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	if state.HighSequence != 257 || len(state.Records) != 256 {
		t.Fatalf("high_sequence=%d records=%d", state.HighSequence, len(state.Records))
	}
}

func TestCorruptResultStateIsQuarantined(t *testing.T) {
	now := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	path := filepath.Join(dir, "command-results.json")
	if err := os.WriteFile(path, []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	newController(t, dir, ui.NewRouter(ui.DefaultRotationConfig(), now), &now)
	if _, err := os.Stat(path + ".corrupt-1786579200"); err != nil {
		t.Fatalf("quarantine missing: %v", err)
	}
}

func TestAcceptedNonRefreshFromPreviousProcessBecomesInterrupted(t *testing.T) {
	now := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	command := displayCommand(t, protocol.CommandNextPage,
		protocol.StepPageCommandParams{DurationSeconds: 30}, 7, now.Add(-time.Second))
	state := map[string]any{
		"high_sequence": 7,
		"records": []any{map[string]any{
			"command_id": command.CommandID, "sequence": 7, "kind": command.Kind,
			"result": protocol.CommandResult{
				CommandID: command.CommandID, Sequence: 7,
				Status: protocol.CommandAccepted, ReceivedAt: now.Add(-time.Second),
			},
		}},
	}
	raw, _ := json.Marshal(state)
	if err := os.WriteFile(filepath.Join(dir, "command-results.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	controller := newController(t, dir, ui.NewRouter(ui.DefaultRotationConfig(), now), &now)
	result := controller.Handle(context.Background(), command)
	if len(result) != 1 || result[0].Status != protocol.CommandFailed || result[0].Code != "interrupted" {
		t.Fatalf("result=%+v", result)
	}
}

func newController(t *testing.T, dir string, router *ui.Router, now *time.Time) *remotecontrol.Controller {
	t.Helper()
	controller, err := remotecontrol.New(remotecontrol.Options{
		DataDir: dir, DeviceID: "display-1", SourceNode: "dev-mac", Router: router,
		Now: func() time.Time { return *now },
	})
	if err != nil {
		t.Fatal(err)
	}
	return controller
}

func displayCommand(t *testing.T, kind protocol.CommandKind, params any, sequence uint64,
	now time.Time) protocol.DisplayCommand {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	return protocol.DisplayCommand{
		SchemaMajor: protocol.CommandSchemaMajor,
		CommandID:   commandID(sequence),
		DeviceID:    "display-1",
		Kind:        kind,
		Params:      raw,
		IssuedAt:    now,
		ExpiresAt:   now.Add(30 * time.Second),
		Priority:    protocol.PriorityNormal,
		Sequence:    sequence,
	}
}

func commandID(sequence uint64) string {
	return "123e4567-e89b-42d3-a456-" + leftPad(sequence)
}

func leftPad(value uint64) string {
	raw := []byte("000000000000")
	for i := len(raw) - 1; value > 0 && i >= 0; i-- {
		raw[i] = byte('0' + value%10)
		value /= 10
	}
	return string(raw)
}
