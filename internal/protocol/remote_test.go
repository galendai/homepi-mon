package protocol

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDisplayCommandStrictValidation(t *testing.T) {
	now := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)
	params, _ := json.Marshal(PageCommandParams{PageID: "API", DurationSeconds: 30})
	valid := DisplayCommand{
		SchemaMajor: CommandSchemaMajor, CommandID: "123e4567-e89b-42d3-a456-426614174000",
		DeviceID: "display-1", Kind: CommandShowPage, Params: params,
		IssuedAt: now, ExpiresAt: now.Add(30 * time.Second), Priority: PriorityNormal, Sequence: 1,
	}
	raw, _ := json.Marshal(valid)
	if _, err := DecodeDisplayCommand(raw, now); err != nil {
		t.Fatalf("valid command: %v", err)
	}

	tests := []struct {
		name string
		edit func(*DisplayCommand)
		code string
	}{
		{"wrong schema", func(c *DisplayCommand) { c.SchemaMajor = 2 }, "schema_unsupported"},
		{"bad id", func(c *DisplayCommand) { c.CommandID = "no" }, "invalid_command_id"},
		{"expired", func(c *DisplayCommand) { c.IssuedAt = now.Add(-time.Minute); c.ExpiresAt = now.Add(-time.Second) }, "expired"},
		{"future", func(c *DisplayCommand) { c.IssuedAt = now.Add(31 * time.Second); c.ExpiresAt = now.Add(time.Minute) }, "issued_in_future"},
		{"unknown", func(c *DisplayCommand) { c.Kind = "exec_shell" }, "unknown_command"},
		{"zero sequence", func(c *DisplayCommand) { c.Sequence = 0 }, "invalid_sequence"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			candidate := valid
			tc.edit(&candidate)
			raw, _ := json.Marshal(candidate)
			_, err := DecodeDisplayCommand(raw, now)
			if err == nil || CommandErrorCode(err) != tc.code {
				t.Fatalf("err=%v code=%s", err, CommandErrorCode(err))
			}
		})
	}
}

func TestCommandParamsRejectUnknownControlAndBounds(t *testing.T) {
	tests := []string{
		`{"kind":"show_page","params":{"page_id":"API","command":"rm"},"ttl_seconds":30}`,
		`{"kind":"show_page","params":{"page_id":"OTHER"},"ttl_seconds":30}`,
		`{"kind":"show_message","params":{"text":"bad\u001b[2J","severity":"warning"},"ttl_seconds":30}`,
		`{"kind":"show_message","params":{"text":"ok","severity":"high"},"ttl_seconds":30}`,
		`{"kind":"set_brightness","params":{"level":101},"ttl_seconds":30}`,
		`{"kind":"next_page","params":{},"ttl_seconds":301}`,
		`{"kind":"next_page","params":{"duration_seconds":4611686018427387909},"ttl_seconds":30}`,
		`{"kind":"next_page","params":{},"ttl_seconds":4611686018427387909}`,
		`{"kind":"exec_shell","params":{},"ttl_seconds":30}`,
		`{"kind":"next_page","params":{},"ttl_seconds":30,"path":"/tmp"}`,
		`{"kind":"next_page","params":null,"ttl_seconds":30}`,
	}
	for _, raw := range tests {
		if _, err := DecodeCommandRequest([]byte(raw)); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
}

func TestMessageAuditDoesNotReturnMessage(t *testing.T) {
	length, hash := MessageAudit("maintenance soon")
	if length != 16 || len(hash) != 64 || hash == "maintenance soon" {
		t.Fatalf("length=%d hash=%q", length, hash)
	}
}
