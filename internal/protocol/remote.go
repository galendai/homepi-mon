package protocol

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

const (
	CommandSchemaMajor   = 1
	MaxCommandBytes      = 8 << 10
	MaxCommandTTL        = 5 * time.Minute
	MaxCommandClockSkew  = 30 * time.Second
	MinCommandDuration   = 5 * time.Second
	MaxCommandDuration   = 5 * time.Minute
	DefaultCommandTTL    = 30 * time.Second
	DefaultPageDuration  = 30 * time.Second
	DefaultMessageLength = 116
	MaxConnectorIDs      = 16
)

type CommandKind string

const (
	CommandShowPage      CommandKind = "show_page"
	CommandNextPage      CommandKind = "next_page"
	CommandPreviousPage  CommandKind = "previous_page"
	CommandSetRotation   CommandKind = "set_rotation"
	CommandRefreshData   CommandKind = "refresh_data"
	CommandShowMessage   CommandKind = "show_message"
	CommandSetBrightness CommandKind = "set_brightness"
)

type CommandPriority string

const (
	PriorityNormal    CommandPriority = "normal"
	PriorityHigh      CommandPriority = "high"
	PriorityEmergency CommandPriority = "emergency"
)

type CommandStatus string

const (
	CommandPublished CommandStatus = "published"
	CommandAccepted  CommandStatus = "accepted"
	CommandExecuted  CommandStatus = "executed"
	CommandRejected  CommandStatus = "rejected"
	CommandExpired   CommandStatus = "expired"
	CommandFailed    CommandStatus = "failed"
)

func (s CommandStatus) Final() bool {
	return s == CommandExecuted || s == CommandRejected || s == CommandExpired || s == CommandFailed
}

type DisplayCommand struct {
	SchemaMajor int             `json:"schema_major"`
	CommandID   string          `json:"command_id"`
	DeviceID    string          `json:"device_id"`
	Kind        CommandKind     `json:"kind"`
	Params      json.RawMessage `json:"params"`
	IssuedAt    time.Time       `json:"issued_at"`
	ExpiresAt   time.Time       `json:"expires_at"`
	Priority    CommandPriority `json:"priority"`
	Sequence    uint64          `json:"sequence"`
}

type CommandRequest struct {
	Kind       CommandKind     `json:"kind"`
	Params     json.RawMessage `json:"params"`
	Priority   CommandPriority `json:"priority,omitempty"`
	TTLSeconds int             `json:"ttl_seconds,omitempty"`
}

type CommandResult struct {
	CommandID   string        `json:"command_id"`
	Sequence    uint64        `json:"sequence"`
	Status      CommandStatus `json:"status"`
	Code        string        `json:"code,omitempty"`
	ReceivedAt  time.Time     `json:"received_at"`
	CompletedAt *time.Time    `json:"completed_at,omitempty"`
	DurationMS  int64         `json:"duration_ms,omitempty"`
}

type PageCommandParams struct {
	PageID          string `json:"page_id"`
	DurationSeconds int    `json:"duration_seconds,omitempty"`
}

type StepPageCommandParams struct {
	DurationSeconds int `json:"duration_seconds,omitempty"`
}

type RotationCommandParams struct {
	Enabled         bool `json:"enabled"`
	IntervalSeconds int  `json:"interval_seconds,omitempty"`
	DurationSeconds int  `json:"duration_seconds,omitempty"`
}

type RefreshCommandParams struct {
	ConnectorIDs []string `json:"connector_ids,omitempty"`
}

type MessageCommandParams struct {
	Text            string `json:"text"`
	Severity        string `json:"severity"`
	DurationSeconds int    `json:"duration_seconds,omitempty"`
}

type BrightnessCommandParams struct {
	Level int `json:"level"`
}

type CommandValidationError struct {
	Code string
	Err  error
}

func (e *CommandValidationError) Error() string { return e.Code + ": " + e.Err.Error() }
func (e *CommandValidationError) Unwrap() error { return e.Err }

func commandError(code, message string) error {
	return &CommandValidationError{Code: code, Err: errors.New(message)}
}

func CommandErrorCode(err error) string {
	var target *CommandValidationError
	if errors.As(err, &target) {
		return target.Code
	}
	return "invalid_command"
}

func DecodeCommandRequest(raw []byte) (CommandRequest, error) {
	if len(raw) == 0 || len(raw) > MaxCommandBytes {
		return CommandRequest{}, commandError("invalid_size", "command request size is invalid")
	}
	var request CommandRequest
	if err := strictJSON(raw, &request); err != nil {
		return CommandRequest{}, commandError("invalid_schema", "command request does not match the schema")
	}
	if request.Priority == "" {
		request.Priority = defaultPriority(request.Kind)
	}
	if request.TTLSeconds == 0 {
		request.TTLSeconds = int(DefaultCommandTTL / time.Second)
	}
	if err := ValidateCommandRequest(request); err != nil {
		return CommandRequest{}, err
	}
	return request, nil
}

func DecodeDisplayCommand(raw []byte, now time.Time) (DisplayCommand, error) {
	if len(raw) == 0 || len(raw) > MaxCommandBytes {
		return DisplayCommand{}, commandError("invalid_size", "display command size is invalid")
	}
	var command DisplayCommand
	if err := strictJSON(raw, &command); err != nil {
		return DisplayCommand{}, commandError("invalid_schema", "display command does not match the schema")
	}
	if err := command.Validate(now); err != nil {
		return command, err
	}
	return command, nil
}

func DecodeCommandResult(raw []byte) (CommandResult, error) {
	if len(raw) == 0 || len(raw) > MaxCommandBytes {
		return CommandResult{}, commandError("invalid_size", "command result size is invalid")
	}
	var result CommandResult
	if err := strictJSON(raw, &result); err != nil {
		return CommandResult{}, commandError("invalid_schema", "command result does not match the schema")
	}
	if !validUUID(result.CommandID) || result.Sequence == 0 ||
		(result.Status != CommandAccepted && !result.Status.Final()) || result.ReceivedAt.IsZero() {
		return CommandResult{}, commandError("invalid_result", "command result fields are invalid")
	}
	if result.Status.Final() && result.CompletedAt == nil {
		return CommandResult{}, commandError("invalid_result", "final command result is missing completed_at")
	}
	if result.CompletedAt != nil && result.CompletedAt.Before(result.ReceivedAt) {
		return CommandResult{}, commandError("invalid_result", "command completion precedes receipt")
	}
	if result.DurationMS < 0 || result.DurationMS > MaxCommandDuration.Milliseconds() {
		return CommandResult{}, commandError("invalid_result", "command result duration is invalid")
	}
	if len(result.Code) > 64 || (result.Code != "" && !safeCommandID(result.Code)) {
		return CommandResult{}, commandError("invalid_result", "command result code is invalid")
	}
	return result, nil
}

func ValidateCommandRequest(request CommandRequest) error {
	if !request.Kind.valid() {
		return commandError("unknown_command", "command kind is not allowed")
	}
	if !request.Priority.valid() {
		return commandError("invalid_priority", "command priority is invalid")
	}
	if request.Priority != defaultPriority(request.Kind) {
		return commandError("invalid_priority", "command priority does not match its kind")
	}
	if request.TTLSeconds < int(MinCommandDuration/time.Second) ||
		request.TTLSeconds > int(MaxCommandTTL/time.Second) {
		return commandError("invalid_ttl", "command TTL must be 5..300 seconds")
	}
	return validateCommandParams(request.Kind, request.Params)
}

func (command DisplayCommand) Validate(now time.Time) error {
	if command.SchemaMajor != CommandSchemaMajor {
		return commandError("schema_unsupported", "command schema major is not supported")
	}
	if !validUUID(command.CommandID) {
		return commandError("invalid_command_id", "command ID must be a canonical UUID")
	}
	if !safeCommandID(command.DeviceID) {
		return commandError("wrong_target", "device ID is invalid")
	}
	if command.Sequence == 0 {
		return commandError("invalid_sequence", "sequence must be positive")
	}
	if !command.Kind.valid() {
		return commandError("unknown_command", "command kind is not allowed")
	}
	if !command.Priority.valid() {
		return commandError("invalid_priority", "command priority is invalid")
	}
	if command.Priority != defaultPriority(command.Kind) {
		return commandError("invalid_priority", "command priority does not match its kind")
	}
	if command.IssuedAt.IsZero() || command.ExpiresAt.IsZero() || !command.ExpiresAt.After(command.IssuedAt) ||
		command.ExpiresAt.Sub(command.IssuedAt) > MaxCommandTTL {
		return commandError("invalid_time", "command timestamps are invalid")
	}
	if command.IssuedAt.After(now.Add(MaxCommandClockSkew)) {
		return commandError("issued_in_future", "command was issued too far in the future")
	}
	if !now.Before(command.ExpiresAt) {
		return commandError("expired", "command has expired")
	}
	return validateCommandParams(command.Kind, command.Params)
}

func DecodeCommandParams[T any](raw json.RawMessage) (T, error) {
	var value T
	if err := strictJSON(raw, &value); err != nil {
		return value, err
	}
	return value, nil
}

func validateCommandParams(kind CommandKind, raw json.RawMessage) error {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) < 2 || trimmed[0] != '{' || trimmed[len(trimmed)-1] != '}' {
		return commandError("invalid_param", "command parameters must be a JSON object")
	}
	switch kind {
	case CommandShowPage:
		params, err := DecodeCommandParams[PageCommandParams](raw)
		if err != nil {
			return commandError("invalid_param", "show_page parameters are invalid")
		}
		if !validPage(params.PageID) || !validOptionalDuration(params.DurationSeconds) {
			return commandError("invalid_param", "show_page page or duration is invalid")
		}
	case CommandNextPage, CommandPreviousPage:
		params, err := DecodeCommandParams[StepPageCommandParams](raw)
		if err != nil || !validOptionalDuration(params.DurationSeconds) {
			return commandError("invalid_param", "page step duration is invalid")
		}
	case CommandSetRotation:
		params, err := DecodeCommandParams[RotationCommandParams](raw)
		if err != nil || !validOptionalDuration(params.DurationSeconds) ||
			(params.IntervalSeconds != 0 && !validDuration(params.IntervalSeconds)) {
			return commandError("invalid_param", "rotation parameters are invalid")
		}
	case CommandRefreshData:
		params, err := DecodeCommandParams[RefreshCommandParams](raw)
		if err != nil || len(params.ConnectorIDs) > MaxConnectorIDs {
			return commandError("invalid_param", "refresh parameters are invalid")
		}
		seen := make(map[string]bool, len(params.ConnectorIDs))
		for _, id := range params.ConnectorIDs {
			if !safeCommandID(id) || seen[id] {
				return commandError("invalid_param", "connector IDs are invalid")
			}
			seen[id] = true
		}
	case CommandShowMessage:
		params, err := DecodeCommandParams[MessageCommandParams](raw)
		if err != nil || !validOptionalDuration(params.DurationSeconds) || !validMessage(params.Text) ||
			(params.Severity != "info" && params.Severity != "warning" && params.Severity != "critical") {
			return commandError("invalid_param", "message parameters are invalid")
		}
	case CommandSetBrightness:
		params, err := DecodeCommandParams[BrightnessCommandParams](raw)
		if err != nil || params.Level < 0 || params.Level > 100 {
			return commandError("invalid_param", "brightness level must be 0..100")
		}
	}
	return nil
}

func (kind CommandKind) valid() bool {
	switch kind {
	case CommandShowPage, CommandNextPage, CommandPreviousPage, CommandSetRotation,
		CommandRefreshData, CommandShowMessage, CommandSetBrightness:
		return true
	}
	return false
}

func (priority CommandPriority) valid() bool {
	return priority == PriorityNormal || priority == PriorityHigh || priority == PriorityEmergency
}

func defaultPriority(kind CommandKind) CommandPriority {
	if kind == CommandShowMessage {
		return PriorityHigh
	}
	return PriorityNormal
}

func validPage(value string) bool {
	switch strings.ToUpper(value) {
	case "CODING", "API", "HOMELAB", "SERVICES", "SYSTEM":
		return true
	}
	return false
}

func validOptionalDuration(seconds int) bool { return seconds == 0 || validDuration(seconds) }

func validDuration(seconds int) bool {
	return seconds >= int(MinCommandDuration/time.Second) &&
		seconds <= int(MaxCommandDuration/time.Second)
}

func validMessage(value string) bool {
	if value == "" || len(value) > DefaultMessageLength {
		return false
	}
	for i := 0; i < len(value); i++ {
		if value[i] < 0x20 || value[i] > 0x7e {
			return false
		}
	}
	return true
}

func safeCommandID(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
			r == '-' || r == '_' || r == '.' {
			continue
		}
		return false
	}
	return true
}

func validUUID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	for i, c := range []byte(value) {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			continue
		}
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return value[14] == '4' && (value[19] == '8' || value[19] == '9' || value[19] == 'a' || value[19] == 'b')
}

func MessageAudit(value string) (int, string) {
	sum := sha256.Sum256([]byte(value))
	return len(value), hex.EncodeToString(sum[:])
}

func strictJSON(raw []byte, out any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("trailing JSON value")
	}
	return nil
}
