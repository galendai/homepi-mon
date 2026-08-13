package commandbus_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/galendai/homepi-mon/internal/commandbus"
	"github.com/galendai/homepi-mon/internal/protocol"
)

func TestBusPersistsSequencePendingAndResult(t *testing.T) {
	now := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "commands.json")
	bus, err := commandbus.Open(path, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	params, _ := json.Marshal(protocol.PageCommandParams{PageID: "API", DurationSeconds: 30})
	entry, err := bus.Publish("display-1", protocol.CommandRequest{
		Kind: protocol.CommandShowPage, Params: params, Priority: protocol.PriorityNormal, TTLSeconds: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	if entry.Command.Sequence != 1 {
		t.Fatalf("sequence=%d", entry.Command.Sequence)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%o", info.Mode().Perm())
	}

	reopened, err := commandbus.Open(path, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	pending, err := reopened.Pending("display-1", nil)
	if err != nil || len(pending) != 1 || pending[0].CommandID != entry.Command.CommandID {
		t.Fatalf("pending=%+v err=%v", pending, err)
	}
	accepted := protocol.CommandResult{
		CommandID: entry.Command.CommandID, Sequence: 1, Status: protocol.CommandAccepted, ReceivedAt: now,
	}
	if _, first, err := reopened.Record("display-1", accepted); err != nil || !first {
		t.Fatalf("accepted first=%v err=%v", first, err)
	}
	completed := now.Add(time.Second)
	accepted.Status, accepted.CompletedAt = protocol.CommandExecuted, &completed
	if _, _, err := reopened.Record("display-1", accepted); err != nil {
		t.Fatal(err)
	}
	got, err := reopened.Get(entry.Command.CommandID)
	if err != nil || got.Result.Status != protocol.CommandExecuted {
		t.Fatalf("got=%+v err=%v", got, err)
	}

	next, err := reopened.Publish("display-1", protocol.CommandRequest{
		Kind: protocol.CommandNextPage, Params: json.RawMessage(`{}`), Priority: protocol.PriorityNormal, TTLSeconds: 30,
	})
	if err != nil || next.Command.Sequence != 2 {
		t.Fatalf("next=%+v err=%v", next, err)
	}
}

func TestBusExpiresAndRedactsMessage(t *testing.T) {
	now := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "commands.json")
	bus, _ := commandbus.Open(path, func() time.Time { return now })
	params, _ := json.Marshal(protocol.MessageCommandParams{Text: "maintenance soon", Severity: "warning", DurationSeconds: 30})
	entry, err := bus.Publish("display-1", protocol.CommandRequest{
		Kind: protocol.CommandShowMessage, Params: params, Priority: protocol.PriorityHigh, TTLSeconds: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(6 * time.Second)
	got, err := bus.Get(entry.Command.CommandID)
	if err != nil || got.Result.Status != protocol.CommandExpired {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	raw, _ := os.ReadFile(path)
	if string(raw) == "" || json.Valid(raw) == false {
		t.Fatal("state is not valid JSON")
	}
	if contains(string(raw), "maintenance soon") {
		t.Fatal("completed state retained the full message")
	}
}

func TestBusPreservesSequenceOrderAndRejectsWrongDeviceResult(t *testing.T) {
	now := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)
	bus, err := commandbus.Open(filepath.Join(t.TempDir(), "commands.json"), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	first, err := bus.Publish("display-1", protocol.CommandRequest{
		Kind: protocol.CommandNextPage, Params: json.RawMessage(`{}`),
		Priority: protocol.PriorityNormal, TTLSeconds: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	message, _ := json.Marshal(protocol.MessageCommandParams{Text: "notice", Severity: "warning"})
	second, err := bus.Publish("display-1", protocol.CommandRequest{
		Kind: protocol.CommandShowMessage, Params: message,
		Priority: protocol.PriorityHigh, TTLSeconds: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	pending, err := bus.Pending("display-1", nil)
	if err != nil || len(pending) != 2 || pending[0].Sequence != first.Command.Sequence || pending[1].Sequence != second.Command.Sequence {
		t.Fatalf("pending=%+v err=%v", pending, err)
	}
	if _, _, err := bus.Record("display-2", protocol.CommandResult{
		CommandID: first.Command.CommandID, Sequence: first.Command.Sequence,
		Status: protocol.CommandAccepted, ReceivedAt: now,
	}); err == nil {
		t.Fatal("wrong device result was accepted")
	}
}

func TestAcceptedCommandUsesExecutionTimeoutNotDeliveryExpiry(t *testing.T) {
	now := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)
	bus, _ := commandbus.Open(filepath.Join(t.TempDir(), "commands.json"), func() time.Time { return now })
	entry, _ := bus.Publish("display-1", protocol.CommandRequest{
		Kind: protocol.CommandRefreshData, Params: json.RawMessage(`{}`),
		Priority: protocol.PriorityNormal, TTLSeconds: 5,
	})
	_, _, err := bus.Record("display-1", protocol.CommandResult{
		CommandID: entry.Command.CommandID, Sequence: entry.Command.Sequence,
		Status: protocol.CommandAccepted, ReceivedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(6 * time.Second)
	got, _ := bus.Get(entry.Command.CommandID)
	if got.Result.Status != protocol.CommandAccepted {
		t.Fatalf("accepted command expired during execution: %+v", got.Result)
	}
	now = now.Add(protocol.MaxCommandTTL)
	got, _ = bus.Get(entry.Command.CommandID)
	if got.Result.Status != protocol.CommandFailed || got.Result.Code != "execution_timeout" {
		t.Fatalf("execution timeout=%+v", got.Result)
	}
}

func TestBusBoundsPendingQueueAndEvictsOldestPublishedNormalCommand(t *testing.T) {
	now := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)
	bus, err := commandbus.Open(filepath.Join(t.TempDir(), "commands.json"), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	var oldest commandbus.Entry
	for index := 0; index < commandbus.MaxPending; index++ {
		entry, publishErr := bus.Publish("display-1", protocol.CommandRequest{
			Kind: protocol.CommandNextPage, Params: json.RawMessage(`{}`),
			Priority: protocol.PriorityNormal, TTLSeconds: 30,
		})
		if publishErr != nil {
			t.Fatalf("publish %d: %v", index, publishErr)
		}
		if index == 0 {
			oldest = entry
		}
	}
	message, _ := json.Marshal(protocol.MessageCommandParams{Text: "urgent notice", Severity: "warning"})
	latest, err := bus.Publish("display-1", protocol.CommandRequest{
		Kind: protocol.CommandShowMessage, Params: message,
		Priority: protocol.PriorityHigh, TTLSeconds: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	evicted, err := bus.Get(oldest.Command.CommandID)
	if err != nil || evicted.Result.Status != protocol.CommandFailed || evicted.Result.Code != "overflow" {
		t.Fatalf("evicted=%+v err=%v", evicted, err)
	}
	pending, err := bus.Pending("display-1", nil)
	if err != nil || len(pending) != commandbus.MaxPending {
		t.Fatalf("pending=%d err=%v", len(pending), err)
	}
	if pending[len(pending)-1].CommandID != latest.Command.CommandID {
		t.Fatalf("last pending command=%s, want %s", pending[len(pending)-1].CommandID, latest.Command.CommandID)
	}
}

func TestCorruptBusStateIsQuarantined(t *testing.T) {
	now := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	path := filepath.Join(dir, "commands.json")
	if err := os.WriteFile(path, []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := commandbus.Open(path, func() time.Time { return now }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".corrupt-1786579200"); err != nil {
		t.Fatalf("quarantine missing: %v", err)
	}
}

func contains(value, fragment string) bool {
	for i := 0; i+len(fragment) <= len(value); i++ {
		if value[i:i+len(fragment)] == fragment {
			return true
		}
	}
	return false
}
