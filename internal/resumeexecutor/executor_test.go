package resumeexecutor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/actionlease"
	"github.com/mrAndreyIsachenko/hexroute/internal/actionplan"
	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/resumeplan"
)

const (
	testActionID  metadata.UUID = "123e4567-e89b-42d3-a456-426614174010"
	testAttemptID metadata.UUID = "123e4567-e89b-42d3-a456-426614174011"
	testBootID    metadata.UUID = "123e4567-e89b-42d3-a456-426614174012"
)

type fakeAuthorizer struct {
	guard  *fakeGuard
	source *fakeAuthorizationSource
	err    error
	calls  int
}

func (authorizer *fakeAuthorizer) AuthorizeOperatorResume(
	plan resumeplan.Plan,
	bootID metadata.UUID,
) (actionplan.LeaseGuard, actionplan.AuthorizationSource, error) {
	authorizer.calls++
	if authorizer.err != nil {
		return nil, nil, authorizer.err
	}
	authorizer.source.current.Target = plan.ActionPlan().Target()
	authorizer.source.current.PlanSHA256 = plan.Digest()
	authorizer.source.current.ControlStateGeneration = plan.Before().Generation
	authorizer.source.current.BootID = bootID
	return authorizer.guard, authorizer.source, nil
}

type fakeAuthorizationSource struct {
	current actionlease.CurrentAuthorization
	err     error
}

func (source *fakeAuthorizationSource) Current(
	context.Context,
) (actionlease.CurrentAuthorization, error) {
	return source.current, source.err
}

type fakeGuard struct {
	started   bool
	outcome   *policy.ActionLeaseOutcome
	beforeErr error
	commitErr error
	abortErr  error
	aborts    int
	commits   int
}

func (guard *fakeGuard) ActionID() metadata.UUID  { return testActionID }
func (guard *fakeGuard) AttemptID() metadata.UUID { return testAttemptID }
func (guard *fakeGuard) BeginExecution() error {
	if guard.started {
		return actionlease.ErrLeaseReplay
	}
	guard.started = true
	return nil
}
func (guard *fakeGuard) BeforeStep(actionlease.CurrentAuthorization) error {
	return guard.beforeErr
}
func (guard *fakeGuard) Commit(current actionlease.CurrentAuthorization) error {
	guard.commits++
	if guard.commitErr != nil {
		return guard.commitErr
	}
	outcome := policy.ActionLeaseOutcome{
		Schema:   policy.ActionLeaseOutcomeSchema,
		ActionID: testActionID, Domain: policy.DomainUser,
		Nonce:  "123e4567-e89b-42d3-a456-426614174013",
		Status: policy.LeaseCommitted, Reason: policy.LeaseOutcomeCompleted,
		ResolvedAt: current.ObservedAt.Format(time.RFC3339Nano),
	}
	guard.outcome = &outcome
	return nil
}
func (guard *fakeGuard) Abort(time.Time) error {
	guard.aborts++
	return guard.abortErr
}
func (guard *fakeGuard) Outcome() (*policy.ActionLeaseOutcome, error) {
	return guard.outcome, nil
}

type fakeState struct {
	snapshot       control.Snapshot
	resumeResult   control.Snapshot
	resumeErr      error
	compensateErr  error
	enterSafeErr   error
	resumeCalls    int
	compensations  int
	enterSafeCalls int
}

func (state *fakeState) Snapshot() control.Snapshot { return state.snapshot }
func (state *fakeState) Resume(
	expected uint64,
	_ control.Tick,
) (control.Snapshot, error) {
	state.resumeCalls++
	if expected != state.snapshot.Generation {
		return control.Snapshot{}, control.ErrStaleGeneration
	}
	state.snapshot = state.resumeResult
	return state.snapshot, state.resumeErr
}
func (state *fakeState) CompensateOperatorResume(
	expected uint64,
	compensation control.Snapshot,
) (control.Snapshot, error) {
	state.compensations++
	if expected != state.snapshot.Generation {
		return control.Snapshot{}, control.ErrStaleGeneration
	}
	if state.compensateErr != nil {
		return control.Snapshot{}, state.compensateErr
	}
	state.snapshot = compensation
	return state.snapshot, nil
}
func (state *fakeState) EnterSafeMode(
	expected uint64,
	at control.Tick,
) (control.Snapshot, error) {
	state.enterSafeCalls++
	if expected != state.snapshot.Generation {
		return control.Snapshot{}, control.ErrStaleGeneration
	}
	if state.enterSafeErr != nil {
		return control.Snapshot{}, state.enterSafeErr
	}
	state.snapshot.Generation++
	state.snapshot.State = control.StateSafeMode
	state.snapshot.LastTick = at
	return state.snapshot, nil
}

type fakeIncidentSink struct {
	incidents []actionplan.CriticalIncident
}

func (sink *fakeIncidentSink) Emit(incident actionplan.CriticalIncident) error {
	sink.incidents = append(sink.incidents, incident)
	return nil
}

func TestExecutorCommitsExactStateOnlyResume(t *testing.T) {
	plan := testPlan(t)
	executor, state, guard, _ := testExecutor(t, plan)
	after, err := executor.ExecuteOperatorResume(plan)
	if err != nil || after != plan.Applied() || state.resumeCalls != 1 ||
		state.compensations != 0 || state.enterSafeCalls != 0 || guard.commits != 1 {
		t.Fatalf(
			"after=%+v error=%v state=%+v guard=%+v",
			after,
			err,
			state,
			guard,
		)
	}
}

func TestExecutorDenialCannotReachState(t *testing.T) {
	plan := testPlan(t)
	state := &fakeState{snapshot: plan.Before(), resumeResult: plan.Applied()}
	authorizer := &fakeAuthorizer{err: errors.New("synthetic denial")}
	executor, err := New(authorizer, state, testBootID, &fakeIncidentSink{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := executor.ExecuteOperatorResume(plan); !errors.Is(err, ErrExecutionFailed) {
		t.Fatalf("ExecuteOperatorResume() error=%v", err)
	}
	if state.resumeCalls != 0 || state.compensations != 0 || state.enterSafeCalls != 0 {
		t.Fatalf("denial reached state=%+v", state)
	}
}

func TestExecutorCommitFailureCompensatesMonotonically(t *testing.T) {
	plan := testPlan(t)
	executor, state, guard, _ := testExecutor(t, plan)
	guard.commitErr = errors.New("synthetic stale commit")
	after, err := executor.ExecuteOperatorResume(plan)
	if !errors.Is(err, ErrExecutionFailed) || after != plan.Compensated() ||
		state.resumeCalls != 1 || state.compensations != 1 ||
		state.enterSafeCalls != 0 || guard.aborts != 1 {
		t.Fatalf(
			"after=%+v error=%v state=%+v guard=%+v",
			after,
			err,
			state,
			guard,
		)
	}
}

func TestExecutorCannotReplayCommittedLease(t *testing.T) {
	plan := testPlan(t)
	executor, state, _, _ := testExecutor(t, plan)
	if _, err := executor.ExecuteOperatorResume(plan); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.ExecuteOperatorResume(plan); !errors.Is(err, ErrExecutionFailed) {
		t.Fatalf("replay error=%v", err)
	}
	if state.resumeCalls != 1 {
		t.Fatalf("replay invoked resume %d times", state.resumeCalls)
	}
}

func TestExecutorNeverCompensatesUnownedMutation(t *testing.T) {
	plan := testPlan(t)
	executor, state, _, incidents := testExecutor(t, plan)
	state.resumeErr = errors.New("synthetic uncertain apply")
	_, err := executor.ExecuteOperatorResume(plan)
	if !errors.Is(err, ErrExecutionFailed) || state.compensations != 0 ||
		state.enterSafeCalls != 1 || len(incidents.incidents) != 1 {
		t.Fatalf("error=%v state=%+v incidents=%+v", err, state, incidents.incidents)
	}
}

func testExecutor(
	t *testing.T,
	plan resumeplan.Plan,
) (*Executor, *fakeState, *fakeGuard, *fakeIncidentSink) {
	t.Helper()
	guard := &fakeGuard{}
	source := &fakeAuthorizationSource{current: actionlease.CurrentAuthorization{
		Domain: policy.DomainUser, Capability: policy.CapabilityOperatorResume,
		BundleGeneration: 1, DomainPolicyGeneration: 1,
		MonotonicNS: int64(time.Second),
		ObservedAt:  time.Date(2030, time.January, 1, 0, 0, 0, 0, time.UTC),
	}}
	state := &fakeState{snapshot: plan.Before(), resumeResult: plan.Applied()}
	incidents := &fakeIncidentSink{}
	executor, err := New(
		&fakeAuthorizer{guard: guard, source: source},
		state,
		testBootID,
		incidents,
	)
	if err != nil {
		t.Fatal(err)
	}
	return executor, state, guard, incidents
}

func testPlan(t *testing.T) resumeplan.Plan {
	t.Helper()
	before := control.NewSnapshot(control.StateSafeMode)
	before.Generation = 7
	before.Attempts = 3
	before.LastTick = 90
	before.SafeUntil = 700
	plan, err := resumeplan.Build(control.ComponentPritunl, before, 100)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}
