package control

import (
	"errors"
	"testing"
)

func testPolicy() Policy {
	return Policy{
		FailureThreshold:   3,
		ActionBudget:       2,
		BaseBackoff:        10,
		MaxBackoff:         40,
		VerificationWindow: 30,
		Cooldown:           100,
	}
}

func mustMachine(t *testing.T, policy Policy, state State) *Machine {
	t.Helper()

	machine, err := NewMachine(policy, NewSnapshot(state))
	if err != nil {
		t.Fatalf("NewMachine() error: %v", err)
	}
	return machine
}

func step(t *testing.T, machine *Machine, at Tick, event Event) Decision {
	t.Helper()

	decision, err := machine.Step(machine.Snapshot().Generation, at, event)
	if err != nil {
		t.Fatalf("Step(%q) error: %v", event, err)
	}
	return decision
}

func TestLifecycleTransitions(t *testing.T) {
	tests := []struct {
		name  string
		state State
		event Event
		want  State
	}{
		{name: "suspended to healthy", state: StateSuspended, event: EventDependenciesReady, want: StateHealthy},
		{name: "healthy remains healthy below threshold", state: StateHealthy, event: EventProbeFailed, want: StateHealthy},
		{name: "degraded recovers on success", state: StateDegraded, event: EventProbeSucceeded, want: StateHealthy},
		{name: "recovering remains until verification window", state: StateRecovering, event: EventProbeSucceeded, want: StateRecovering},
		{name: "safe mode remains without cooldown event", state: StateSafeMode, event: EventProbeSucceeded, want: StateSafeMode},
		{name: "healthy suspends", state: StateHealthy, event: EventSuspend, want: StateSuspended},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			machine := mustMachine(t, testPolicy(), test.state)
			if test.state == StateRecovering {
				machine.snapshot.RecoveringSince = 1
			}
			decision := step(t, machine, 10, test.event)
			if decision.To != test.want {
				t.Fatalf("state = %s, want %s", decision.To, test.want)
			}
		})
	}
}

func TestConsecutiveFailureThreshold(t *testing.T) {
	machine := mustMachine(t, testPolicy(), StateHealthy)

	for at := Tick(1); at <= 2; at++ {
		decision := step(t, machine, at, EventProbeFailed)
		if decision.To != StateHealthy || decision.RecoveryApproved {
			t.Fatalf("isolated failure %d produced %+v", at, decision)
		}
	}

	decision := step(t, machine, 3, EventProbeFailed)
	if decision.To != StateDegraded || decision.RecoveryApproved {
		t.Fatalf("threshold decision = %+v", decision)
	}
}

func TestRecoveryVerificationAndBudget(t *testing.T) {
	policy := testPolicy()
	policy.FailureThreshold = 1
	machine := mustMachine(t, policy, StateHealthy)

	step(t, machine, 1, EventProbeFailed)
	first := step(t, machine, 2, EventBeginRecovery)
	if !first.RecoveryApproved || first.To != StateRecovering {
		t.Fatalf("first recovery decision = %+v", first)
	}
	if machine.Snapshot().NextActionAt != 12 {
		t.Fatalf("first backoff deadline = %d, want 12", machine.Snapshot().NextActionAt)
	}

	early := step(t, machine, 5, EventProbeSucceeded)
	if early.To != StateRecovering {
		t.Fatalf("early verification state = %s, want %s", early.To, StateRecovering)
	}

	step(t, machine, 6, EventProbeFailed)
	blocked := step(t, machine, 7, EventBeginRecovery)
	if blocked.RecoveryApproved {
		t.Fatalf("recovery approved before backoff: %+v", blocked)
	}

	second := step(t, machine, 12, EventBeginRecovery)
	if !second.RecoveryApproved {
		t.Fatalf("second recovery not approved: %+v", second)
	}
	if machine.Snapshot().NextActionAt != 32 {
		t.Fatalf("second backoff deadline = %d, want 32", machine.Snapshot().NextActionAt)
	}

	safe := step(t, machine, 13, EventProbeFailed)
	if safe.To != StateSafeMode || safe.RecoveryApproved {
		t.Fatalf("budget exhaustion decision = %+v", safe)
	}

	stillSafe := step(t, machine, 14, EventBeginRecovery)
	if stillSafe.To != StateSafeMode || stillSafe.RecoveryApproved {
		t.Fatalf("safe mode allowed recovery: %+v", stillSafe)
	}

	beforeCooldown := step(t, machine, 112, EventCooldownElapsed)
	if beforeCooldown.To != StateSafeMode {
		t.Fatalf("cooldown ended early: %+v", beforeCooldown)
	}

	afterCooldown := step(t, machine, 113, EventCooldownElapsed)
	if afterCooldown.To != StateDegraded || machine.Snapshot().Attempts != 0 {
		t.Fatalf("cooldown did not reset budget: %+v", afterCooldown)
	}
}

func TestVerificationWindowReturnsHealthy(t *testing.T) {
	policy := testPolicy()
	policy.FailureThreshold = 1
	machine := mustMachine(t, policy, StateHealthy)

	step(t, machine, 1, EventProbeFailed)
	step(t, machine, 2, EventBeginRecovery)
	decision := step(t, machine, 32, EventProbeSucceeded)

	if decision.To != StateHealthy {
		t.Fatalf("verified recovery state = %s, want %s", decision.To, StateHealthy)
	}
	if machine.Snapshot().Attempts != 0 {
		t.Fatalf("verified recovery attempts = %d, want 0", machine.Snapshot().Attempts)
	}
}

func TestBudgetsArePerMachineTarget(t *testing.T) {
	policy := testPolicy()
	policy.FailureThreshold = 1
	policy.ActionBudget = 1

	failedTarget := mustMachine(t, policy, StateHealthy)
	healthyTarget := mustMachine(t, policy, StateHealthy)

	step(t, failedTarget, 1, EventProbeFailed)
	step(t, failedTarget, 2, EventBeginRecovery)
	step(t, failedTarget, 3, EventProbeFailed)

	if failedTarget.Snapshot().State != StateSafeMode {
		t.Fatalf("failed target state = %s, want %s", failedTarget.Snapshot().State, StateSafeMode)
	}
	if healthyTarget.Snapshot().State != StateHealthy || healthyTarget.Snapshot().Attempts != 0 {
		t.Fatalf("independent target was mutated: %+v", healthyTarget.Snapshot())
	}
}

func TestSleepFailureDoesNotConsumeBudget(t *testing.T) {
	machine := mustMachine(t, testPolicy(), StateSuspended)

	step(t, machine, 10, EventProbeFailed)

	snapshot := machine.Snapshot()
	if snapshot.ConsecutiveFailures != 0 || snapshot.Attempts != 0 || snapshot.State != StateSuspended {
		t.Fatalf("suspended probe failure mutated budget: %+v", snapshot)
	}
}

func TestGenerationAndMonotonicGuards(t *testing.T) {
	machine := mustMachine(t, testPolicy(), StateHealthy)
	step(t, machine, 10, EventProbeSucceeded)

	if _, err := machine.Step(0, 11, EventProbeSucceeded); !errors.Is(err, ErrStaleGeneration) {
		t.Fatalf("stale generation error = %v, want %v", err, ErrStaleGeneration)
	}

	generation := machine.Snapshot().Generation
	if _, err := machine.Step(generation, 9, EventProbeSucceeded); !errors.Is(err, ErrNonMonotonicTick) {
		t.Fatalf("non-monotonic error = %v, want %v", err, ErrNonMonotonicTick)
	}
}

func TestOperatorResumeRequiresSafeModeAndExactGeneration(t *testing.T) {
	policy := testPolicy()
	snapshot := NewSnapshot(StateSafeMode)
	snapshot.Generation = 9
	snapshot.Attempts = policy.ActionBudget
	snapshot.LastTick = 100
	snapshot.RecoveringSince = 90
	snapshot.NextActionAt = 120
	snapshot.SafeUntil = 700
	machine, err := NewMachine(policy, snapshot)
	if err != nil {
		t.Fatalf("NewMachine() error: %v", err)
	}

	if _, err := machine.Step(8, 101, EventOperatorResume); !errors.Is(
		err,
		ErrStaleGeneration,
	) {
		t.Fatalf("stale resume error = %v, want %v", err, ErrStaleGeneration)
	}
	decision, err := machine.Step(9, 101, EventOperatorResume)
	if err != nil {
		t.Fatalf("operator resume error: %v", err)
	}
	after := machine.Snapshot()
	if decision.From != StateSafeMode ||
		decision.To != StateDegraded ||
		decision.Reason != ReasonOperatorResume ||
		after.Generation != 10 ||
		after.Attempts != 0 ||
		after.RecoveringSince != 0 ||
		after.NextActionAt != 101 ||
		after.SafeUntil != 0 {
		t.Fatalf("resume decision=%+v snapshot=%+v", decision, after)
	}
	if _, err := machine.Step(10, 102, EventOperatorResume); !errors.Is(
		err,
		ErrResumePrecondition,
	) {
		t.Fatalf("second resume error = %v, want %v", err, ErrResumePrecondition)
	}
}
