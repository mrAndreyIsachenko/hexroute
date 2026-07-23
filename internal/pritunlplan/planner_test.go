package pritunlplan

import (
	"errors"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/observe"
	"github.com/mrAndreyIsachenko/hexroute/internal/userobserve"
)

func testPolicy() Policy {
	return Policy{
		Recovery: control.Policy{
			FailureThreshold:   2,
			ActionBudget:       2,
			BaseBackoff:        15,
			MaxBackoff:         120,
			VerificationWindow: 30,
			Cooldown:           600,
		},
		WakeSettle:      30,
		ConnectingGrace: 120,
		OTPPeriod:       30,
		OTPMinValid:     8,
	}
}

func newPlanner(t *testing.T, policy Policy) *Planner {
	t.Helper()
	planner, err := NewPlanner(policy, control.NewSnapshot(control.StateHealthy))
	if err != nil {
		t.Fatalf("NewPlanner() error: %v", err)
	}
	return planner
}

func awake(at control.Tick) Observation {
	return Observation{
		At:      at,
		Session: userobserve.SessionActive,
		Wake: userobserve.WakeObservation{
			Lid:  observe.LidStateOpen,
			Wake: observe.WakeKindFull,
		},
		OuterReady: true,
		Profile: userobserve.ProfileObservation{
			Found:            true,
			State:            userobserve.ProfileActive,
			HasClientAddress: true,
		},
		OptionalInner:       OptionalInnerUnknown,
		OTPSecondsRemaining: 20,
	}
}

func settle(t *testing.T, planner *Planner) {
	t.Helper()
	first, err := planner.Plan(awake(0))
	if err != nil {
		t.Fatalf("initial Plan() error: %v", err)
	}
	if first.Reason != ReasonWakeSettling || first.State != control.StateSuspended {
		t.Fatalf("initial Plan() = %+v", first)
	}
	second, err := planner.Plan(awake(30))
	if err != nil {
		t.Fatalf("settled Plan() error: %v", err)
	}
	if second.Reason != ReasonProfileConnected || second.State != control.StateHealthy {
		t.Fatalf("settled Plan() = %+v", second)
	}
}

func TestOTPSecondsRemainingMatchesTOTPWindow(t *testing.T) {
	tests := []struct {
		unixSeconds int64
		want        uint32
	}{
		{unixSeconds: 0, want: 30},
		{unixSeconds: 1, want: 29},
		{unixSeconds: 22, want: 8},
		{unixSeconds: 29, want: 1},
		{unixSeconds: 30, want: 30},
	}
	for _, test := range tests {
		got, err := OTPSecondsRemaining(test.unixSeconds, 30)
		if err != nil {
			t.Fatalf("OTPSecondsRemaining(%d) error: %v", test.unixSeconds, err)
		}
		if got != test.want {
			t.Fatalf(
				"OTPSecondsRemaining(%d) = %d, want %d",
				test.unixSeconds,
				got,
				test.want,
			)
		}
	}
	if _, err := OTPSecondsRemaining(-1, 30); err == nil {
		t.Fatal("negative Unix time accepted")
	}
	if _, err := OTPSecondsRemaining(1, 0); err == nil {
		t.Fatal("zero TOTP period accepted")
	}
}

func TestPlannerRejectsInvalidPolicyAndObservation(t *testing.T) {
	policy := testPolicy()
	policy.OTPMinValid = policy.OTPPeriod + 1
	if _, err := NewPlanner(policy, control.NewSnapshot(control.StateHealthy)); err == nil {
		t.Fatal("invalid OTP policy accepted")
	}

	planner := newPlanner(t, testPolicy())
	observation := awake(1)
	observation.OTPSecondsRemaining = testPolicy().OTPPeriod + 1
	if _, err := planner.Plan(observation); err == nil {
		t.Fatal("invalid OTP observation accepted")
	}
	observation = awake(1)
	observation.OptionalInner = OptionalInnerState("unsupported")
	if _, err := planner.Plan(observation); err == nil {
		t.Fatal("invalid optional inner state accepted")
	}
}

func TestPlannerRejectsNonMonotonicObservationOnNoEventBranch(t *testing.T) {
	policy := testPolicy()
	policy.WakeSettle = 0
	planner := newPlanner(t, policy)
	observation := awake(100)
	observation.OuterReady = false
	if _, err := planner.Plan(observation); err != nil {
		t.Fatalf("initial Plan() error: %v", err)
	}

	observation.At = 99
	if _, err := planner.Plan(observation); !errors.Is(err, control.ErrNonMonotonicTick) {
		t.Fatalf("non-monotonic Plan() error = %v, want %v", err, control.ErrNonMonotonicTick)
	}
}

func TestSleepDarkWakeAndSettleDoNotConsumeRecoveryBudget(t *testing.T) {
	planner := newPlanner(t, testPolicy())
	tests := []struct {
		name        string
		observation Observation
		reason      Reason
	}{
		{
			name: "inactive session",
			observation: Observation{
				At:      1,
				Session: userobserve.SessionInactive,
			},
			reason: ReasonSessionInactive,
		},
		{
			name: "closed lid",
			observation: Observation{
				At:      2,
				Session: userobserve.SessionActive,
				Wake: userobserve.WakeObservation{
					Lid:  observe.LidStateClosed,
					Wake: observe.WakeKindFull,
				},
			},
			reason: ReasonLidClosed,
		},
		{
			name: "dark wake",
			observation: Observation{
				At:      3,
				Session: userobserve.SessionActive,
				Wake: userobserve.WakeObservation{
					Lid:  observe.LidStateOpen,
					Wake: observe.WakeKindDark,
				},
			},
			reason: ReasonDarkWake,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan, err := planner.Plan(test.observation)
			if err != nil {
				t.Fatalf("Plan() error: %v", err)
			}
			if plan.Action != ActionNone ||
				plan.Reason != test.reason ||
				plan.Snapshot.Attempts != 0 ||
				plan.Snapshot.ConsecutiveFailures != 0 {
				t.Fatalf("Plan() = %+v", plan)
			}
		})
	}

	settling, err := planner.Plan(awake(4))
	if err != nil {
		t.Fatalf("Plan() settle error: %v", err)
	}
	if settling.Reason != ReasonWakeSettling ||
		settling.NextEvaluationAt != 34 ||
		settling.Snapshot.Attempts != 0 {
		t.Fatalf("settling Plan() = %+v", settling)
	}
}

func TestOuterReadinessBlocksThresholdAndReconnect(t *testing.T) {
	planner := newPlanner(t, testPolicy())
	settle(t, planner)
	observation := awake(31)
	observation.OuterReady = false
	observation.Profile = userobserve.ProfileObservation{
		Found: true,
		State: userobserve.ProfileInactive,
	}

	for observation.At = 31; observation.At < 40; observation.At++ {
		plan, err := planner.Plan(observation)
		if err != nil {
			t.Fatalf("Plan() error: %v", err)
		}
		if plan.Reason != ReasonOuterNotReady ||
			plan.Action != ActionNone ||
			plan.Snapshot.ConsecutiveFailures != 0 ||
			plan.Snapshot.Attempts != 0 {
			t.Fatalf("Plan() = %+v", plan)
		}
	}
}

func TestActiveWithClientAddressIgnoresOptionalInnerFailure(t *testing.T) {
	policy := testPolicy()
	policy.WakeSettle = 0
	policy.Recovery.FailureThreshold = 1
	planner := newPlanner(t, policy)
	observation := awake(0)
	observation.OptionalInner = OptionalInnerFailed

	for observation.At = 0; observation.At < 3; observation.At++ {
		plan, err := planner.Plan(observation)
		if err != nil {
			t.Fatalf("Plan() error: %v", err)
		}
		if plan.State != control.StateHealthy ||
			plan.Action != ActionNone ||
			plan.Reason != ReasonProfileConnected ||
			plan.Snapshot.ConsecutiveFailures != 0 ||
			plan.Snapshot.Attempts != 0 {
			t.Fatalf("Plan() = %+v", plan)
		}
	}
}

func TestOptionalInnerSuccessCannotReplaceMissingClientAddress(t *testing.T) {
	policy := testPolicy()
	policy.WakeSettle = 0
	policy.Recovery.FailureThreshold = 1
	planner := newPlanner(t, policy)
	observation := awake(0)
	observation.Profile = userobserve.ProfileObservation{
		Found: true,
		State: userobserve.ProfileActive,
	}
	observation.OptionalInner = OptionalInnerReady

	plan, err := planner.Plan(observation)
	if err != nil {
		t.Fatalf("Plan() error: %v", err)
	}
	if plan.Action != ActionReconnect ||
		plan.State != control.StateRecovering ||
		plan.Reason != ReasonReconnectAllowed {
		t.Fatalf("Plan() = %+v", plan)
	}
}

func TestReconnectRequiresFailureThresholdAndValidOTPWindow(t *testing.T) {
	planner := newPlanner(t, testPolicy())
	settle(t, planner)
	disconnected := awake(31)
	disconnected.Profile = userobserve.ProfileObservation{
		Found: true,
		State: userobserve.ProfileInactive,
	}

	first, err := planner.Plan(disconnected)
	if err != nil {
		t.Fatalf("first Plan() error: %v", err)
	}
	if first.Action != ActionNone ||
		first.State != control.StateHealthy ||
		first.Snapshot.ConsecutiveFailures != 1 {
		t.Fatalf("first Plan() = %+v", first)
	}

	disconnected.At = 32
	disconnected.OTPSecondsRemaining = 3
	blocked, err := planner.Plan(disconnected)
	if err != nil {
		t.Fatalf("blocked Plan() error: %v", err)
	}
	if blocked.Action != ActionNone ||
		blocked.State != control.StateDegraded ||
		blocked.Reason != ReasonOTPWindowTooShort ||
		blocked.Snapshot.Attempts != 0 ||
		blocked.NextEvaluationAt != 36 {
		t.Fatalf("blocked Plan() = %+v", blocked)
	}

	disconnected.At = 36
	disconnected.OTPSecondsRemaining = 24
	approved, err := planner.Plan(disconnected)
	if err != nil {
		t.Fatalf("approved Plan() error: %v", err)
	}
	if approved.Action != ActionReconnect ||
		approved.State != control.StateRecovering ||
		approved.Reason != ReasonReconnectAllowed ||
		approved.Snapshot.Attempts != 1 {
		t.Fatalf("approved Plan() = %+v", approved)
	}
}

func TestConnectingGraceAndRecoveryVerification(t *testing.T) {
	policy := testPolicy()
	policy.Recovery.FailureThreshold = 1
	planner := newPlanner(t, policy)
	settle(t, planner)

	connecting := awake(31)
	connecting.Profile = userobserve.ProfileObservation{
		Found:      true,
		State:      userobserve.ProfileActive,
		Connecting: true,
	}
	first, err := planner.Plan(connecting)
	if err != nil {
		t.Fatalf("first connecting Plan() error: %v", err)
	}
	if first.Reason != ReasonProfileConnecting ||
		first.NextEvaluationAt != 151 ||
		first.Snapshot.ConsecutiveFailures != 0 {
		t.Fatalf("first connecting Plan() = %+v", first)
	}

	connecting.At = 150
	grace, err := planner.Plan(connecting)
	if err != nil {
		t.Fatalf("grace Plan() error: %v", err)
	}
	if grace.Action != ActionNone || grace.Reason != ReasonProfileConnecting {
		t.Fatalf("grace Plan() = %+v", grace)
	}

	connecting.At = 151
	stale, err := planner.Plan(connecting)
	if err != nil {
		t.Fatalf("stale Plan() error: %v", err)
	}
	if stale.Action != ActionReconnect || stale.State != control.StateRecovering {
		t.Fatalf("stale Plan() = %+v", stale)
	}

	connected := awake(160)
	verifying, err := planner.Plan(connected)
	if err != nil {
		t.Fatalf("verifying Plan() error: %v", err)
	}
	if verifying.State != control.StateRecovering ||
		verifying.Reason != ReasonRecoveryVerifying {
		t.Fatalf("verifying Plan() = %+v", verifying)
	}

	connected.At = 181
	healthy, err := planner.Plan(connected)
	if err != nil {
		t.Fatalf("healthy Plan() error: %v", err)
	}
	if healthy.State != control.StateHealthy ||
		healthy.Reason != ReasonProfileConnected ||
		healthy.Snapshot.Attempts != 0 {
		t.Fatalf("healthy Plan() = %+v", healthy)
	}
}

func TestExhaustedBudgetEntersSafeModeUntilCooldown(t *testing.T) {
	policy := testPolicy()
	policy.Recovery.FailureThreshold = 1
	policy.Recovery.ActionBudget = 1
	policy.ConnectingGrace = 10
	policy.Recovery.Cooldown = 100
	planner := newPlanner(t, policy)
	settle(t, planner)

	disconnected := awake(31)
	disconnected.Profile = userobserve.ProfileObservation{
		Found: true,
		State: userobserve.ProfileInactive,
	}
	first, err := planner.Plan(disconnected)
	if err != nil {
		t.Fatalf("first Plan() error: %v", err)
	}
	if first.Action != ActionReconnect {
		t.Fatalf("first Plan() = %+v", first)
	}

	disconnected.At = 40
	waiting, err := planner.Plan(disconnected)
	if err != nil {
		t.Fatalf("waiting Plan() error: %v", err)
	}
	if waiting.Reason != ReasonRecoveryVerifying {
		t.Fatalf("waiting Plan() = %+v", waiting)
	}

	disconnected.At = 41
	safe, err := planner.Plan(disconnected)
	if err != nil {
		t.Fatalf("safe Plan() error: %v", err)
	}
	if safe.State != control.StateSafeMode ||
		safe.Action != ActionNone ||
		safe.NextEvaluationAt != 141 {
		t.Fatalf("safe Plan() = %+v", safe)
	}

	disconnected.At = 140
	stillSafe, err := planner.Plan(disconnected)
	if err != nil {
		t.Fatalf("still-safe Plan() error: %v", err)
	}
	if stillSafe.State != control.StateSafeMode || stillSafe.Action != ActionNone {
		t.Fatalf("still-safe Plan() = %+v", stillSafe)
	}

	disconnected.At = 141
	afterCooldown, err := planner.Plan(disconnected)
	if err != nil {
		t.Fatalf("after-cooldown Plan() error: %v", err)
	}
	if afterCooldown.Action != ActionReconnect ||
		afterCooldown.State != control.StateRecovering ||
		afterCooldown.Snapshot.Attempts != 1 {
		t.Fatalf("after-cooldown Plan() = %+v", afterCooldown)
	}
}
