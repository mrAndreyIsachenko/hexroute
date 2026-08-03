package actionplan

import (
	"errors"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

type Ownership string

const (
	OwnershipAvailable Ownership = "available"
	OwnershipOwned     Ownership = "owned"
	OwnershipForeign   Ownership = "foreign"
	OwnershipAmbiguous Ownership = "ambiguous"
)

func (ownership Ownership) Valid() bool {
	switch ownership {
	case OwnershipAvailable, OwnershipOwned, OwnershipForeign, OwnershipAmbiguous:
		return true
	default:
		return false
	}
}

type Observation struct {
	StepID      string
	StateSHA256 string
	Ownership   Ownership
	ActionID    metadata.UUID
	AttemptID   metadata.UUID
}

type AppliedStep struct {
	planSHA256 string
	position   int
	step       StepSpec
	actionID   metadata.UUID
	attemptID  metadata.UUID
}

type Execution struct {
	plan      Plan
	actionID  metadata.UUID
	attemptID metadata.UUID
	applied   []AppliedStep
}

type RollbackSkipReason string

const (
	RollbackSkipMissingObservation RollbackSkipReason = "missing_observation"
	RollbackSkipNotOwned           RollbackSkipReason = "not_owned"
	RollbackSkipForeignOwner       RollbackSkipReason = "foreign_owner"
	RollbackSkipAmbiguousOwner     RollbackSkipReason = "ambiguous_owner"
	RollbackSkipStateChanged       RollbackSkipReason = "state_changed"
)

type RollbackSkip struct {
	StepID string
	Reason RollbackSkipReason
}

type RollbackOperation struct {
	stepID              string
	position            int
	kind                InverseKind
	inputSHA256         string
	expectedStateSHA256 string
	restoredStateSHA256 string
	actionID            metadata.UUID
	attemptID           metadata.UUID
}

type RollbackPlan struct {
	operations []RollbackOperation
	skipped    []RollbackSkip
}

var (
	ErrInvalidExecution   = errors.New("invalid action execution")
	ErrUnexpectedStep     = errors.New("unexpected action step")
	ErrVerificationFailed = errors.New("action step verification failed")
	ErrForeignOwnership   = errors.New("foreign action state ownership")
	ErrAmbiguousOwnership = errors.New("ambiguous action state ownership")
)

func NewExecution(plan Plan, actionID, attemptID metadata.UUID) (Execution, error) {
	if !plan.valid() || !validUUID(actionID) || !validUUID(attemptID) ||
		actionID == attemptID {
		return Execution{}, ErrInvalidExecution
	}
	return Execution{plan: plan, actionID: actionID, attemptID: attemptID}, nil
}

func VerifyBefore(step StepSpec, observation Observation) error {
	if validateStep(step) != nil || validateObservation(observation) != nil ||
		observation.StepID != step.ID || observation.StateSHA256 != step.BeforeSHA256 {
		return ErrVerificationFailed
	}
	switch observation.Ownership {
	case OwnershipAvailable:
		return nil
	case OwnershipForeign:
		return ErrForeignOwnership
	case OwnershipAmbiguous:
		return ErrAmbiguousOwnership
	case OwnershipOwned:
		return ErrForeignOwnership
	default:
		return ErrVerificationFailed
	}
}

func (execution Execution) RecordApplied(
	position int,
	observation Observation,
) (Execution, error) {
	if !execution.valid() || position != len(execution.applied) {
		return Execution{}, ErrUnexpectedStep
	}
	step, exists := execution.plan.Step(position)
	if !exists {
		return Execution{}, ErrUnexpectedStep
	}
	if err := verifyOwnedState(
		step.ID,
		step.AppliedSHA256,
		execution.actionID,
		execution.attemptID,
		observation,
	); err != nil {
		return Execution{}, err
	}
	next := execution
	next.applied = append(append([]AppliedStep(nil), execution.applied...), AppliedStep{
		planSHA256: execution.plan.Digest(),
		position:   position,
		step:       step,
		actionID:   execution.actionID,
		attemptID:  execution.attemptID,
	})
	return next, nil
}

func (execution Execution) AppliedCount() int {
	return len(execution.applied)
}

func (execution Execution) BuildRollback(
	observations map[string]Observation,
) (RollbackPlan, error) {
	if !execution.valid() {
		return RollbackPlan{}, ErrInvalidExecution
	}
	result := RollbackPlan{}
	for position := len(execution.applied) - 1; position >= 0; position-- {
		applied := execution.applied[position]
		if !applied.matches(execution, position) {
			return RollbackPlan{}, ErrInvalidExecution
		}
		observation, exists := observations[applied.step.ID]
		if !exists {
			result.skipped = append(result.skipped, RollbackSkip{
				StepID: applied.step.ID,
				Reason: RollbackSkipMissingObservation,
			})
			continue
		}
		if validateObservation(observation) != nil || observation.StepID != applied.step.ID {
			return RollbackPlan{}, ErrVerificationFailed
		}
		reason, eligible := rollbackEligibility(applied, observation)
		if !eligible {
			result.skipped = append(result.skipped, RollbackSkip{
				StepID: applied.step.ID,
				Reason: reason,
			})
			continue
		}
		result.operations = append(result.operations, RollbackOperation{
			stepID:              applied.step.ID,
			position:            applied.position,
			kind:                applied.step.Inverse.Kind,
			inputSHA256:         applied.step.Inverse.InputSHA256,
			expectedStateSHA256: applied.step.AppliedSHA256,
			restoredStateSHA256: applied.step.BeforeSHA256,
			actionID:            applied.actionID,
			attemptID:           applied.attemptID,
		})
	}
	return result, nil
}

func (plan RollbackPlan) Operations() []RollbackOperation {
	return append([]RollbackOperation(nil), plan.operations...)
}

func (plan RollbackPlan) Skipped() []RollbackSkip {
	return append([]RollbackSkip(nil), plan.skipped...)
}

func (operation RollbackOperation) StepID() string {
	return operation.stepID
}

func (operation RollbackOperation) Position() int {
	return operation.position
}

func (operation RollbackOperation) Kind() InverseKind {
	return operation.kind
}

func (operation RollbackOperation) InputSHA256() string {
	return operation.inputSHA256
}

func (operation RollbackOperation) ExpectedStateSHA256() string {
	return operation.expectedStateSHA256
}

func (operation RollbackOperation) RestoredStateSHA256() string {
	return operation.restoredStateSHA256
}

func (operation RollbackOperation) VerifyReady(observation Observation) error {
	return verifyOwnedState(
		operation.stepID,
		operation.expectedStateSHA256,
		operation.actionID,
		operation.attemptID,
		observation,
	)
}

func (operation RollbackOperation) VerifyRestored(observation Observation) error {
	if validateObservation(observation) != nil || observation.StepID != operation.stepID ||
		observation.StateSHA256 != operation.restoredStateSHA256 ||
		observation.Ownership != OwnershipAvailable {
		return ErrVerificationFailed
	}
	return nil
}

func (execution Execution) valid() bool {
	if !execution.plan.valid() || !validUUID(execution.actionID) ||
		!validUUID(execution.attemptID) || execution.actionID == execution.attemptID ||
		len(execution.applied) > execution.plan.Len() {
		return false
	}
	for position, applied := range execution.applied {
		if !applied.matches(execution, position) {
			return false
		}
	}
	return true
}

func (applied AppliedStep) matches(execution Execution, position int) bool {
	step, exists := execution.plan.Step(position)
	return exists && applied.planSHA256 == execution.plan.Digest() &&
		applied.position == position && applied.step == step &&
		applied.actionID == execution.actionID && applied.attemptID == execution.attemptID
}

func rollbackEligibility(
	applied AppliedStep,
	observation Observation,
) (RollbackSkipReason, bool) {
	switch observation.Ownership {
	case OwnershipAvailable:
		return RollbackSkipNotOwned, false
	case OwnershipForeign:
		return RollbackSkipForeignOwner, false
	case OwnershipAmbiguous:
		return RollbackSkipAmbiguousOwner, false
	case OwnershipOwned:
		if observation.ActionID != applied.actionID || observation.AttemptID != applied.attemptID {
			return RollbackSkipForeignOwner, false
		}
		if observation.StateSHA256 != applied.step.AppliedSHA256 {
			return RollbackSkipStateChanged, false
		}
		return "", true
	default:
		return RollbackSkipAmbiguousOwner, false
	}
}

func verifyOwnedState(
	stepID string,
	stateSHA256 string,
	actionID metadata.UUID,
	attemptID metadata.UUID,
	observation Observation,
) error {
	if validateObservation(observation) != nil || observation.StepID != stepID ||
		observation.StateSHA256 != stateSHA256 {
		return ErrVerificationFailed
	}
	switch observation.Ownership {
	case OwnershipOwned:
		if observation.ActionID != actionID || observation.AttemptID != attemptID {
			return ErrForeignOwnership
		}
		return nil
	case OwnershipForeign, OwnershipAvailable:
		return ErrForeignOwnership
	case OwnershipAmbiguous:
		return ErrAmbiguousOwnership
	default:
		return ErrVerificationFailed
	}
}

func validateObservation(observation Observation) error {
	if !stepIDPattern.MatchString(observation.StepID) ||
		!validDigest(observation.StateSHA256) || !observation.Ownership.Valid() {
		return ErrVerificationFailed
	}
	switch observation.Ownership {
	case OwnershipOwned:
		if !validUUID(observation.ActionID) || !validUUID(observation.AttemptID) ||
			observation.ActionID == observation.AttemptID {
			return ErrVerificationFailed
		}
	case OwnershipAvailable, OwnershipForeign, OwnershipAmbiguous:
		if observation.ActionID != "" || observation.AttemptID != "" {
			return ErrVerificationFailed
		}
	}
	return nil
}

func validUUID(value metadata.UUID) bool {
	_, err := metadata.ParseUUID(string(value))
	return err == nil
}
