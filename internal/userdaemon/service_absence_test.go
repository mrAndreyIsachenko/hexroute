package userdaemon

import (
	"context"
	"errors"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/pritunlplan"
	"github.com/mrAndreyIsachenko/hexroute/internal/userobserve"
)

// A service that is not running is grounds to ask root, and nothing else can be.
//
// The service observation was collected and discarded: summary.ServiceRunning
// was written in one place and read by no production code, and the planner's
// Observation had no service field at all. Its only route to a rescue request
// was the blackhole case — a session reporting itself connected while carrying
// no traffic — so a service that was simply gone could never become a reason to
// ask for a restart.
//
// That is why inducing the documented precondition produced nothing: the
// runbook says to boot the service out and watch the chain run, and the chain
// had no path from that condition to a request.
func TestAServiceThatIsNotRunningAsksRootToRestartIt(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		service userobserve.ServiceObservation
		err     error
	}{
		{
			name:    "loaded but not running",
			service: userobserve.ServiceObservation{Loaded: true},
		},
		{
			name: "not in the launchd domain at all",
			err:  errors.New("service not found in the system domain"),
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			config := userRuntimeFixture(t)
			pritunl := disconnectedPritunl()
			pritunl.service, pritunl.serviceErr = testCase.service, testCase.err
			cycle := newUserCycle(
				t,
				config,
				awakeSession(),
				pritunl,
				&fakeReadinessObserver{ready: true},
			)

			plan := cycle.Observe(context.Background(), 0, 20).Plan
			if plan.Action != pritunlplan.ActionRequestRescue {
				t.Fatalf("action = %q, want %q — a reconnect goes through the "+
					"service that is not there",
					plan.Action, pritunlplan.ActionRequestRescue)
			}
		})
	}
}

// The condition the runbook induces must reach a request.
//
// Booting the service out removes the service, the tunnel and the client
// socket at once: the profile cannot be read and the service is not there.
func TestTheInducedPreconditionReachesARequest(t *testing.T) {
	config := userRuntimeFixture(t)
	pritunl := disconnectedPritunl()
	pritunl.profileErr = errors.New("dial unix /var/run/pritunl.sock: no such file")
	pritunl.serviceErr = errors.New("service not found in the system domain")
	cycle := newUserCycle(
		t,
		config,
		awakeSession(),
		pritunl,
		&fakeReadinessObserver{ready: true},
	)

	plan := cycle.Observe(context.Background(), 0, 20).Plan
	if plan.Action != pritunlplan.ActionRequestRescue {
		t.Fatalf("booting the service out planned %q, want %q", plan.Action,
			pritunlplan.ActionRequestRescue)
	}
}

// A running service is not grounds to ask for anything.
//
// The request costs root a restart of a production service. It must follow from
// the service being absent, not from the session being unhappy for its own
// reasons.
func TestARunningServiceIsNotGroundsForARestart(t *testing.T) {
	config := userRuntimeFixture(t)
	pritunl := disconnectedPritunl()
	pritunl.service = userobserve.ServiceObservation{Loaded: true, Running: true, PID: 42}
	cycle := newUserCycle(
		t,
		config,
		awakeSession(),
		pritunl,
		&fakeReadinessObserver{ready: true},
	)

	plan := cycle.Observe(context.Background(), 0, 20).Plan
	if plan.Action == pritunlplan.ActionRequestRescue {
		t.Fatal("a restart was requested while the service was running")
	}
}
