package actionplan

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/actionlease"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

type runnerGuard struct {
	actionID  metadata.UUID
	attemptID metadata.UUID
	expected  actionlease.CurrentAuthorization
	outcome   *policy.ActionLeaseOutcome
	commitErr error
	started   bool
}

func (guard *runnerGuard) ActionID() metadata.UUID {
	return guard.actionID
}

func (guard *runnerGuard) AttemptID() metadata.UUID {
	return guard.attemptID
}

func (guard *runnerGuard) BeginExecution() error {
	if guard.started {
		return actionlease.ErrLeaseReplay
	}
	guard.started = true
	return nil
}

func (guard *runnerGuard) BeforeStep(current actionlease.CurrentAuthorization) error {
	if !sameAuthorizationGeneration(guard.expected, current) {
		guard.outcome = runnerOutcome(policy.LeaseAborted, policy.LeaseOutcomeStaleGeneration)
		return actionlease.ErrLeaseStale
	}
	return nil
}

func (guard *runnerGuard) Commit(current actionlease.CurrentAuthorization) error {
	if !sameAuthorizationGeneration(guard.expected, current) {
		guard.outcome = runnerOutcome(policy.LeaseAborted, policy.LeaseOutcomeStaleGeneration)
		return actionlease.ErrLeaseStale
	}
	if guard.commitErr != nil {
		guard.outcome = runnerOutcome(policy.LeaseAborted, policy.LeaseOutcomeCanceled)
		return guard.commitErr
	}
	guard.outcome = runnerOutcome(policy.LeaseCommitted, policy.LeaseOutcomeCompleted)
	return nil
}

func (guard *runnerGuard) Abort(time.Time) error {
	if guard.outcome != nil {
		return actionlease.ErrLeaseReplay
	}
	guard.outcome = runnerOutcome(policy.LeaseAborted, policy.LeaseOutcomeCanceled)
	return nil
}

func (guard *runnerGuard) Outcome() (*policy.ActionLeaseOutcome, error) {
	if guard.outcome == nil {
		return nil, nil
	}
	outcome := *guard.outcome
	return &outcome, nil
}

type runnerAuthorization struct {
	values []actionlease.CurrentAuthorization
	errors map[int]error
	next   int
}

func (source *runnerAuthorization) Current(
	context.Context,
) (actionlease.CurrentAuthorization, error) {
	position := source.next
	source.next++
	if err := source.errors[position]; err != nil {
		return actionlease.CurrentAuthorization{}, err
	}
	if position >= len(source.values) {
		return actionlease.CurrentAuthorization{}, errors.New("no synthetic authorization")
	}
	return source.values[position], nil
}

type runnerRuntime struct {
	states          map[string]Observation
	steps           map[string]StepSpec
	applyFailures   map[string]error
	inverseFailures map[string]error
	observeHooks    map[string]func(int, *Observation)
	observeCounts   map[string]int
	applyCalls      []string
	inverseCalls    []string
}

func newRunnerRuntime(plan Plan) *runnerRuntime {
	runtime := &runnerRuntime{
		states:          make(map[string]Observation, plan.Len()),
		steps:           make(map[string]StepSpec, plan.Len()),
		applyFailures:   make(map[string]error),
		inverseFailures: make(map[string]error),
		observeHooks:    make(map[string]func(int, *Observation)),
		observeCounts:   make(map[string]int),
	}
	for _, step := range plan.Steps() {
		runtime.steps[step.ID] = step
		runtime.states[step.ID] = beforeObservation(step)
	}
	return runtime
}

func (runtime *runnerRuntime) Observe(
	_ context.Context,
	stepID string,
) (Observation, error) {
	observation, exists := runtime.states[stepID]
	if !exists {
		return Observation{}, errors.New("synthetic state not found")
	}
	runtime.observeCounts[stepID]++
	if hook := runtime.observeHooks[stepID]; hook != nil {
		hook(runtime.observeCounts[stepID], &observation)
		runtime.states[stepID] = observation
	}
	return observation, nil
}

func (runtime *runnerRuntime) Apply(
	_ context.Context,
	step StepSpec,
	claim OwnershipClaim,
) error {
	runtime.applyCalls = append(runtime.applyCalls, step.ID)
	if err := runtime.applyFailures[step.ID]; err != nil {
		return err
	}
	runtime.states[step.ID] = appliedObservation(step, claim.ActionID(), claim.AttemptID())
	return nil
}

func (runtime *runnerRuntime) ApplyInverse(
	_ context.Context,
	operation RollbackOperation,
) error {
	runtime.inverseCalls = append(runtime.inverseCalls, operation.StepID())
	if err := runtime.inverseFailures[operation.StepID()]; err != nil {
		return err
	}
	step := runtime.steps[operation.StepID()]
	runtime.states[operation.StepID()] = beforeObservation(step)
	return nil
}

type runnerFailures struct {
	states    map[string]string
	reasons   map[string]SafeModeReason
	incidents []CriticalIncident
}

func (failures *runnerFailures) EnterTargetSafeMode(
	_ context.Context,
	target string,
	reason SafeModeReason,
) error {
	failures.states[target] = "SAFE_MODE"
	failures.reasons[target] = reason
	return nil
}

func (failures *runnerFailures) EmitCriticalIncident(
	_ context.Context,
	incident CriticalIncident,
) error {
	failures.incidents = append(failures.incidents, incident)
	return nil
}

func TestRunnerStopsStalePlanAndRollsBackVerifiedOwnedStep(t *testing.T) {
	plan := mustPlan(t, 2)
	current := runnerCurrent(plan)
	stale := current
	stale.ControlStateGeneration++
	stale.ObservedAt = stale.ObservedAt.Add(2 * time.Second)
	stale.MonotonicNS += int64(2 * time.Second)
	guard := newRunnerGuard(current)
	runtime := newRunnerRuntime(plan)
	failures := newRunnerFailures(plan.Target())
	runner := mustRunner(t, plan, guard, &runnerAuthorization{
		values: []actionlease.CurrentAuthorization{
			current,
			advanceCurrent(current, time.Second),
			stale,
		},
	}, runtime, failures)

	result, err := runner.Run(context.Background())
	if !errors.Is(err, ErrExecutionFailed) {
		t.Fatalf("Run() error = %v", err)
	}
	if result.AppliedSteps != 1 || result.RolledBackSteps != 1 ||
		result.SkippedSteps != 0 || result.SafeMode || result.CriticalIncident {
		t.Fatalf("result = %+v", result)
	}
	if fmt.Sprint(runtime.applyCalls) != "[resume-a]" ||
		fmt.Sprint(runtime.inverseCalls) != "[resume-a]" {
		t.Fatalf("apply=%v inverse=%v", runtime.applyCalls, runtime.inverseCalls)
	}
	step, _ := plan.Step(0)
	if err := VerifyBefore(step, runtime.states[step.ID]); err != nil {
		t.Fatalf("rolled-back state: %v", err)
	}
	if failures.states[plan.Target()] != "HEALTHY" || len(failures.incidents) != 0 {
		t.Fatalf("unexpected failure state=%v incidents=%v", failures.states, failures.incidents)
	}
}

func TestRunnerRollbackFailureMovesOnlyTargetToSafeMode(t *testing.T) {
	plan := mustPlan(t, 2)
	current := runnerCurrent(plan)
	guard := newRunnerGuard(current)
	runtime := newRunnerRuntime(plan)
	runtime.applyFailures["resume-b"] = errors.New("synthetic apply failure")
	runtime.inverseFailures["resume-a"] = errors.New("synthetic inverse failure")
	failures := newRunnerFailures(plan.Target())
	runner := mustRunner(t, plan, guard, &runnerAuthorization{
		values: []actionlease.CurrentAuthorization{
			current,
			advanceCurrent(current, time.Second),
			advanceCurrent(current, 2*time.Second),
		},
	}, runtime, failures)

	result, err := runner.Run(context.Background())
	if !errors.Is(err, ErrRollbackFailed) {
		t.Fatalf("Run() error = %v", err)
	}
	if result.AppliedSteps != 1 || result.RolledBackSteps != 0 ||
		!result.SafeMode || !result.CriticalIncident {
		t.Fatalf("result = %+v", result)
	}
	if failures.states[plan.Target()] != "SAFE_MODE" ||
		failures.states["unrelated-target"] != "HEALTHY" ||
		failures.reasons[plan.Target()] != SafeModeRollbackFailed {
		t.Fatalf("target states=%v reasons=%v", failures.states, failures.reasons)
	}
	if len(failures.incidents) != 1 ||
		failures.incidents[0].Severity != IncidentSeverityCritical ||
		failures.incidents[0].Code != IncidentRollbackFailed {
		t.Fatalf("incidents = %+v", failures.incidents)
	}
}

func TestRunnerRechecksOwnershipBeforeInverseAndNeverTouchesForeignState(t *testing.T) {
	plan := mustPlan(t, 1)
	current := runnerCurrent(plan)
	guard := newRunnerGuard(current)
	guard.commitErr = errors.New("synthetic commit failure")
	runtime := newRunnerRuntime(plan)
	runtime.observeHooks["resume-a"] = func(count int, observation *Observation) {
		if count == 4 {
			*observation = Observation{
				StepID:      observation.StepID,
				StateSHA256: observation.StateSHA256,
				Ownership:   OwnershipForeign,
			}
		}
	}
	failures := newRunnerFailures(plan.Target())
	runner := mustRunner(t, plan, guard, &runnerAuthorization{
		values: []actionlease.CurrentAuthorization{
			current,
			advanceCurrent(current, time.Second),
			advanceCurrent(current, 2*time.Second),
		},
	}, runtime, failures)

	result, err := runner.Run(context.Background())
	if !errors.Is(err, ErrRollbackFailed) {
		t.Fatalf("Run() error = %v", err)
	}
	if len(runtime.inverseCalls) != 0 {
		t.Fatalf("foreign state received inverse calls: %v", runtime.inverseCalls)
	}
	if !result.SafeMode || !result.CriticalIncident ||
		failures.states[plan.Target()] != "SAFE_MODE" ||
		failures.states["unrelated-target"] != "HEALTHY" {
		t.Fatalf("result=%+v states=%v", result, failures.states)
	}
}

func TestRunnerCommitsVerifiedPlan(t *testing.T) {
	plan := mustPlan(t, 1)
	current := runnerCurrent(plan)
	guard := newRunnerGuard(current)
	runtime := newRunnerRuntime(plan)
	failures := newRunnerFailures(plan.Target())
	runner := mustRunner(t, plan, guard, &runnerAuthorization{
		values: []actionlease.CurrentAuthorization{
			current,
			advanceCurrent(current, time.Second),
			advanceCurrent(current, 2*time.Second),
		},
	}, runtime, failures)

	result, err := runner.Run(context.Background())
	if err != nil || !result.Committed || result.AppliedSteps != 1 ||
		result.RolledBackSteps != 0 || result.SafeMode {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	if guard.outcome == nil || guard.outcome.Status != policy.LeaseCommitted {
		t.Fatalf("outcome = %+v", guard.outcome)
	}
	if _, err := runner.Run(context.Background()); !errors.Is(err, ErrExecutionFailed) {
		t.Fatalf("second Run() error = %v", err)
	}
	if len(runtime.applyCalls) != 1 {
		t.Fatalf("runner replayed mutation: %v", runtime.applyCalls)
	}
}

func mustRunner(
	t *testing.T,
	plan Plan,
	guard LeaseGuard,
	authorization AuthorizationSource,
	runtime Runtime,
	failures FailureHandler,
) *Runner {
	t.Helper()
	runner, err := NewRunner(plan, guard, authorization, runtime, failures)
	if err != nil {
		t.Fatal(err)
	}
	return runner
}

func newRunnerGuard(current actionlease.CurrentAuthorization) *runnerGuard {
	return &runnerGuard{
		actionID:  testActionID,
		attemptID: testAttemptID,
		expected:  current,
	}
}

func newRunnerFailures(target string) *runnerFailures {
	return &runnerFailures{
		states: map[string]string{
			target:             "HEALTHY",
			"unrelated-target": "HEALTHY",
		},
		reasons: make(map[string]SafeModeReason),
	}
}

func runnerCurrent(plan Plan) actionlease.CurrentAuthorization {
	return actionlease.CurrentAuthorization{
		Domain:                 policy.DomainUser,
		Capability:             policy.CapabilityOperatorResume,
		BundleGeneration:       8,
		DomainPolicyGeneration: 5,
		ControlStateGeneration: 13,
		Target:                 plan.Target(),
		PlanSHA256:             plan.Digest(),
		BootID:                 "523e4567-e89b-42d3-a456-426614174000",
		MonotonicNS:            int64(10 * time.Second),
		ObservedAt:             time.Date(2030, time.January, 1, 0, 0, 10, 0, time.UTC),
	}
}

func advanceCurrent(
	current actionlease.CurrentAuthorization,
	delta time.Duration,
) actionlease.CurrentAuthorization {
	current.ObservedAt = current.ObservedAt.Add(delta)
	current.MonotonicNS += int64(delta)
	return current
}

func sameAuthorizationGeneration(
	expected actionlease.CurrentAuthorization,
	current actionlease.CurrentAuthorization,
) bool {
	return expected.Domain == current.Domain && expected.Capability == current.Capability &&
		expected.BundleGeneration == current.BundleGeneration &&
		expected.DomainPolicyGeneration == current.DomainPolicyGeneration &&
		expected.ControlStateGeneration == current.ControlStateGeneration &&
		expected.Target == current.Target && expected.PlanSHA256 == current.PlanSHA256 &&
		expected.BootID == current.BootID
}

func runnerOutcome(
	status policy.LeaseStatus,
	reason policy.LeaseOutcomeReason,
) *policy.ActionLeaseOutcome {
	return &policy.ActionLeaseOutcome{
		Schema:     policy.ActionLeaseOutcomeSchema,
		ActionID:   testActionID,
		Domain:     policy.DomainUser,
		Nonce:      "623e4567-e89b-42d3-a456-426614174000",
		Status:     status,
		Reason:     reason,
		ResolvedAt: "2030-01-01T00:00:20Z",
	}
}
