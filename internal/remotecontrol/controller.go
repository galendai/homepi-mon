// Package remotecontrol validates and executes allowlisted display commands.
package remotecontrol

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"github.com/galendai/homepi-mon/internal/protocol"
	"github.com/galendai/homepi-mon/internal/ui"
)

type Brightness interface {
	Set(context.Context, int) error
}

type Options struct {
	DataDir    string
	DeviceID   string
	SourceNode string
	Router     *ui.Router
	Brightness Brightness
	Logger     *slog.Logger
	Now        func() time.Time
}

type Controller struct {
	deviceID   string
	sourceNode string
	router     *ui.Router
	brightness Brightness
	log        *slog.Logger
	now        func() time.Time
	store      *resultStore

	mu          sync.Mutex
	notice      *noticeState
	lastEpoch   string
	lastVersion uint64
}

type noticeState struct {
	text     string
	severity string
	expires  time.Time
}

func New(opts Options) (*Controller, error) {
	if opts.DataDir == "" || opts.DeviceID == "" || opts.SourceNode == "" || opts.Router == nil {
		return nil, errors.New("remote control: data dir, device, source node and router are required")
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	store, err := openResultStore(filepath.Join(opts.DataDir, "command-results.json"), now)
	if err != nil {
		return nil, err
	}
	return &Controller{
		deviceID: opts.DeviceID, sourceNode: opts.SourceNode, router: opts.Router,
		brightness: opts.Brightness, log: log, now: now, store: store,
	}, nil
}

func (c *Controller) Handle(ctx context.Context, command protocol.DisplayCommand) []protocol.CommandResult {
	if existing, ok := c.store.lookup(command.CommandID); ok {
		return []protocol.CommandResult{existing.Result}
	}
	now := c.now().UTC()
	if err := command.Validate(now); err != nil {
		status := protocol.CommandRejected
		if protocol.CommandErrorCode(err) == "expired" {
			status = protocol.CommandExpired
		}
		item, saveErr := c.store.reject(command, status, protocol.CommandErrorCode(err))
		if saveErr != nil {
			c.log.Warn("remote command rejection was not persisted", "error", saveErr.Error())
		}
		return []protocol.CommandResult{item.Result}
	}
	if command.DeviceID != c.deviceID {
		item, err := c.store.reject(command, protocol.CommandRejected, "wrong_target")
		if err != nil {
			c.log.Warn("remote command rejection was not persisted", "error", err.Error())
		}
		return []protocol.CommandResult{item.Result}
	}

	c.mu.Lock()
	baselineEpoch, baselineVersion := c.lastEpoch, c.lastVersion
	c.mu.Unlock()
	item, duplicate, err := c.store.accept(command, baselineEpoch, baselineVersion)
	if err != nil {
		return []protocol.CommandResult{c.transientFailure(command, "state_unavailable")}
	}
	if duplicate || item.Result.Status.Final() {
		return []protocol.CommandResult{item.Result}
	}
	results := []protocol.CommandResult{item.Result}
	if command.Kind == protocol.CommandRefreshData {
		c.audit(command, item.Result)
		return results
	}

	code := ""
	status := protocol.CommandExecuted
	switch command.Kind {
	case protocol.CommandShowPage:
		params, _ := protocol.DecodeCommandParams[protocol.PageCommandParams](command.Params)
		page, _ := ui.ParsePage(params.PageID)
		c.router.ShowPage(page, commandDuration(params.DurationSeconds, protocol.DefaultPageDuration), now)
	case protocol.CommandNextPage:
		params, _ := protocol.DecodeCommandParams[protocol.StepPageCommandParams](command.Params)
		c.router.NextPage(commandDuration(params.DurationSeconds, protocol.DefaultPageDuration), now)
	case protocol.CommandPreviousPage:
		params, _ := protocol.DecodeCommandParams[protocol.StepPageCommandParams](command.Params)
		c.router.PreviousPage(commandDuration(params.DurationSeconds, protocol.DefaultPageDuration), now)
	case protocol.CommandSetRotation:
		params, _ := protocol.DecodeCommandParams[protocol.RotationCommandParams](command.Params)
		c.router.SetRotation(params.Enabled, time.Duration(params.IntervalSeconds)*time.Second,
			commandDuration(params.DurationSeconds, 5*time.Minute), now)
	case protocol.CommandShowMessage:
		params, _ := protocol.DecodeCommandParams[protocol.MessageCommandParams](command.Params)
		c.mu.Lock()
		c.notice = &noticeState{
			text: params.Text, severity: params.Severity,
			expires: now.Add(commandDuration(params.DurationSeconds, protocol.DefaultPageDuration)),
		}
		c.mu.Unlock()
	case protocol.CommandSetBrightness:
		params, _ := protocol.DecodeCommandParams[protocol.BrightnessCommandParams](command.Params)
		if c.brightness == nil {
			status, code = protocol.CommandFailed, "unsupported_capability"
		} else if err := c.brightness.Set(ctx, params.Level); err != nil {
			status, code = protocol.CommandFailed, "brightness_failed"
		}
	}
	completed, err := c.store.complete(command.CommandID, status, code)
	if err != nil {
		results = append(results, c.transientFailure(command, "state_unavailable"))
		return results
	}
	c.audit(command, completed.Result)
	return append(results, completed.Result)
}

func (c *Controller) SnapshotApplied(snapshot *protocol.MetricSnapshot) []protocol.CommandResult {
	if snapshot == nil {
		return nil
	}
	c.mu.Lock()
	c.lastEpoch, c.lastVersion = snapshot.SourceEpoch, snapshot.SnapshotVersion
	c.mu.Unlock()
	var results []protocol.CommandResult
	for _, pending := range c.store.pendingRefreshes() {
		advanced := snapshot.SourceEpoch != pending.BaselineEpoch || snapshot.SnapshotVersion > pending.BaselineVersion
		if !advanced {
			continue
		}
		completed, err := c.store.complete(pending.CommandID, protocol.CommandExecuted, "")
		if err == nil {
			results = append(results, completed.Result)
			c.log.Info("display command result", "command_id", pending.CommandID,
				"kind", pending.Kind, "status", completed.Result.Status,
				"duration_ms", completed.Result.DurationMS)
		}
	}
	return results
}

func (c *Controller) Notice(now time.Time) *ui.RemoteNotice {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.notice == nil || !now.Before(c.notice.expires) {
		c.notice = nil
		return nil
	}
	return &ui.RemoteNotice{
		Text: c.notice.text, Severity: c.notice.severity,
		Source: c.sourceNode, ReturnIn: c.notice.expires.Sub(now),
	}
}

func (c *Controller) transientFailure(command protocol.DisplayCommand, code string) protocol.CommandResult {
	now := c.now().UTC()
	return protocol.CommandResult{
		CommandID: command.CommandID, Sequence: command.Sequence,
		Status: protocol.CommandFailed, Code: code, ReceivedAt: now, CompletedAt: &now,
	}
}

func (c *Controller) audit(command protocol.DisplayCommand, result protocol.CommandResult) {
	args := []any{
		"command_id", command.CommandID, "kind", command.Kind, "device", command.DeviceID,
		"sequence", command.Sequence, "issued_at", command.IssuedAt,
		"received_at", result.ReceivedAt, "completed_at", result.CompletedAt,
		"status", result.Status, "code", result.Code, "duration_ms", result.DurationMS,
	}
	if command.Kind == protocol.CommandShowMessage {
		params, err := protocol.DecodeCommandParams[protocol.MessageCommandParams](command.Params)
		if err == nil {
			length, hash := protocol.MessageAudit(params.Text)
			args = append(args, "message_length", length, "message_sha256", hash)
		}
	}
	c.log.Info("display command result", args...)
}

func commandDuration(seconds int, fallback time.Duration) time.Duration {
	if seconds == 0 {
		return fallback
	}
	return time.Duration(seconds) * time.Second
}
