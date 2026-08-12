package configtx

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/galendai/homepi-mon/internal/config"
	"github.com/galendai/homepi-mon/internal/secretstore"
)

// ApplyResult is the public summary of a successful Apply call. The
// Service never returns raw secret values; only references, booleans
// and counts.
type ApplyResult struct {
	Revision         string
	PersistedAt      time.Time
	AddedProviders   []string
	RemovedProviders []string
	ModifiedFields   map[string][]string
	OldSecretsPruned []string
	// Restarted is true when the caller asked for a service restart
	// and the Service performed one. It is false in dry-run mode.
	Restarted bool
	// Healthy is the result of the post-restart health check. When
	// false, the Apply was rolled back and the operator should
	// re-load the draft.
	Healthy bool
	// StepLog records the steps the Apply pipeline executed. The
	// Web Admin displays this for the operator.
	StepLog []StepRecord
}

// StepRecord is one entry of an Apply pipeline trace. Step names a
// pipeline stage; Status is either "ok", "skipped" or "rolled_back".
// Detail is a short, redacted message safe to surface in the UI.
type StepRecord struct {
	Step   string
	Status string
	Detail string
}

// ApplyOptions configure a single Apply call. Restart and HealthCheck
// are optional; when both are nil the Apply is a "dry save" that does
// not touch the running service.
type ApplyOptions struct {
	// Restart, when non-nil, is invoked after the new config is
	// persisted. It is responsible for telling the user-level service
	// manager to reload the daemon. The Service does not run any
	// platform-specific restart code itself.
	Restart func(ctx context.Context) error
	// HealthCheck, when non-nil, is called after Restart returns. It
	// must return nil for the Apply to be considered healthy. A
	// non-nil result triggers a rollback.
	HealthCheck func(ctx context.Context) error
	// Now is injectable for tests; nil falls back to Service.opts.Now.
	Now func() time.Time
	// KeepSecretRefs prevents selected obsolete references from being
	// pruned. The CLI uses it to honour provider remove -keep-secret.
	KeepSecretRefs []string
	// RequireProviderTests enforces successful read-only tests for new
	// and credential-changing enabled providers. Web Admin always sets it;
	// non-interactive CLI commands retain their historical explicit flow.
	RequireProviderTests bool
}

// Apply commits the draft to disk and the keyring, in the order
// prescribed by Module-Spec-005 §7.2:
//
//  1. Validate the complete draft, revision and required tests.
//  2. Persist every new/rotated secret under a versioned reference.
//  3. Persist the staged config atomically.
//  4. Optionally restart the user service.
//  5. Optionally run a health check; on failure, restore the previous
//     config and the previous secret set.
//  6. Prune obsolete keyring entries (only after step 5 succeeds).
//
// The function never returns the draft's candidate secret values; the
// returned ApplyResult only carries ref names and counts.
func (s *Service) Apply(ctx context.Context, draft *Draft, opts ApplyOptions) (ApplyResult, error) {
	if draft == nil {
		return ApplyResult{}, errors.New("configtx: nil draft")
	}
	if draft.svc != s {
		return ApplyResult{}, errors.New("configtx: draft belongs to a different Service")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	draft.mu.Lock()
	defer draft.mu.Unlock()

	diskRevision, err := revisionForDisk(s.opts.ConfigPath)
	if err != nil {
		return ApplyResult{}, &ApplyError{Step: StepValidating, Reason: err}
	}
	if draft.revision != diskRevision {
		return ApplyResult{}, ErrRevisionConflict
	}

	now := s.opts.Now
	if opts.Now != nil {
		now = opts.Now
	}

	result := ApplyResult{
		Revision:       draft.revision,
		PersistedAt:    now(),
		ModifiedFields: map[string][]string{},
	}
	if err := draft.pending.ValidateWithRegistry(knownTypeSet()); err != nil {
		return result, &ApplyError{Step: StepValidating,
			Reason: fmt.Errorf("%w: %v", ErrInvalidDraft, err)}
	}
	if err := draft.checkShadows(); err != nil {
		return result, &ApplyError{Step: StepValidating, Reason: err}
	}
	if opts.RequireProviderTests {
		if err := draft.validateRequiredTests(); err != nil {
			return result, &ApplyError{Step: StepValidating, Reason: err}
		}
	}
	result.StepLog = append(result.StepLog, StepRecord{
		Step: string(StepValidating), Status: "ok", Detail: "draft and revision validated",
	})
	// Build a snapshot of the existing on-disk state so the rollback
	// path can restore it without consulting the caller.
	prevPath := s.opts.ConfigPath
	prevBytes, prevHadPrev, err := readPreviousIfAny(prevPath)
	if err != nil {
		return result, &ApplyError{Step: StepValidating, Reason: err}
	}

	// Step 2: persist the candidate secrets. The overlay carries the
	// refs and values entered by the operator; on success each ref
	// is committed to the keyring.
	store, err := s.openStore(ctx)
	if err != nil {
		return result, &ApplyError{Step: StepPersisting, Reason: err}
	}
	overlaySnapshot := draft.overlay.snapshotValues()
	committed := make([]string, 0, len(overlaySnapshot))
	for ref, value := range overlaySnapshot {
		if _, getErr := store.Get(ctx, ref); getErr == nil {
			return s.rollback(ctx, result, rollbackState{
				prevBytes: prevBytes, prevHadPrev: prevHadPrev,
				store: store, committedRefs: committed,
			}, &ApplyError{Step: StepPersisting,
				Reason: errors.New("candidate secret version already exists")})
		} else if !errors.Is(getErr, secretstore.ErrNotFound) {
			return s.rollback(ctx, result, rollbackState{
				prevBytes: prevBytes, prevHadPrev: prevHadPrev,
				store: store, committedRefs: committed,
			}, &ApplyError{Step: StepPersisting,
				Reason: errors.New("candidate secret version could not be checked")})
		}
		if err := store.Set(ctx, ref, value); err != nil {
			return s.rollback(ctx, result, rollbackState{
				prevBytes: prevBytes, prevHadPrev: prevHadPrev,
				store: store, committedRefs: append(committed, ref),
			}, &ApplyError{Step: StepPersisting, Reason: errors.New("store candidate secret")})
		}
		committed = append(committed, ref)
	}
	if len(overlaySnapshot) > 0 {
		result.StepLog = append(result.StepLog, StepRecord{
			Step: string(StepPersisting), Status: "ok",
			Detail: fmt.Sprintf("committed %d secret(s)", len(overlaySnapshot)),
		})
	}

	// Step 3: only after every versioned secret is durable may the
	// non-secret config start referencing it.
	if err := draft.pending.Save(prevPath); err != nil {
		return s.rollback(ctx, result, rollbackState{
			prevBytes: prevBytes, prevHadPrev: prevHadPrev,
			store: store, committedRefs: committed,
		}, &ApplyError{Step: StepPersisting, Reason: err})
	}
	result.StepLog = append(result.StepLog, StepRecord{
		Step: string(StepPersisting), Status: "ok",
		Detail: fmt.Sprintf("wrote %s", prevPath),
	})

	// Compute removed / added / modified for the audit summary.
	added, removed, modified := summariseDiff(draft)
	result.AddedProviders = added
	result.RemovedProviders = removed
	for k, v := range modified {
		result.ModifiedFields[k] = v
	}

	// Step 4 + 5: restart and health check. Both are optional; the
	// "dry save" path skips them.
	if opts.Restart != nil {
		restartAttempted := true
		if err := opts.Restart(ctx); err != nil {
			return s.rollback(ctx, result, rollbackState{
				prevBytes: prevBytes, prevHadPrev: prevHadPrev,
				store: store, committedRefs: committed, configPersisted: true,
				restart: opts.Restart, restartAttempted: restartAttempted,
			}, &ApplyError{Step: StepRestarting, Reason: err})
		}
		result.Restarted = true
		result.StepLog = append(result.StepLog, StepRecord{
			Step: string(StepRestarting), Status: "ok", Detail: "service restart signalled",
		})
	}
	if opts.HealthCheck != nil {
		hcCtx, cancel := context.WithTimeout(ctx, s.opts.ApplyTimeout)
		defer cancel()
		if err := opts.HealthCheck(hcCtx); err != nil {
			return s.rollback(ctx, result, rollbackState{
				prevBytes: prevBytes, prevHadPrev: prevHadPrev,
				store: store, committedRefs: committed, configPersisted: true,
				restart: opts.Restart, restartAttempted: opts.Restart != nil,
			}, &ApplyError{Step: StepVerifying, Reason: err})
		}
		result.Healthy = true
		result.StepLog = append(result.StepLog, StepRecord{
			Step: string(StepVerifying), Status: "ok", Detail: "health check passed",
		})
	}

	// Step 6: prune orphan secret refs. The Service only deletes
	// refs that were referenced by the previous config and are no
	// longer referenced by the new config.
	keepRefs := make(map[string]bool, len(opts.KeepSecretRefs))
	for _, ref := range opts.KeepSecretRefs {
		keepRefs[ref] = true
	}
	pruned, pruneErr := s.pruneOrphanSecrets(ctx, store, draft, keepRefs)
	if pruneErr != nil {
		// Pruning failure is non-fatal: the new config is already
		// in place and the orphan secrets are harmless. The audit
		// summary records the failure for the operator to clean up.
		result.StepLog = append(result.StepLog, StepRecord{
			Step: string(StepFinalising), Status: "warning",
			Detail: fmt.Sprintf("orphan prune failed: %v", pruneErr),
		})
	}
	result.OldSecretsPruned = pruned
	if pruneErr == nil && len(pruned) > 0 {
		result.StepLog = append(result.StepLog, StepRecord{
			Step: string(StepFinalising), Status: "ok",
			Detail: fmt.Sprintf("pruned %d orphan secret(s)", len(pruned)),
		})
	}

	// Bump the base revision only after the on-disk config and
	// keyring are consistent. Future drafts will see the new
	// revision and refuse to Apply if the operator reloaded.
	s.baseRevision, err = revisionForDisk(s.opts.ConfigPath)
	if err != nil {
		return result, &ApplyError{Step: StepFinalising, Reason: err}
	}
	result.Revision = s.baseRevision
	s.lastApply = applyRecord{
		at:      now(),
		status:  "ok",
		summary: fmt.Sprintf("+%d -%d ~%d providers", len(added), len(removed), len(modified)),
	}
	result.StepLog = append(result.StepLog, StepRecord{
		Step: string(StepCompleted), Status: "ok", Detail: s.lastApply.summary,
	})
	return result, nil
}

// rollback is shared by every failing Apply step. It restores the
// previous config from the in-memory snapshot and clears any
// candidate secrets the operator entered; it then attempts to restart
// the service so the previous config is live again. A non-nil error
// from restart is reported as ErrRollbackFailed.
type rollbackState struct {
	prevBytes        []byte
	prevHadPrev      bool
	store            secretstore.Store
	committedRefs    []string
	configPersisted  bool
	restart          func(context.Context) error
	restartAttempted bool
}

func (s *Service) rollback(ctx context.Context, result ApplyResult,
	state rollbackState, fail *ApplyError) (ApplyResult, error) {
	var rollbackErrors []error
	if state.configPersisted && state.prevHadPrev {
		if err := atomicWrite(s.opts.ConfigPath, state.prevBytes, 0o600); err != nil {
			result.StepLog = append(result.StepLog, StepRecord{
				Step: string(StepRollingBack), Status: "failed",
				Detail: "could not restore previous config: " + err.Error(),
			})
			rollbackErrors = append(rollbackErrors, err)
		} else {
			result.StepLog = append(result.StepLog, StepRecord{
				Step: string(StepRollingBack), Status: "ok",
				Detail: "restored previous config atomically",
			})
		}
	} else if state.configPersisted {
		// No previous file existed: remove the half-written one.
		if err := os.Remove(s.opts.ConfigPath); err != nil && !os.IsNotExist(err) {
			result.StepLog = append(result.StepLog, StepRecord{
				Step: string(StepRollingBack), Status: "failed",
				Detail: "could not remove partial config: " + err.Error(),
			})
			rollbackErrors = append(rollbackErrors, err)
		} else {
			_ = syncDir(filepath.Dir(s.opts.ConfigPath))
			result.StepLog = append(result.StepLog, StepRecord{
				Step: string(StepRollingBack), Status: "ok",
				Detail: "removed partial config",
			})
		}
	}

	if state.restartAttempted && state.restart != nil && len(rollbackErrors) == 0 {
		if err := state.restart(ctx); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("restart previous config: %w", err))
			result.StepLog = append(result.StepLog, StepRecord{
				Step: string(StepRollingBack), Status: "failed",
				Detail: "previous service restart failed",
			})
		} else {
			result.StepLog = append(result.StepLog, StepRecord{
				Step: string(StepRollingBack), Status: "ok",
				Detail: "previous config restarted",
			})
		}
	}

	if state.store != nil {
		for _, ref := range state.committedRefs {
			if err := state.store.Delete(ctx, ref); err != nil &&
				!errors.Is(err, secretstore.ErrNotFound) {
				rollbackErrors = append(rollbackErrors, errors.New("delete candidate secret"))
			}
		}
		if len(state.committedRefs) > 0 {
			result.StepLog = append(result.StepLog, StepRecord{
				Step: string(StepRollingBack), Status: "ok",
				Detail: fmt.Sprintf("deleted %d candidate secret(s)", len(state.committedRefs)),
			})
		}
	}

	result.Healthy = false
	result.StepLog = append(result.StepLog, StepRecord{
		Step: string(fail.Step), Status: "rolled_back", Detail: fail.Error(),
	})
	s.lastApply = applyRecord{
		at:      s.opts.Now(),
		status:  "rolled_back",
		summary: fail.Error(),
	}
	if len(rollbackErrors) > 0 {
		return result, fmt.Errorf("%w: %w", ErrRollbackFailed, errors.Join(rollbackErrors...))
	}
	return result, fail
}

func atomicWrite(path string, body []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".rollback-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return err
	}
	return syncDir(filepath.Dir(path))
}

func syncDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

// readPreviousIfAny returns the previous config bytes and a flag
// indicating whether the file existed. The Apply caller passes the
// flag to rollback so it knows whether to restore or to delete.
func readPreviousIfAny(path string) ([]byte, bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return b, true, nil
}

// summariseDiff returns (added, removed, modified). modified is keyed
// by Provider ID; values are the field names that changed. The map
// never contains secret values.
func summariseDiff(d *Draft) ([]string, []string, map[string][]string) {
	before := indexProviders(d.base.Providers)
	after := indexProviders(d.pending.Providers)
	added := make([]string, 0)
	removed := make([]string, 0)
	modified := make(map[string][]string)
	seen := make(map[string]struct{}, len(after))
	for id, post := range after {
		seen[id] = struct{}{}
		pre, ok := before[id]
		if !ok {
			added = append(added, id)
			continue
		}
		if changes := diffProvider(pre, post); len(changes) > 0 {
			modified[id] = changes
		}
	}
	for id := range before {
		if _, ok := seen[id]; ok {
			continue
		}
		removed = append(removed, id)
	}
	return added, removed, modified
}

// pruneOrphanSecrets removes any keyring entry referenced by the
// previous config but no longer referenced by the pending config. It
// never touches refs the new config still uses, nor refs the operator
// staged for rotation (those are kept until the next Apply).
func (s *Service) pruneOrphanSecrets(ctx context.Context, store secretstore.Store,
	draft *Draft, keepRefs map[string]bool) ([]string, error) {

	prevRefs := collectRefs(draft.base)
	nextRefs := collectRefs(draft.pending)
	// Add the new refs the operator just committed so the prune does
	// not race against a follow-up draft load.
	for ref := range draft.overlay.snapshot() {
		nextRefs[ref] = true
	}
	pruned := make([]string, 0)
	for ref := range prevRefs {
		if nextRefs[ref] || keepRefs[ref] {
			continue
		}
		if err := store.Delete(ctx, ref); err != nil {
			if errors.Is(err, secretstore.ErrNotFound) {
				continue
			}
			return pruned, fmt.Errorf("delete obsolete secret: %w", err)
		}
		pruned = append(pruned, ref)
	}
	return pruned, nil
}

// collectRefs returns the set of secret_ref values referenced by cfg.
// Device token refs and provider secret refs are both included.
func collectRefs(cfg *config.Config) map[string]bool {
	out := make(map[string]bool)
	if cfg == nil {
		return out
	}
	for _, p := range cfg.Providers {
		if p.SecretRef != "" {
			out[p.SecretRef] = true
		}
	}
	for _, d := range cfg.Devices {
		if d.TokenRef != "" {
			out[d.TokenRef] = true
		}
	}
	return out
}
