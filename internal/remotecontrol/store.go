package remotecontrol

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/galendai/homepi-mon/internal/protocol"
)

const maxRecords = 256

type record struct {
	CommandID       string                 `json:"command_id"`
	DeviceID        string                 `json:"device_id,omitempty"`
	Sequence        uint64                 `json:"sequence"`
	Kind            protocol.CommandKind   `json:"kind"`
	IssuedAt        time.Time              `json:"issued_at,omitempty"`
	Result          protocol.CommandResult `json:"result"`
	PendingRefresh  bool                   `json:"pending_refresh,omitempty"`
	BaselineEpoch   string                 `json:"baseline_epoch,omitempty"`
	BaselineVersion uint64                 `json:"baseline_version,omitempty"`
}

type storeState struct {
	HighSequence uint64   `json:"high_sequence"`
	Records      []record `json:"records"`
}

type resultStore struct {
	path string
	now  func() time.Time
	mu   sync.Mutex
	data storeState
}

func openResultStore(path string, now func() time.Time) (*resultStore, error) {
	if now == nil {
		now = time.Now
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("remote control: create data directory: %w", err)
	}
	if err := os.Chmod(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("remote control: protect data directory: %w", err)
	}
	store := &resultStore{path: path, now: now}
	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		if decodeErr := json.Unmarshal(raw, &store.data); decodeErr != nil || len(store.data.Records) > maxRecords {
			quarantine := availableQuarantinePath(path, now().UTC().Unix())
			if renameErr := os.Rename(path, quarantine); renameErr != nil {
				return nil, fmt.Errorf("remote control: quarantine corrupt state: %w", renameErr)
			}
			store.data = storeState{}
			if persistErr := store.persistLocked(); persistErr != nil {
				return nil, persistErr
			}
		} else {
			changed := false
			stamp := now().UTC()
			for i := range store.data.Records {
				item := &store.data.Records[i]
				if item.Result.Status == protocol.CommandAccepted && !item.PendingRefresh {
					item.Result.Status = protocol.CommandFailed
					item.Result.Code = "interrupted"
					item.Result.CompletedAt = &stamp
					item.Result.DurationMS = stamp.Sub(item.Result.ReceivedAt).Milliseconds()
					changed = true
				}
			}
			if changed {
				if persistErr := store.persistLocked(); persistErr != nil {
					return nil, persistErr
				}
			} else if chmodErr := os.Chmod(path, 0o600); chmodErr != nil {
				return nil, fmt.Errorf("remote control: protect state: %w", chmodErr)
			}
		}
	case errors.Is(err, os.ErrNotExist):
		if err := store.persistLocked(); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("remote control: read state: %w", err)
	}
	return store, nil
}

func (s *resultStore) lookup(commandID string) (record, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.data.Records {
		if item.CommandID == commandID {
			return item, true
		}
	}
	return record{}, false
}

func (s *resultStore) accept(command protocol.DisplayCommand, baselineEpoch string,
	baselineVersion uint64) (record, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.data.Records {
		if item.CommandID == command.CommandID {
			return item, true, nil
		}
	}
	now := s.now().UTC()
	item := record{
		CommandID: command.CommandID, DeviceID: command.DeviceID,
		Sequence: command.Sequence, Kind: command.Kind, IssuedAt: command.IssuedAt,
		BaselineEpoch: baselineEpoch, BaselineVersion: baselineVersion,
		Result: protocol.CommandResult{
			CommandID: command.CommandID, Sequence: command.Sequence,
			Status: protocol.CommandAccepted, ReceivedAt: now,
		},
	}
	if command.Sequence <= s.data.HighSequence {
		item.Result.Status = protocol.CommandRejected
		item.Result.Code = "out_of_order"
		item.Result.CompletedAt = &now
		s.appendLocked(item)
		return item, false, s.persistLocked()
	}
	s.data.HighSequence = command.Sequence
	item.PendingRefresh = command.Kind == protocol.CommandRefreshData
	s.appendLocked(item)
	return item, false, s.persistLocked()
}

func (s *resultStore) reject(command protocol.DisplayCommand, status protocol.CommandStatus,
	code string) (record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.data.Records {
		if item.CommandID == command.CommandID {
			return item, nil
		}
	}
	now := s.now().UTC()
	item := record{CommandID: command.CommandID, DeviceID: command.DeviceID,
		Sequence: command.Sequence, Kind: command.Kind, IssuedAt: command.IssuedAt,
		Result: protocol.CommandResult{
			CommandID: command.CommandID, Sequence: command.Sequence, Status: status,
			Code: code, ReceivedAt: now, CompletedAt: &now,
		}}
	s.appendLocked(item)
	return item, s.persistLocked()
}

func (s *resultStore) complete(commandID string, status protocol.CommandStatus, code string) (record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.data.Records {
		item := &s.data.Records[i]
		if item.CommandID != commandID {
			continue
		}
		if item.Result.Status.Final() {
			return *item, nil
		}
		now := s.now().UTC()
		item.Result.Status = status
		item.Result.Code = code
		item.Result.CompletedAt = &now
		item.Result.DurationMS = now.Sub(item.Result.ReceivedAt).Milliseconds()
		item.PendingRefresh = false
		if err := s.persistLocked(); err != nil {
			return record{}, err
		}
		return *item, nil
	}
	return record{}, errors.New("remote control: command result was not found")
}

func (s *resultStore) pendingRefreshes() []record {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result []record
	for _, item := range s.data.Records {
		if item.PendingRefresh && item.Result.Status == protocol.CommandAccepted {
			result = append(result, item)
		}
	}
	return result
}

func (s *resultStore) appendLocked(item record) {
	s.data.Records = append(s.data.Records, item)
	if len(s.data.Records) > maxRecords {
		s.data.Records = append([]record(nil), s.data.Records[len(s.data.Records)-maxRecords:]...)
	}
}

func (s *resultStore) persistLocked() error {
	body, err := json.Marshal(s.data)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".remote-results-*.tmp")
	if err != nil {
		return err
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
	// Rename makes the ledger atomically visible to a restarted process. Do not
	// force the file or directory to stable storage: low-end SD cards can stall
	// either sync for seconds and violate the display command latency budget. An
	// abrupt power loss may lose the newest buffered ledger update, but a normal
	// process or service restart remains idempotent.
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmp.Name(), s.path); err != nil {
		cleanup()
		return err
	}
	return nil
}

func availableQuarantinePath(path string, timestamp int64) string {
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
