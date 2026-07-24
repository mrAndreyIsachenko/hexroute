package userdaemon

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/observe"
	"github.com/mrAndreyIsachenko/hexroute/internal/pritunlplan"
	"github.com/mrAndreyIsachenko/hexroute/internal/userobserve"
)

type fakeSessionObserver struct {
	session userobserve.SessionObservation
	wake    userobserve.WakeObservation
	err     error
}

func (observer *fakeSessionObserver) UserSession(
	context.Context,
	int,
) (userobserve.SessionObservation, error) {
	return observer.session, observer.err
}

func (observer *fakeSessionObserver) Clamshell(
	context.Context,
) (userobserve.WakeObservation, error) {
	return observer.wake, observer.err
}

type fakePritunlObserver struct {
	profile      userobserve.ProfileObservation
	service      userobserve.ServiceObservation
	client       userobserve.ClientAddressObservation
	profileErr   error
	serviceErr   error
	clientErr    error
	profileCalls int
	serviceCalls int
	clientCalls  int
}

func (observer *fakePritunlObserver) Profile(
	context.Context,
	string,
) (userobserve.ProfileObservation, error) {
	observer.profileCalls++
	return observer.profile, observer.profileErr
}

func (observer *fakePritunlObserver) Service(
	context.Context,
) (userobserve.ServiceObservation, error) {
	observer.serviceCalls++
	return observer.service, observer.serviceErr
}

func (observer *fakePritunlObserver) ClientAddress(
	context.Context,
	userobserve.ProfileObservation,
) (userobserve.ClientAddressObservation, error) {
	observer.clientCalls++
	return observer.client, observer.clientErr
}

type fakeReadinessObserver struct {
	ready bool
	err   error
	calls int
}

func (observer *fakeReadinessObserver) Endpoint(
	context.Context,
	observe.Endpoint,
) (observe.ReadinessObservation, error) {
	observer.calls++
	return observe.ReadinessObservation{Name: "outer-ready", Ready: observer.ready}, observer.err
}

func userRuntimeFixture(t *testing.T) RuntimeConfig {
	t.Helper()
	config, err := DecodeConfig(strings.NewReader(validConfig))
	if err != nil {
		t.Fatalf("DecodeConfig() error: %v", err)
	}
	config.Policy.WakeSettle = 0
	config.Policy.Recovery.FailureThreshold = 1
	config.Policy.Recovery.ActionBudget = 1
	return config
}

func awakeSession() *fakeSessionObserver {
	return &fakeSessionObserver{
		session: userobserve.SessionObservation{State: userobserve.SessionActive},
		wake: userobserve.WakeObservation{
			Lid:  observe.LidStateOpen,
			Wake: observe.WakeKindFull,
		},
	}
}

func connectedPritunl() *fakePritunlObserver {
	return &fakePritunlObserver{
		profile: userobserve.ProfileObservation{
			Found:            true,
			State:            userobserve.ProfileActive,
			HasClientAddress: true,
		},
		service: userobserve.ServiceObservation{
			Loaded:  true,
			Running: true,
			PID:     123,
		},
		client: userobserve.ClientAddressObservation{
			Present:   true,
			Interface: "utun8",
		},
	}
}

func disconnectedPritunl() *fakePritunlObserver {
	observer := connectedPritunl()
	observer.profile = userobserve.ProfileObservation{
		Found: true,
		State: userobserve.ProfileInactive,
	}
	return observer
}

func newUserCycle(
	t *testing.T,
	config RuntimeConfig,
	session *fakeSessionObserver,
	pritunl *fakePritunlObserver,
	readiness *fakeReadinessObserver,
) *Cycle {
	t.Helper()
	planner, err := pritunlplan.NewPlanner(
		config.Policy,
		control.NewSnapshot(control.StateHealthy),
	)
	if err != nil {
		t.Fatalf("pritunlplan.NewPlanner() error: %v", err)
	}
	cycle, err := NewCycle(config, session, pritunl, readiness, planner)
	if err != nil {
		t.Fatalf("NewCycle() error: %v", err)
	}
	return cycle
}

func TestCycleProposesReconnectForDisconnectedProfile(t *testing.T) {
	config := userRuntimeFixture(t)
	cycle := newUserCycle(
		t,
		config,
		awakeSession(),
		disconnectedPritunl(),
		&fakeReadinessObserver{ready: true},
	)

	summary := cycle.Observe(context.Background(), 0, 20)
	if summary.Plan.Action != pritunlplan.ActionReconnect ||
		summary.Plan.Reason != pritunlplan.ReasonReconnectAllowed ||
		!summary.Plan.ObserveOnly {
		t.Fatalf("Observe() = %+v", summary)
	}
}

func TestCycleReportsStaleServiceWithoutApplyingRecovery(t *testing.T) {
	config := userRuntimeFixture(t)
	pritunl := disconnectedPritunl()
	pritunl.service = userobserve.ServiceObservation{Loaded: true}
	cycle := newUserCycle(
		t,
		config,
		awakeSession(),
		pritunl,
		&fakeReadinessObserver{ready: true},
	)

	summary := cycle.Observe(context.Background(), 0, 20)
	if summary.Failures != 1 ||
		summary.ServiceRunning ||
		summary.Plan.Action != pritunlplan.ActionReconnect ||
		!summary.Plan.ObserveOnly {
		t.Fatalf("Observe() = %+v", summary)
	}
}

func TestCycleBlocksReconnectWhenOuterReadinessIsInvalid(t *testing.T) {
	config := userRuntimeFixture(t)
	cycle := newUserCycle(
		t,
		config,
		awakeSession(),
		disconnectedPritunl(),
		&fakeReadinessObserver{err: errors.New("invalid outer readiness")},
	)

	summary := cycle.Observe(context.Background(), 0, 20)
	if summary.Failures != 1 ||
		summary.OuterReady ||
		summary.Plan.Action != pritunlplan.ActionNone ||
		summary.Plan.Reason != pritunlplan.ReasonOuterNotReady ||
		summary.Plan.Snapshot.Attempts != 0 {
		t.Fatalf("Observe() = %+v", summary)
	}
}

func TestCycleSuspendsDarkWakeWithoutCallingNetworkAdapters(t *testing.T) {
	config := userRuntimeFixture(t)
	session := awakeSession()
	session.wake.Wake = observe.WakeKindDark
	pritunl := disconnectedPritunl()
	readiness := &fakeReadinessObserver{ready: true}
	cycle := newUserCycle(t, config, session, pritunl, readiness)

	summary := cycle.Observe(context.Background(), 0, 20)
	if summary.Plan.Action != pritunlplan.ActionNone ||
		summary.Plan.Reason != pritunlplan.ReasonDarkWake ||
		summary.Plan.Snapshot.Attempts != 0 ||
		pritunl.profileCalls != 0 ||
		pritunl.serviceCalls != 0 ||
		readiness.calls != 0 {
		t.Fatalf(
			"Observe() = %+v calls=%d/%d/%d",
			summary,
			pritunl.profileCalls,
			pritunl.serviceCalls,
			readiness.calls,
		)
	}
}

func TestCycleWaitsForFullWakeSettleBeforeReconnect(t *testing.T) {
	config := userRuntimeFixture(t)
	config.Policy.WakeSettle = 30
	cycle := newUserCycle(
		t,
		config,
		awakeSession(),
		disconnectedPritunl(),
		&fakeReadinessObserver{ready: true},
	)

	settling := cycle.Observe(context.Background(), 100, 20)
	if settling.Plan.Action != pritunlplan.ActionNone ||
		settling.Plan.Reason != pritunlplan.ReasonWakeSettling ||
		settling.Plan.NextEvaluationAt != 130 {
		t.Fatalf("settling Observe() = %+v", settling)
	}
	ready := cycle.Observe(context.Background(), 130, 20)
	if ready.Plan.Action != pritunlplan.ActionReconnect ||
		ready.Plan.Reason != pritunlplan.ReasonReconnectAllowed {
		t.Fatalf("ready Observe() = %+v", ready)
	}
}

func TestCycleStopsAtExhaustedReconnectBudget(t *testing.T) {
	config := userRuntimeFixture(t)
	cycle := newUserCycle(
		t,
		config,
		awakeSession(),
		disconnectedPritunl(),
		&fakeReadinessObserver{ready: true},
	)

	first := cycle.Observe(context.Background(), 0, 20)
	if first.Plan.Action != pritunlplan.ActionReconnect {
		t.Fatalf("first Observe() = %+v", first)
	}
	exhausted := cycle.Observe(
		context.Background(),
		config.Policy.ConnectingGrace,
		20,
	)
	if exhausted.Plan.Action != pritunlplan.ActionNone ||
		exhausted.Plan.Reason != pritunlplan.ReasonRecoveryBudget ||
		exhausted.Plan.State != control.StateSafeMode ||
		exhausted.Plan.Snapshot.Attempts != 1 {
		t.Fatalf("exhausted Observe() = %+v", exhausted)
	}
}

func TestClientAddressDiagnosticFailureCannotOverrideConnectedProfile(t *testing.T) {
	config := userRuntimeFixture(t)
	pritunl := connectedPritunl()
	pritunl.clientErr = errors.New("diagnostic unavailable")
	cycle := newUserCycle(
		t,
		config,
		awakeSession(),
		pritunl,
		&fakeReadinessObserver{ready: true},
	)

	summary := cycle.Observe(context.Background(), 0, 20)
	if summary.Failures != 0 ||
		summary.Plan.Action != pritunlplan.ActionNone ||
		summary.Plan.Reason != pritunlplan.ReasonProfileConnected ||
		summary.Plan.State != control.StateHealthy {
		t.Fatalf("Observe() = %+v", summary)
	}
}
