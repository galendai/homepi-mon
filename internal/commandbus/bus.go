// Package commandbus owns the daemon-side bounded command queue and result audit.
package commandbus

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/galendai/homepi-mon/internal/protocol"
)

const (
	MaxPending = 64
	MaxResults = 256
)

var ErrNotFound = errors.New("command not found")

type Entry struct {
	Command protocol.DisplayCommand `json:"command"`
	Result  protocol.CommandResult  `json:"result"`
}

type stateFile struct {
	Sequence uint64  `json:"sequence"`
	Entries  []Entry `json:"entries"`
}

type Bus struct {
	path string
	now  func() time.Time

	mu     sync.Mutex
	state  stateFile
	notify map[string]chan struct{}
}

func Open(path string, now func() time.Time) (*Bus, error) {
	if path == "" {
		return nil, errors.New("commandbus: state path is required")
	}
	if now == nil {
		now = time.Now
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("commandbus: create state directory: %w", err)
	}
	if err := os.Chmod(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("commandbus: protect state directory: %w", err)
	}
	b := &Bus{path: path, now: now, notify: make(map[string]chan struct{})}
	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		decodeErr := json.Unmarshal(raw, &b.state)
		if decodeErr != nil || len(b.state.Entries) > MaxPending+MaxResults {
			quarantine := availableStatePath(path, now().UTC().Unix())
			if renameErr := os.Rename(path, quarantine); renameErr != nil {
				return nil, fmt.Errorf("commandbus: quarantine corrupt state: %w", renameErr)
			}
			b.state = stateFile{}
			if persistErr := b.persistLocked(); persistErr != nil {
				return nil, persistErr
			}
		} else if err := os.Chmod(path, 0o600); err != nil {
			return nil, fmt.Errorf("commandbus: protect state: %w", err)
		}
	case errors.Is(err, os.ErrNotExist):
		if err := b.persistLocked(); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("commandbus: read state: %w", err)
	}
	return b, nil
}

func (b *Bus) Publish(deviceID string, request protocol.CommandRequest) (Entry, error) {
	if err := protocol.ValidateCommandRequest(request); err != nil {
		return Entry{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	previous := cloneState(b.state)

	if b.pendingCountLocked() >= MaxPending && !b.dropOldestNormalLocked() {
		return Entry{}, errors.New("command queue is full")
	}
	commandID, err := newUUID()
	if err != nil {
		return Entry{}, err
	}
	now := b.now().UTC()
	b.state.Sequence++
	command := protocol.DisplayCommand{
		SchemaMajor: protocol.CommandSchemaMajor,
		CommandID:   commandID,
		DeviceID:    deviceID,
		Kind:        request.Kind,
		Params:      append(json.RawMessage(nil), request.Params...),
		IssuedAt:    now,
		ExpiresAt:   now.Add(time.Duration(request.TTLSeconds) * time.Second),
		Priority:    request.Priority,
		Sequence:    b.state.Sequence,
	}
	entry := Entry{Command: command, Result: protocol.CommandResult{
		CommandID: command.CommandID, Sequence: command.Sequence,
		Status: protocol.CommandPublished, ReceivedAt: now,
	}}
	b.state.Entries = append(b.state.Entries, entry)
	b.trimLocked()
	if err := b.persistLocked(); err != nil {
		b.state = previous
		return Entry{}, err
	}
	b.signalLocked(deviceID)
	return cloneEntry(entry), nil
}

func (b *Bus) Pending(deviceID string, sent map[string]bool) ([]protocol.DisplayCommand, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	previous := cloneState(b.state)
	changed := b.expireLocked()
	commands := make([]protocol.DisplayCommand, 0)
	for i := range b.state.Entries {
		entry := b.state.Entries[i]
		if entry.Command.DeviceID == deviceID && !entry.Result.Status.Final() && !sent[entry.Command.CommandID] {
			commands = append(commands, cloneCommand(entry.Command))
		}
	}
	if changed {
		if err := b.persistLocked(); err != nil {
			b.state = previous
			return nil, err
		}
	}
	return commands, nil
}

func (b *Bus) Record(deviceID string, result protocol.CommandResult) (Entry, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	previous := cloneState(b.state)
	for i := range b.state.Entries {
		entry := &b.state.Entries[i]
		if entry.Command.CommandID != result.CommandID {
			continue
		}
		if entry.Command.DeviceID != deviceID {
			return Entry{}, false, errors.New("command result device mismatch")
		}
		if result.Sequence != entry.Command.Sequence {
			return Entry{}, false, errors.New("command result sequence mismatch")
		}
		if entry.Result.Status.Final() {
			return cloneEntry(*entry), false, nil
		}
		if result.Status != protocol.CommandAccepted && !result.Status.Final() {
			return Entry{}, false, errors.New("command result has an invalid status")
		}
		firstAccepted := result.Status == protocol.CommandAccepted && entry.Result.Status != protocol.CommandAccepted
		entry.Result = result
		if entry.Result.ReceivedAt.IsZero() {
			entry.Result.ReceivedAt = b.now().UTC()
		}
		if result.Status.Final() {
			entry.Command.Params = redactParams(entry.Command)
		}
		updated := cloneEntry(*entry)
		deviceID := entry.Command.DeviceID
		b.trimLocked()
		if err := b.persistLocked(); err != nil {
			b.state = previous
			return Entry{}, false, err
		}
		b.signalLocked(deviceID)
		return updated, firstAccepted, nil
	}
	return Entry{}, false, ErrNotFound
}

func (b *Bus) Get(commandID string) (Entry, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	previous := cloneState(b.state)
	changed := b.expireLocked()
	for i := range b.state.Entries {
		if b.state.Entries[i].Command.CommandID == commandID {
			if changed {
				if err := b.persistLocked(); err != nil {
					b.state = previous
					return Entry{}, err
				}
			}
			return cloneEntry(b.state.Entries[i]), nil
		}
	}
	return Entry{}, ErrNotFound
}

func (b *Bus) Subscribe(deviceID string) <-chan struct{} {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := b.notify[deviceID]
	if ch == nil {
		ch = make(chan struct{}, 1)
		b.notify[deviceID] = ch
	}
	return ch
}

func (b *Bus) pendingCountLocked() int {
	count := 0
	for _, entry := range b.state.Entries {
		if !entry.Result.Status.Final() {
			count++
		}
	}
	return count
}

func (b *Bus) dropOldestNormalLocked() bool {
	for i := range b.state.Entries {
		entry := &b.state.Entries[i]
		if entry.Command.Priority != protocol.PriorityNormal || entry.Result.Status != protocol.CommandPublished {
			continue
		}
		now := b.now().UTC()
		entry.Result.Status = protocol.CommandFailed
		entry.Result.Code = "overflow"
		entry.Result.CompletedAt = &now
		entry.Command.Params = redactParams(entry.Command)
		return true
	}
	return false
}

func (b *Bus) expireLocked() bool {
	changed := false
	now := b.now().UTC()
	for i := range b.state.Entries {
		entry := &b.state.Entries[i]
		if entry.Result.Status.Final() {
			continue
		}
		switch {
		case entry.Result.Status == protocol.CommandPublished && !now.Before(entry.Command.ExpiresAt):
			entry.Result.Status = protocol.CommandExpired
			entry.Result.Code = "expired"
		case entry.Result.Status == protocol.CommandAccepted &&
			now.Sub(entry.Result.ReceivedAt) >= protocol.MaxCommandTTL:
			entry.Result.Status = protocol.CommandFailed
			entry.Result.Code = "execution_timeout"
		default:
			continue
		}
		entry.Result.CompletedAt = &now
		entry.Result.DurationMS = now.Sub(entry.Result.ReceivedAt).Milliseconds()
		entry.Command.Params = redactParams(entry.Command)
		changed = true
	}
	return changed
}

func (b *Bus) trimLocked() {
	finals := 0
	for _, entry := range b.state.Entries {
		if entry.Result.Status.Final() {
			finals++
		}
	}
	for finals > MaxResults {
		removed := false
		for i, entry := range b.state.Entries {
			if entry.Result.Status.Final() {
				b.state.Entries = append(b.state.Entries[:i], b.state.Entries[i+1:]...)
				finals--
				removed = true
				break
			}
		}
		if !removed {
			return
		}
	}
}

func (b *Bus) signalLocked(deviceID string) {
	if ch := b.notify[deviceID]; ch != nil {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (b *Bus) persistLocked() error {
	body, err := json.Marshal(b.state)
	if err != nil {
		return fmt.Errorf("commandbus: encode state: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(b.path), ".commands-*.tmp")
	if err != nil {
		return fmt.Errorf("commandbus: create state: %w", err)
	}
	cleanup := func() { _ = os.Remove(tmp.Name()) }
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	// Rename makes the queue atomically visible to a restarted process. Avoid
	// file and directory fsync on the interactive command path: consumer disks
	// can stall those calls beyond the one-second display latency budget. A
	// sudden power loss may lose the newest buffered state, but a normal process
	// or service restart remains recoverable.
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmp.Name(), b.path); err != nil {
		cleanup()
		return err
	}
	return nil
}

func redactParams(command protocol.DisplayCommand) json.RawMessage {
	if command.Kind != protocol.CommandShowMessage {
		return json.RawMessage(`{}`)
	}
	params, err := protocol.DecodeCommandParams[protocol.MessageCommandParams](command.Params)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	length, hash := protocol.MessageAudit(params.Text)
	body, _ := json.Marshal(map[string]any{
		"text_length": length, "text_sha256": hash,
		"severity": params.Severity, "duration_seconds": params.DurationSeconds,
	})
	return body
}

func cloneEntry(entry Entry) Entry {
	entry.Command = cloneCommand(entry.Command)
	return entry
}

func cloneCommand(command protocol.DisplayCommand) protocol.DisplayCommand {
	command.Params = append(json.RawMessage(nil), command.Params...)
	return command
}

func cloneState(state stateFile) stateFile {
	clone := stateFile{Sequence: state.Sequence, Entries: make([]Entry, len(state.Entries))}
	for i := range state.Entries {
		clone.Entries[i] = cloneEntry(state.Entries[i])
	}
	return clone
}

func availableStatePath(path string, timestamp int64) string {
	base := path + ".corrupt-" + fmt.Sprint(timestamp)
	for index := 0; ; index++ {
		candidate := base
		if index > 0 {
			candidate += "-" + fmt.Sprint(index)
		}
		if _, err := os.Lstat(candidate); err != nil {
			return candidate
		}
	}
}

func newUUID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("commandbus: generate command ID: %w", err)
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	raw := hex.EncodeToString(value[:])
	return raw[:8] + "-" + raw[8:12] + "-" + raw[12:16] + "-" + raw[16:20] + "-" + raw[20:], nil
}
