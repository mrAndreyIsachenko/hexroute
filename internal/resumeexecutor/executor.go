package resumeexecutor

import (
	"context"
	"errors"
	"sync"

	"github.com/mrAndreyIsachenko/hexroute/internal/actionplan"
	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/resumeplan"
)

type Authorizer interface {
	AuthorizeOperatorResume(
		resumeplan.Plan,
		metadata.UUID,
	) (actionplan.LeaseGuard, actionplan.AuthorizationSource, error)
}

type State interface {
	Snapshot() control.Snapshot
	Resume(uint64, control.Tick) (control.Snapshot, error)
	CompensateOperatorResume(uint64, control.Snapshot) (control.Snapshot, error)
	EnterSafeMode(uint64, control.Tick) (control.Snapshot, error)
}

type IncidentSink interface {
	Emit(actionplan.CriticalIncident) error
}

type Executor struct {
	authorizer Authorizer
	state      State
	bootID     metadata.UUID
	incidents  IncidentSink
	mu         sync.Mutex
}

var (
	ErrInvalidExecutor = errors.New("invalid operator resume executor")
	ErrExecutionFailed = errors.New("operator resume execution failed")
)

func New(
	authorizer Authorizer,
	state State,
	bootID metadata.UUID,
	incidents IncidentSink,
) (*Executor, error) {
	if authorizer == nil || state == nil || incidents == nil {
		return nil, ErrInvalidExecutor
	}
	if _, err := metadata.ParseUUID(string(bootID)); err != nil {
		return nil, ErrInvalidExecutor
	}
	return &Executor{
		authorizer: authorizer, state: state, bootID: bootID, incidents: incidents,
	}, nil
}

func (executor *Executor) ExecuteOperatorResume(
	plan resumeplan.Plan,
) (control.Snapshot, error) {
	if executor == nil || plan.Digest() == "" {
		return control.Snapshot{}, ErrInvalidExecutor
	}
	executor.mu.Lock()
	defer executor.mu.Unlock()
	guard, authorization, err := executor.authorizer.AuthorizeOperatorResume(
		plan,
		executor.bootID,
	)
	if err != nil {
		return executor.state.Snapshot(), ErrExecutionFailed
	}
	runtime := &runtime{plan: plan, state: executor.state}
	failures := &failureHandler{
		plan: plan, state: executor.state, incidents: executor.incidents,
	}
	runner, err := actionplan.NewRunner(
		plan.ActionPlan(),
		guard,
		authorization,
		runtime,
		failures,
	)
	if err != nil {
		return executor.state.Snapshot(), ErrInvalidExecutor
	}
	result, err := runner.Run(context.Background())
	after := executor.state.Snapshot()
	if err != nil || !result.Committed || after != plan.Applied() {
		return after, ErrExecutionFailed
	}
	return after, nil
}

type ownership struct {
	actionID  metadata.UUID
	attemptID metadata.UUID
}

type runtime struct {
	plan  resumeplan.Plan
	state State
	owner *ownership
}

func (runtime *runtime) Observe(
	_ context.Context,
	stepID string,
) (actionplan.Observation, error) {
	step, ok := runtime.plan.ActionPlan().Step(0)
	if !ok || stepID != step.ID {
		return actionplan.Observation{}, ErrExecutionFailed
	}
	digest, err := snapshotDigest(runtime.state.Snapshot())
	if err != nil {
		return actionplan.Observation{}, err
	}
	observation := actionplan.Observation{
		StepID: stepID, StateSHA256: digest, Ownership: actionplan.OwnershipAmbiguous,
	}
	switch {
	case digest == step.BeforeSHA256 || digest == step.Inverse.RestoredSHA256:
		observation.Ownership = actionplan.OwnershipAvailable
	case digest == step.AppliedSHA256 && runtime.owner != nil:
		observation.Ownership = actionplan.OwnershipOwned
		observation.ActionID = runtime.owner.actionID
		observation.AttemptID = runtime.owner.attemptID
	}
	return observation, nil
}

func (runtime *runtime) Apply(
	_ context.Context,
	step actionplan.StepSpec,
	claim actionplan.OwnershipClaim,
) error {
	expected, ok := runtime.plan.ActionPlan().Step(0)
	if !ok || step != expected || runtime.owner != nil {
		return ErrExecutionFailed
	}
	after, err := runtime.state.Resume(
		runtime.plan.Before().Generation,
		runtime.plan.Applied().LastTick,
	)
	if err != nil || after != runtime.plan.Applied() {
		return ErrExecutionFailed
	}
	runtime.owner = &ownership{
		actionID: claim.ActionID(), attemptID: claim.AttemptID(),
	}
	return nil
}

func (runtime *runtime) ApplyInverse(
	_ context.Context,
	operation actionplan.RollbackOperation,
) error {
	step, ok := runtime.plan.ActionPlan().Step(0)
	if !ok || runtime.owner == nil || operation.StepID() != step.ID ||
		operation.Kind() != actionplan.InverseRestoreControlSnapshot ||
		operation.ExpectedStateSHA256() != step.AppliedSHA256 ||
		operation.RestoredStateSHA256() != step.Inverse.RestoredSHA256 {
		return ErrExecutionFailed
	}
	after, err := runtime.state.CompensateOperatorResume(
		runtime.plan.Applied().Generation,
		runtime.plan.Compensated(),
	)
	if err != nil || after != runtime.plan.Compensated() {
		return ErrExecutionFailed
	}
	runtime.owner = nil
	return nil
}

type failureHandler struct {
	plan      resumeplan.Plan
	state     State
	incidents IncidentSink
}

func (handler *failureHandler) EnterTargetSafeMode(
	_ context.Context,
	target string,
	_ actionplan.SafeModeReason,
) error {
	if target != handler.plan.ActionPlan().Target() {
		return ErrExecutionFailed
	}
	current := handler.state.Snapshot()
	at := current.LastTick
	if at < handler.plan.Compensated().LastTick {
		at = handler.plan.Compensated().LastTick
	}
	_, err := handler.state.EnterSafeMode(current.Generation, at)
	return err
}

func (handler *failureHandler) EmitCriticalIncident(
	_ context.Context,
	incident actionplan.CriticalIncident,
) error {
	return handler.incidents.Emit(incident)
}

func snapshotDigest(snapshot control.Snapshot) (string, error) {
	digest, _, err := policy.CanonicalSHA256(snapshot)
	if err != nil {
		return "", ErrExecutionFailed
	}
	return digest, nil
}
