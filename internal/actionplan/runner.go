package actionplan

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/actionlease"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

type LeaseGuard interface {
	ActionID() metadata.UUID
	AttemptID() metadata.UUID
	BeginExecution() error
	BeforeStep(actionlease.CurrentAuthorization) error
	Commit(actionlease.CurrentAuthorization) error
	Abort(time.Time) error
	Outcome() (*policy.ActionLeaseOutcome, error)
}

type AuthorizationSource interface {
	Current(context.Context) (actionlease.CurrentAuthorization, error)
}

type OwnershipClaim struct {
	actionID  metadata.UUID
	attemptID metadata.UUID
}

func (claim OwnershipClaim) ActionID() metadata.UUID {
	return claim.actionID
}

func (claim OwnershipClaim) AttemptID() metadata.UUID {
	return claim.attemptID
}

type Runtime interface {
	Observe(context.Context, string) (Observation, error)
	Apply(context.Context, StepSpec, OwnershipClaim) error
	ApplyInverse(context.Context, RollbackOperation) error
}

type SafeModeReason string

const SafeModeRollbackFailed SafeModeReason = "action_rollback_failed"

type IncidentSeverity string

const IncidentSeverityCritical IncidentSeverity = "critical"

type IncidentCode string

const IncidentRollbackFailed IncidentCode = "action_rollback_failed"

type CriticalIncident struct {
	Severity   IncidentSeverity
	Code       IncidentCode
	OccurredAt time.Time
}

type FailureHandler interface {
	EnterTargetSafeMode(context.Context, string, SafeModeReason) error
	EmitCriticalIncident(context.Context, CriticalIncident) error
}

type RunResult struct {
	AppliedSteps     int
	RolledBackSteps  int
	SkippedSteps     int
	Committed        bool
	SafeMode         bool
	CriticalIncident bool
}

type Runner struct {
	plan          Plan
	guard         LeaseGuard
	authorization AuthorizationSource
	runtime       Runtime
	failures      FailureHandler
	mu            sync.Mutex
	started       bool
}

var (
	ErrInvalidRunner   = errors.New("invalid action plan runner")
	ErrExecutionFailed = errors.New("action plan execution failed")
	ErrRollbackFailed  = errors.New("action plan rollback failed")
)

const RollbackTimeout = 5 * time.Second

func NewRunner(
	plan Plan,
	guard LeaseGuard,
	authorization AuthorizationSource,
	runtime Runtime,
	failures FailureHandler,
) (*Runner, error) {
	if !plan.valid() || guard == nil || authorization == nil || runtime == nil || failures == nil ||
		!validUUID(guard.ActionID()) || !validUUID(guard.AttemptID()) ||
		guard.ActionID() == guard.AttemptID() {
		return nil, ErrInvalidRunner
	}
	return &Runner{
		plan:          plan,
		guard:         guard,
		authorization: authorization,
		runtime:       runtime,
		failures:      failures,
	}, nil
}

func (runner *Runner) Run(ctx context.Context) (RunResult, error) {
	if runner == nil {
		return RunResult{}, ErrInvalidRunner
	}
	runner.mu.Lock()
	if runner.started {
		runner.mu.Unlock()
		return RunResult{}, ErrExecutionFailed
	}
	runner.started = true
	runner.mu.Unlock()
	outcome, err := runner.guard.Outcome()
	if err != nil || outcome != nil {
		return RunResult{}, ErrExecutionFailed
	}
	if err := runner.guard.BeginExecution(); err != nil {
		return RunResult{}, ErrExecutionFailed
	}
	execution, err := NewExecution(
		runner.plan,
		runner.guard.ActionID(),
		runner.guard.AttemptID(),
	)
	if err != nil {
		return RunResult{}, ErrInvalidRunner
	}
	current, err := runner.current(ctx)
	if err != nil {
		return RunResult{}, ErrExecutionFailed
	}
	lastObservedAt := current.ObservedAt
	claim := OwnershipClaim{
		actionID:  runner.guard.ActionID(),
		attemptID: runner.guard.AttemptID(),
	}

	for position := 0; position < runner.plan.Len(); position++ {
		step, _ := runner.plan.Step(position)
		current, err = runner.current(ctx)
		if err != nil {
			return runner.fail(ctx, execution, lastObservedAt, false)
		}
		lastObservedAt = current.ObservedAt
		if err := runner.guard.BeforeStep(current); err != nil {
			return runner.fail(ctx, execution, lastObservedAt, false)
		}
		before, observeErr := runner.runtime.Observe(ctx, step.ID)
		if observeErr != nil || VerifyBefore(step, before) != nil {
			return runner.fail(ctx, execution, lastObservedAt, false)
		}

		applyErr := runner.runtime.Apply(ctx, step, claim)
		after, afterErr := runner.runtime.Observe(ctx, step.ID)
		uncertain := afterErr != nil
		if afterErr == nil {
			next, recordErr := execution.RecordApplied(position, after)
			switch {
			case recordErr == nil:
				execution = next
			case VerifyBefore(step, after) == nil:
				// The step did not mutate its state; earlier owned steps can still roll back.
			default:
				uncertain = true
			}
		}
		if applyErr != nil || uncertain || execution.AppliedCount() != position+1 {
			return runner.fail(ctx, execution, lastObservedAt, uncertain)
		}
	}

	current, err = runner.current(ctx)
	if err != nil {
		return runner.fail(ctx, execution, lastObservedAt, false)
	}
	lastObservedAt = current.ObservedAt
	if err := runner.guard.Commit(current); err != nil {
		outcome, outcomeErr := runner.guard.Outcome()
		if outcomeErr == nil && outcome != nil && outcome.Status == policy.LeaseCommitted {
			return RunResult{AppliedSteps: execution.AppliedCount(), Committed: true}, nil
		}
		return runner.fail(ctx, execution, lastObservedAt, false)
	}
	return RunResult{AppliedSteps: execution.AppliedCount(), Committed: true}, nil
}

func (runner *Runner) current(
	ctx context.Context,
) (actionlease.CurrentAuthorization, error) {
	current, err := runner.authorization.Current(ctx)
	if err != nil || current.Validate() != nil {
		return actionlease.CurrentAuthorization{}, ErrExecutionFailed
	}
	if current.Target != runner.plan.Target() || current.PlanSHA256 != runner.plan.Digest() {
		return actionlease.CurrentAuthorization{}, ErrExecutionFailed
	}
	return current, nil
}

func (runner *Runner) fail(
	ctx context.Context,
	execution Execution,
	observedAt time.Time,
	forceSafeMode bool,
) (RunResult, error) {
	result := RunResult{AppliedSteps: execution.AppliedCount()}
	safetyContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), RollbackTimeout)
	defer cancel()
	rollback, rollbackUnsafe := runner.rollback(safetyContext, execution)
	result.RolledBackSteps = rollback.RolledBackSteps
	result.SkippedSteps = rollback.SkippedSteps
	forceSafeMode = forceSafeMode || rollbackUnsafe

	if err := runner.resolveAbort(observedAt); err != nil {
		forceSafeMode = true
	}
	if forceSafeMode {
		result.SafeMode, result.CriticalIncident = runner.enterSafeMode(
			safetyContext,
			observedAt,
		)
		return result, ErrRollbackFailed
	}
	return result, ErrExecutionFailed
}

type rollbackResult struct {
	RolledBackSteps int
	SkippedSteps    int
}

func (runner *Runner) rollback(
	ctx context.Context,
	execution Execution,
) (rollbackResult, bool) {
	observations := make(map[string]Observation, execution.AppliedCount())
	unsafe := false
	for position := 0; position < execution.AppliedCount(); position++ {
		step, _ := runner.plan.Step(position)
		observation, err := runner.runtime.Observe(ctx, step.ID)
		if err != nil {
			unsafe = true
			continue
		}
		observations[step.ID] = observation
	}
	rollback, err := execution.BuildRollback(observations)
	if err != nil {
		return rollbackResult{}, true
	}
	result := rollbackResult{SkippedSteps: len(rollback.Skipped())}
	if result.SkippedSteps > 0 {
		unsafe = true
	}
	for _, operation := range rollback.Operations() {
		before, err := runner.runtime.Observe(ctx, operation.StepID())
		if err != nil || operation.VerifyReady(before) != nil {
			return result, true
		}
		if err := runner.runtime.ApplyInverse(ctx, operation); err != nil {
			return result, true
		}
		after, err := runner.runtime.Observe(ctx, operation.StepID())
		if err != nil || operation.VerifyRestored(after) != nil {
			return result, true
		}
		result.RolledBackSteps++
	}
	return result, unsafe
}

func (runner *Runner) resolveAbort(observedAt time.Time) error {
	if err := runner.guard.Abort(observedAt); err == nil {
		return nil
	}
	outcome, err := runner.guard.Outcome()
	if err != nil || outcome == nil || outcome.Status == policy.LeaseCommitted {
		return ErrRollbackFailed
	}
	return nil
}

func (runner *Runner) enterSafeMode(
	ctx context.Context,
	observedAt time.Time,
) (bool, bool) {
	safeMode := runner.failures.EnterTargetSafeMode(
		ctx,
		runner.plan.Target(),
		SafeModeRollbackFailed,
	) == nil
	incident := runner.failures.EmitCriticalIncident(ctx, CriticalIncident{
		Severity:   IncidentSeverityCritical,
		Code:       IncidentRollbackFailed,
		OccurredAt: observedAt.UTC(),
	}) == nil
	return safeMode, incident
}
