package configtx

import (
	"errors"
	"fmt"
)

// ErrRevisionConflict indicates the on-disk config changed between
// draft load and Apply. The Web Admin surfaces this as a "please
// reload" message; the CLI exits non-zero.
var ErrRevisionConflict = errors.New("configtx: revision conflict")

// ErrInvalidDraft is returned when Edit/Apply finds a field-level
// problem in the draft. The wrapped error is safe to show to the user.
var ErrInvalidDraft = errors.New("configtx: invalid draft")

// ErrSecretOversize is returned when a candidate secret exceeds the
// cap declared in the connector's TypeMeta.
var ErrSecretOversize = errors.New("configtx: secret exceeds size limit")

// ErrShadowedProvider is returned when two enabled Providers would
// emit the same stable metric ID. The wrapped error names both
// Provider IDs.
var ErrShadowedProvider = errors.New("configtx: shadowed metric id")

// ErrSecretRequired is returned when a non-mock / non-file-backed
// Provider's draft has neither a stored secret_ref nor a candidate
// overlay value.
var ErrSecretRequired = errors.New("configtx: secret required")

// ErrApplyFailed is returned when one of the Apply steps failed
// irrecoverably. The wrapped error names the failing step.
var ErrApplyFailed = errors.New("configtx: apply failed")

// ErrRollbackFailed is returned when the rollback path could not
// restore the previous config and secrets. The wrapped error names the
// failing step. The Service marks the result manual_recovery_required.
var ErrRollbackFailed = errors.New("configtx: rollback failed")

// ErrBusy indicates a concurrent Apply is in progress. Callers should
// retry after a short delay; the Web Admin shows "another apply is
// running".
var ErrBusy = errors.New("configtx: apply in progress")

// ApplyStep names the sub-step of an Apply pipeline. The Service
// reports the failing step in its audit summary so the operator can
// tell why an Apply failed.
type ApplyStep string

const (
	StepValidating  ApplyStep = "validating"
	StepPersisting  ApplyStep = "persisting"
	StepRestarting  ApplyStep = "restarting"
	StepVerifying   ApplyStep = "verifying"
	StepRollingBack ApplyStep = "rolling_back"
	StepFinalising  ApplyStep = "finalising"
	StepCompleted   ApplyStep = "completed"
)

// ApplyError attaches a failing step to an underlying error.
type ApplyError struct {
	Step   ApplyStep
	Reason error
}

func (e *ApplyError) Error() string {
	return fmt.Sprintf("configtx: apply failed at %s: %v", e.Step, e.Reason)
}

func (e *ApplyError) Unwrap() error { return e.Reason }
