package protocol

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// MessageType enumerates the WebSocket stream messages (HL-Spec 6.2).
type MessageType string

const (
	// Client to server.
	MsgHello         MessageType = "hello"
	MsgAck           MessageType = "ack"
	MsgCommandResult MessageType = "command_result"
	MsgDeviceHealth  MessageType = "device_health"

	// Server to client.
	MsgSnapshotFull    MessageType = "snapshot_full"
	MsgSnapshotDelta   MessageType = "snapshot_delta"
	MsgConnectorHealth MessageType = "connector_health"
	MsgDisplayCommand  MessageType = "display_command"
	MsgHeartbeat       MessageType = "heartbeat"
	MsgError           MessageType = "error"
)

// Envelope wraps every stream message so both sides can route on Type without
// speculatively decoding the payload.
type Envelope struct {
	Type    MessageType     `json:"type"`
	SentAt  time.Time       `json:"sent_at"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Hello is the first client message. The server uses it to decide between a
// delta and a full snapshot (MOD-001 9.2).
type Hello struct {
	DeviceID            string `json:"device_id"`
	ClientVersion       string `json:"client_version"`
	SchemaMajor         int    `json:"schema_major"`
	LastEpoch           string `json:"last_epoch,omitempty"`
	LastSnapshotVersion uint64 `json:"last_snapshot_version,omitempty"`
	TerminalCols        int    `json:"terminal_cols,omitempty"`
	TerminalRows        int    `json:"terminal_rows,omitempty"`
}

// Ack confirms a received snapshot version so the server can trim its buffer.
type Ack struct {
	Epoch           string `json:"epoch"`
	SnapshotVersion uint64 `json:"snapshot_version"`
}

// Heartbeat keeps the stream alive and carries the server's expected interval
// so the client can size its own read deadline (HL-Spec 6.2).
type Heartbeat struct {
	Interval Duration `json:"interval"`
}

// DeviceHealth is the display's own status, reported back for diagnostics.
// It contains no provider data.
type DeviceHealth struct {
	DeviceID     string     `json:"device_id"`
	TempC        float64    `json:"temp_c,omitempty"`
	CPUPercent   int        `json:"cpu_percent"`
	MemPercent   int        `json:"mem_percent"`
	TerminalCols int        `json:"terminal_cols"`
	TerminalRows int        `json:"terminal_rows"`
	LastRenderAt *time.Time `json:"last_render_at,omitempty"`
}

// StreamError is a redacted, classified error sent to the client.
type StreamError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Error implements the error interface for convenience at call sites.
func (e StreamError) Error() string { return e.Code + ": " + e.Message }

// Encode builds an envelope with the payload marshalled.
func Encode(t MessageType, now time.Time, payload any) ([]byte, error) {
	env := Envelope{Type: t, SentAt: now}
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("encode %s: %w", t, err)
		}
		env.Payload = raw
	}
	return json.Marshal(env)
}

// DecodeEnvelope parses the outer envelope only.
func DecodeEnvelope(raw []byte) (Envelope, error) {
	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return Envelope{}, fmt.Errorf("decode envelope: %w", err)
	}
	if env.Type == "" {
		return Envelope{}, errors.New("decode envelope: missing type")
	}
	return env, nil
}

// DecodePayload unmarshals an envelope payload into out.
//
// Unknown fields are ignored on purpose: HL-Spec 6.3 requires a client to keep
// rendering when a newer minor schema adds optional fields.
func DecodePayload(env Envelope, out any) error {
	if len(env.Payload) == 0 {
		return fmt.Errorf("decode %s: empty payload", env.Type)
	}
	if err := json.Unmarshal(env.Payload, out); err != nil {
		return fmt.Errorf("decode %s: %w", env.Type, err)
	}
	return nil
}

// DecodeSnapshot parses and validates a snapshot payload.
// It rejects an unsupported major schema version before touching the data.
func DecodeSnapshot(raw []byte) (*MetricSnapshot, error) {
	var probe struct {
		SchemaVersion string `json:"schema_version"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, fmt.Errorf("decode snapshot: %w", err)
	}
	if err := CheckSchema(probe.SchemaVersion); err != nil {
		return nil, err
	}
	var snap MetricSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return nil, fmt.Errorf("decode snapshot: %w", err)
	}
	if err := snap.Validate(); err != nil {
		return nil, fmt.Errorf("decode snapshot: %w", err)
	}
	return &snap, nil
}
