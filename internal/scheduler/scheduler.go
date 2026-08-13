package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"github.com/galendai/homepi-mon/internal/connector"
	"github.com/galendai/homepi-mon/internal/protocol"
	"github.com/galendai/homepi-mon/internal/state"
)

// DefaultMaxConcurrency is the MOD-001 6.1 global default.
const DefaultMaxConcurrency = 4

// Task binds one connector to its schedule.
type Task struct {
	Connector connector.Connector
	Policy    Policy
	// Timeout bounds a single Collect call.
	Timeout time.Duration
	// Enabled false publishes a disabled connector and never collects.
	Enabled bool
	// StaleAfter is the provider-level freshness budget applied uniformly to
	// every metric before it enters current state.
	StaleAfter time.Duration
}

// HealthReporter is implemented by connectors that publish their own health
// records in addition to metrics.
type HealthReporter interface {
	Health() ([]protocol.ConnectorHealth, error)
}

// Scheduler runs collection tasks with bounded concurrency.
type Scheduler struct {
	tasks     []Task
	store     *state.Current
	sem       chan struct{}
	log       *slog.Logger
	now       func() time.Time
	rand      func() float64
	nodeLabel string

	seqMu  sync.Mutex
	seq    uint64
	taskMu []sync.Mutex
}

// Options configures a Scheduler.
type Options struct {
	// Store receives every collection result. Required.
	Store *state.Current
	// MaxConcurrency bounds simultaneous collections; defaults to 4.
	MaxConcurrency int
	// NodeLabel is used in the user-facing re-authentication hint.
	NodeLabel string
	// Logger receives redacted diagnostics.
	Logger *slog.Logger
	// Now and Rand are injectable for deterministic tests.
	Now  func() time.Time
	Rand func() float64
}

// New builds a Scheduler.
func New(tasks []Task, opts Options) *Scheduler {
	max := opts.MaxConcurrency
	if max <= 0 {
		max = DefaultMaxConcurrency
	}
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	rnd := opts.Rand
	if rnd == nil {
		// Each scheduler gets its own source so tests stay independent.
		src := rand.New(rand.NewSource(time.Now().UnixNano()))
		var mu sync.Mutex
		rnd = func() float64 {
			mu.Lock()
			defer mu.Unlock()
			return src.Float64()
		}
	}
	label := opts.NodeLabel
	if label == "" {
		label = opts.Store.NodeID()
	}
	return &Scheduler{
		tasks:     tasks,
		store:     opts.Store,
		sem:       make(chan struct{}, max),
		log:       log,
		now:       now,
		rand:      rnd,
		nodeLabel: label,
		taskMu:    make([]sync.Mutex, len(tasks)),
	}
}

// ValidateAll runs every connector's offline configuration check.
// The daemon calls this before listening, so it never starts with bad config
// (MOD-001 13).
func (s *Scheduler) ValidateAll() error {
	for _, t := range s.tasks {
		if err := t.Connector.ValidateConfig(); err != nil {
			return err
		}
	}
	return nil
}

// Run drives every task until ctx is cancelled. Each task runs in its own
// goroutine with per-provider concurrency of one; the shared semaphore bounds
// global concurrency.
func (s *Scheduler) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for i := range s.tasks {
		index := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.runTask(ctx, index)
		}()
	}
	wg.Wait()
}

func (s *Scheduler) runTask(ctx context.Context, index int) {
	t := s.tasks[index]
	if !t.Enabled {
		s.publishHealth(t, protocol.ErrNone, "", 0, false, nil, nil)
		return
	}

	// Stagger first runs so connectors do not all fire at once.
	if !sleepCtx(ctx, t.Policy.StartupJitter(s.rand())) {
		return
	}

	failures := 0
	var lastSuccessAt *time.Time
	for {
		result := s.collectTask(ctx, index)
		if ctx.Err() != nil {
			return
		}
		if result.class == protocol.ErrNone {
			failures = 0
		} else {
			failures++
		}

		delay := t.Policy.NextDelay(result.class, failures, result.retryAfter, s.rand())
		now := s.now().UTC()
		if result.class == protocol.ErrNone {
			lastSuccessAt = &now
		}
		nextAttemptAt := now.Add(delay)
		s.publishHealth(t, result.class, result.message, failures, true,
			lastSuccessAt, &nextAttemptAt)
		if !sleepCtx(ctx, delay) {
			return
		}
	}
}

// Refresh performs one immediate bounded collection for the selected enabled
// connectors. An empty list selects every enabled connector. Per-task locks
// preserve the scheduler's one-collection-at-a-time invariant.
func (s *Scheduler) Refresh(ctx context.Context, connectorIDs []string) error {
	all := len(connectorIDs) == 0
	selected := make(map[string]bool, len(connectorIDs))
	for _, id := range connectorIDs {
		selected[id] = true
	}
	indices := make([]int, 0, len(s.tasks))
	previousHealth := make(map[string]protocol.ConnectorHealth)
	for _, health := range s.store.Snapshot().ConnectorHealth {
		previousHealth[health.ConnectorID] = health
	}
	for i, task := range s.tasks {
		id := task.Connector.ID()
		if all {
			if task.Enabled {
				indices = append(indices, i)
			}
			continue
		}
		if selected[id] {
			if !task.Enabled {
				return fmt.Errorf("connector %q is disabled", id)
			}
			indices = append(indices, i)
			delete(selected, id)
		}
	}
	if len(selected) > 0 {
		for id := range selected {
			return fmt.Errorf("connector %q is not configured", id)
		}
	}
	if len(indices) == 0 {
		return errors.New("no enabled connectors to refresh")
	}

	var wg sync.WaitGroup
	for _, index := range indices {
		index := index
		wg.Add(1)
		go func() {
			defer wg.Done()
			result := s.collectTask(ctx, index)
			failures := 0
			prior := previousHealth[s.tasks[index].Connector.ID()]
			lastSuccessAt, nextAttemptAt := prior.LastSuccessAt, prior.NextAttemptAt
			if result.class != protocol.ErrNone {
				failures = 1
			} else {
				now := s.now().UTC()
				lastSuccessAt = &now
			}
			s.publishHealth(s.tasks[index], result.class, result.message, failures, true,
				lastSuccessAt, nextAttemptAt)
		}()
	}
	wg.Wait()
	return ctx.Err()
}

// ConnectorIDs returns the configured connector IDs for command validation.
func (s *Scheduler) ConnectorIDs() []string {
	ids := make([]string, 0, len(s.tasks))
	for _, task := range s.tasks {
		if task.Enabled {
			ids = append(ids, task.Connector.ID())
		}
	}
	return ids
}

func (s *Scheduler) collectTask(ctx context.Context, index int) collectionResult {
	s.taskMu[index].Lock()
	defer s.taskMu[index].Unlock()
	return s.collectOnce(ctx, s.tasks[index])
}

type collectionResult struct {
	class      protocol.ErrorClass
	retryAfter int
	message    string
}

// collectOnce performs one bounded collection and records the outcome.
func (s *Scheduler) collectOnce(ctx context.Context, t Task) collectionResult {
	select {
	case s.sem <- struct{}{}:
	case <-ctx.Done():
		return collectionResult{class: protocol.ErrTimeout}
	}
	defer func() { <-s.sem }()

	timeout := t.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	metrics, err := t.Connector.Collect(callCtx)
	class := connector.Classify(err)
	if err != nil {
		msg := connector.Message(err)
		if class == protocol.ErrAuth {
			// ADR-013: the only remedy the daemon offers is the official CLI.
			msg = connector.AuthActionMessage(s.nodeLabel)
		}
		s.log.Warn("collection failed",
			"connector", t.Connector.ID(),
			"provider", t.Connector.Provider(),
			"error_class", string(class),
			"message", msg)
		if _, aerr := s.store.ApplyConnectorError(t.Connector.ID(), class, msg); aerr != nil {
			s.log.Warn("could not annotate connector metrics",
				"connector", t.Connector.ID(), "error", aerr.Error())
		}
		return collectionResult{class: class, retryAfter: connector.RetryAfter(err), message: msg}
	}
	if t.StaleAfter > 0 {
		for i := range metrics {
			metrics[i].StaleAfter = protocol.Duration(t.StaleAfter)
		}
	}

	seq := s.nextSeq()
	if _, aerr := s.store.ApplyConnectorMetrics(t.Connector.ID(), seq, metrics); aerr != nil {
		s.log.Error("rejected connector output",
			"connector", t.Connector.ID(), "error", aerr.Error())
		msg := "connector produced an invalid metric"
		if _, err := s.store.ApplyConnectorError(t.Connector.ID(), protocol.ErrSchemaChanged, msg); err != nil {
			s.log.Warn("could not annotate connector metrics",
				"connector", t.Connector.ID(), "error", err.Error())
		}
		return collectionResult{class: protocol.ErrSchemaChanged, message: msg}
	}

	if hr, ok := t.Connector.(HealthReporter); ok {
		if healths, herr := hr.Health(); herr == nil {
			for _, h := range healths {
				if aerr := s.store.ApplyHealth(h); aerr != nil {
					s.log.Warn("rejected connector health",
						"connector", t.Connector.ID(), "error", aerr.Error())
				}
			}
		}
	}
	if reporter, ok := t.Connector.(connector.HomeLabReporter); ok {
		report, reportErr := reporter.HomeLab()
		if reportErr != nil {
			msg := "connector produced an invalid HomeLab summary"
			_, _ = s.store.ApplyConnectorError(t.Connector.ID(), protocol.ErrSchemaChanged, msg)
			return collectionResult{class: protocol.ErrSchemaChanged, message: msg}
		}
		if t.StaleAfter > 0 {
			for i := range report.Nodes {
				report.Nodes[i].StaleAfter = protocol.Duration(t.StaleAfter)
			}
			for i := range report.Services {
				report.Services[i].StaleAfter = protocol.Duration(t.StaleAfter)
			}
		}
		if applyErr := s.store.ApplyConnectorHomeLab(t.Connector.ID(), report); applyErr != nil {
			msg := "connector produced an invalid HomeLab summary"
			_, _ = s.store.ApplyConnectorError(t.Connector.ID(), protocol.ErrSchemaChanged, msg)
			return collectionResult{class: protocol.ErrSchemaChanged, message: msg}
		}
	}
	return collectionResult{class: protocol.ErrNone}
}

func (s *Scheduler) publishHealth(t Task, class protocol.ErrorClass, msg string,
	failures int, enabled bool, lastSuccessAt, nextAttemptAt *time.Time) {
	now := s.now().UTC()
	h := protocol.ConnectorHealth{
		ConnectorID:         t.Connector.ID(),
		Provider:            t.Connector.Provider(),
		Enabled:             enabled,
		State:               StateFor(class, enabled),
		LastAttemptAt:       &now,
		ConsecutiveFailures: failures,
		ErrorClass:          class,
		Message:             msg,
		LastSuccessAt:       lastSuccessAt,
		NextAttemptAt:       nextAttemptAt,
	}
	if err := s.store.ApplyHealth(h); err != nil {
		s.log.Warn("health rejected", "connector", t.Connector.ID(), "error", err.Error())
	}
}

func (s *Scheduler) nextSeq() uint64 {
	s.seqMu.Lock()
	defer s.seqMu.Unlock()
	s.seq++
	return s.seq
}

// sleepCtx waits for d or until ctx is done. It reports false when cancelled.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		select {
		case <-ctx.Done():
			return false
		default:
			return true
		}
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}
