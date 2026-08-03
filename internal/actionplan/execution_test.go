package actionplan

import (
	"errors"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

const (
	testActionID  metadata.UUID = "123e4567-e89b-42d3-a456-426614174000"
	testAttemptID metadata.UUID = "223e4567-e89b-42d3-a456-426614174000"
	otherActionID metadata.UUID = "323e4567-e89b-42d3-a456-426614174000"
	otherAttempt  metadata.UUID = "423e4567-e89b-42d3-a456-426614174000"
)

func TestVerifyBeforeRejectsOwnedForeignAndAmbiguousState(t *testing.T) {
	step := testStep("resume-a")
	available := beforeObservation(step)
	if err := VerifyBefore(step, available); err != nil {
		t.Fatalf("available state: %v", err)
	}

	tests := []struct {
		name      string
		mutate    func(*Observation)
		wantError error
	}{
		{
			name: "wrong state", mutate: func(value *Observation) {
				value.StateSHA256 = testStep("resume-b").BeforeSHA256
			},
			wantError: ErrVerificationFailed,
		},
		{
			name: "already owned", mutate: func(value *Observation) {
				value.Ownership = OwnershipOwned
				value.ActionID = testActionID
				value.AttemptID = testAttemptID
			},
			wantError: ErrForeignOwnership,
		},
		{
			name: "foreign", mutate: func(value *Observation) {
				value.Ownership = OwnershipForeign
			},
			wantError: ErrForeignOwnership,
		},
		{
			name: "ambiguous", mutate: func(value *Observation) {
				value.Ownership = OwnershipAmbiguous
			},
			wantError: ErrAmbiguousOwnership,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			observation := available
			test.mutate(&observation)
			if err := VerifyBefore(step, observation); !errors.Is(err, test.wantError) {
				t.Fatalf("VerifyBefore() error = %v", err)
			}
		})
	}
}

func TestExecutionRecordsOnlyExactOrderedOwnedSteps(t *testing.T) {
	plan := mustPlan(t, 2)
	execution, err := NewExecution(plan, testActionID, testAttemptID)
	if err != nil {
		t.Fatal(err)
	}
	first, _ := plan.Step(0)
	next, err := execution.RecordApplied(0, appliedObservation(first, testActionID, testAttemptID))
	if err != nil {
		t.Fatal(err)
	}
	if execution.AppliedCount() != 0 || next.AppliedCount() != 1 {
		t.Fatalf("immutable ledger counts before=%d after=%d", execution.AppliedCount(), next.AppliedCount())
	}

	second, _ := plan.Step(1)
	if _, err := next.RecordApplied(0, appliedObservation(first, testActionID, testAttemptID)); !errors.Is(err, ErrUnexpectedStep) {
		t.Fatalf("duplicate step error = %v", err)
	}
	foreign := appliedObservation(second, otherActionID, otherAttempt)
	if _, err := next.RecordApplied(1, foreign); !errors.Is(err, ErrForeignOwnership) {
		t.Fatalf("foreign applied state error = %v", err)
	}
	wrongState := appliedObservation(second, testActionID, testAttemptID)
	wrongState.StateSHA256 = second.BeforeSHA256
	if _, err := next.RecordApplied(1, wrongState); !errors.Is(err, ErrVerificationFailed) {
		t.Fatalf("wrong applied state error = %v", err)
	}
}

func TestRollbackContainsOnlyExactOwnedStateInReverseOrder(t *testing.T) {
	plan := mustPlan(t, 4)
	execution, err := NewExecution(plan, testActionID, testAttemptID)
	if err != nil {
		t.Fatal(err)
	}
	for position := 0; position < plan.Len(); position++ {
		step, _ := plan.Step(position)
		execution, err = execution.RecordApplied(
			position,
			appliedObservation(step, testActionID, testAttemptID),
		)
		if err != nil {
			t.Fatalf("record step %d: %v", position, err)
		}
	}

	step0, _ := plan.Step(0)
	step1, _ := plan.Step(1)
	step2, _ := plan.Step(2)
	step3, _ := plan.Step(3)
	observations := map[string]Observation{
		step0.ID: appliedObservation(step0, testActionID, testAttemptID),
		step1.ID: {
			StepID: step1.ID, StateSHA256: step1.AppliedSHA256,
			Ownership: OwnershipForeign,
		},
		step2.ID: appliedObservation(step2, testActionID, testAttemptID),
		step3.ID: {
			StepID: step3.ID, StateSHA256: step3.AppliedSHA256,
			Ownership: OwnershipAmbiguous,
		},
	}
	rollback, err := execution.BuildRollback(observations)
	if err != nil {
		t.Fatal(err)
	}
	operations := rollback.Operations()
	if len(operations) != 2 || operations[0].StepID() != step2.ID ||
		operations[1].StepID() != step0.ID {
		t.Fatalf("rollback operations = %+v", operations)
	}
	for _, operation := range operations {
		if operation.Kind() != InverseRestoreControlSnapshot ||
			operation.InputSHA256() == "" || operation.ExpectedStateSHA256() == "" ||
			operation.RestoredStateSHA256() == "" {
			t.Fatalf("incomplete inverse operation: %+v", operation)
		}
		if err := operation.VerifyReady(observations[operation.StepID()]); err != nil {
			t.Fatalf("ready inverse %q: %v", operation.StepID(), err)
		}
		restored := Observation{
			StepID:      operation.StepID(),
			StateSHA256: operation.RestoredStateSHA256(),
			Ownership:   OwnershipAvailable,
		}
		if err := operation.VerifyRestored(restored); err != nil {
			t.Fatalf("restored inverse %q: %v", operation.StepID(), err)
		}
	}
	skipped := rollback.Skipped()
	if len(skipped) != 2 ||
		skipped[0] != (RollbackSkip{StepID: step3.ID, Reason: RollbackSkipAmbiguousOwner}) ||
		skipped[1] != (RollbackSkip{StepID: step1.ID, Reason: RollbackSkipForeignOwner}) {
		t.Fatalf("rollback skipped = %+v", skipped)
	}
}

func TestRollbackRechecksOwnershipAndCurrentState(t *testing.T) {
	plan := mustPlan(t, 1)
	step, _ := plan.Step(0)
	execution, err := NewExecution(plan, testActionID, testAttemptID)
	if err != nil {
		t.Fatal(err)
	}
	execution, err = execution.RecordApplied(
		0,
		appliedObservation(step, testActionID, testAttemptID),
	)
	if err != nil {
		t.Fatal(err)
	}
	changed := appliedObservation(step, testActionID, testAttemptID)
	changed.StateSHA256 = step.BeforeSHA256
	rollback, err := execution.BuildRollback(map[string]Observation{step.ID: changed})
	if err != nil {
		t.Fatal(err)
	}
	if len(rollback.Operations()) != 0 ||
		len(rollback.Skipped()) != 1 ||
		rollback.Skipped()[0].Reason != RollbackSkipStateChanged {
		t.Fatalf("changed-state rollback = operations=%+v skipped=%+v", rollback.Operations(), rollback.Skipped())
	}

	ready := appliedObservation(step, testActionID, testAttemptID)
	rollback, err = execution.BuildRollback(map[string]Observation{step.ID: ready})
	if err != nil {
		t.Fatal(err)
	}
	operation := rollback.Operations()[0]
	ready.ActionID = otherActionID
	ready.AttemptID = otherAttempt
	if err := operation.VerifyReady(ready); !errors.Is(err, ErrForeignOwnership) {
		t.Fatalf("late ownership change error = %v", err)
	}
}

func mustPlan(t *testing.T, count int) Plan {
	t.Helper()
	steps := make([]StepSpec, 0, count)
	for position := 0; position < count; position++ {
		steps = append(steps, testStep("resume-"+string(rune('a'+position))))
	}
	plan, err := New("synthetic-target", steps)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func beforeObservation(step StepSpec) Observation {
	return Observation{
		StepID:      step.ID,
		StateSHA256: step.BeforeSHA256,
		Ownership:   OwnershipAvailable,
	}
}

func appliedObservation(
	step StepSpec,
	actionID metadata.UUID,
	attemptID metadata.UUID,
) Observation {
	return Observation{
		StepID:      step.ID,
		StateSHA256: step.AppliedSHA256,
		Ownership:   OwnershipOwned,
		ActionID:    actionID,
		AttemptID:   attemptID,
	}
}
