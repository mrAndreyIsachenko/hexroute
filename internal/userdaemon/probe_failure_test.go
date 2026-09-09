package userdaemon

import (
	"context"
	"errors"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"

	"github.com/mrAndreyIsachenko/hexroute/internal/pritunlplan"
	"github.com/mrAndreyIsachenko/hexroute/internal/userobserve"
)

// The probe that can see a missing service must not sit behind one that needs it.
//
// Reading the Pritunl profile goes through the service's socket. Reading the
// service goes through launchd and answers whether or not the service is there.
// The profile was read first and its failure returned from the cycle, so the
// only observation that could notice an absent service was unreachable exactly
// when the service was absent.
//
// Induced on the host on 2026-09-09: booting the service out removed the
// service, the tunnel and the client socket, and for nine minutes the user
// daemon logged nothing while its planner reported HEALTHY with zero
// consecutive failures.
func TestAFailedProfileProbeStillReachesTheServiceProbe(t *testing.T) {
	config := userRuntimeFixture(t)
	pritunl := disconnectedPritunl()
	// What a missing service does to the CLI: there is no socket to ask.
	pritunl.profileErr = errors.New("dial unix /var/run/pritunl.sock: no such file")
	cycle := newUserCycle(
		t,
		config,
		awakeSession(),
		pritunl,
		&fakeReadinessObserver{ready: true},
	)

	summary := cycle.Observe(context.Background(), 0, 20)

	if pritunl.profileCalls == 0 {
		t.Fatal("the profile was never probed; the test is not exercising the path")
	}
	if pritunl.serviceCalls == 0 {
		t.Fatal("the service was never probed: the observation that can see an " +
			"absent service is unreachable when the service is absent")
	}
	if summary.Failures == 0 {
		t.Fatal("a probe that could not run was not counted as a failure")
	}
}

// A cycle that could not see must still let the planner decide.
//
// Returning before the planner leaves its previous conclusion standing, and the
// runtime then reports the state it last observed as though it were current.
// That is how a supervised service stayed gone while its supervisor reported
// health.
func TestACycleThatCouldNotSeeStillConsultsThePlanner(t *testing.T) {
	config := userRuntimeFixture(t)
	pritunl := disconnectedPritunl()
	pritunl.profileErr = errors.New("dial unix /var/run/pritunl.sock: no such file")
	cycle := newUserCycle(
		t,
		config,
		awakeSession(),
		pritunl,
		&fakeReadinessObserver{ready: true},
	)

	summary := cycle.Observe(context.Background(), 0, 20)

	// failedSummary carries the planner's previous snapshot with ActionNone and
	// ReasonNone. A cycle that consulted the planner carries a reason for what
	// it decided, even when the decision is to do nothing.
	if summary.Plan.Reason == pritunlplan.ReasonNone {
		t.Fatalf("the planner was never consulted; the cycle returned the "+
			"previous conclusion as the current one: %+v", summary.Plan)
	}
}

// The state must move while the runtime cannot see.
//
// Consulting the planner is not enough on its own: if the failure does not
// advance the state machine, HEALTHY stands for as long as the outage lasts,
// which is the symptom exactly. On the host the service was gone for
// twenty-six minutes and the planner reported HEALTHY with zero consecutive
// failures throughout, then degraded one cycle after the service came back.
func TestRepeatedUnreadableCyclesDegradeTheState(t *testing.T) {
	config := userRuntimeFixture(t)
	pritunl := disconnectedPritunl()
	pritunl.profileErr = errors.New("dial unix /var/run/pritunl.sock: no such file")
	cycle := newUserCycle(
		t,
		config,
		awakeSession(),
		pritunl,
		&fakeReadinessObserver{ready: true},
	)

	var last pritunlplan.Plan
	for tick := range 5 {
		last = cycle.Observe(context.Background(), control.Tick(tick), 20).Plan
	}
	if last.Snapshot.ConsecutiveFailures == 0 {
		t.Fatalf("five cycles that could not see left the failure count at zero: %+v",
			last.Snapshot)
	}
	if last.State == control.StateHealthy {
		t.Fatalf("the runtime reports %s after five cycles that saw nothing; "+
			"the previous conclusion is standing as the current one", last.State)
	}
}

// An unreadable profile says nothing about the session, and must not be read as
// if it did.
func TestAnUnreadableProfileIsNotEvidenceOfAnything(t *testing.T) {
	config := userRuntimeFixture(t)
	pritunl := disconnectedPritunl()
	pritunl.profile = userobserve.ProfileObservation{
		Found: true,
		State: userobserve.ProfileActive,
	}
	pritunl.profileErr = errors.New("dial unix /var/run/pritunl.sock: no such file")
	cycle := newUserCycle(
		t,
		config,
		awakeSession(),
		pritunl,
		&fakeReadinessObserver{ready: true},
	)

	summary := cycle.Observe(context.Background(), 0, 20)

	if summary.Observed.ProfileError == nil {
		t.Fatal("the failure to read the profile was not recorded")
	}
	// The struct returned alongside an error describes nothing that was observed.
	if summary.Plan.Action == pritunlplan.ActionReconnect {
		t.Fatal("a reconnect was planned from a profile that could not be read")
	}
}
