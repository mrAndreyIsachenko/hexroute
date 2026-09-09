package pritunlplan

import (
	"errors"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/observe"
	"github.com/mrAndreyIsachenko/hexroute/internal/userobserve"
)

type Action string

const (
	ActionNone      Action = "none"
	ActionReconnect Action = "reconnect"
	// ActionRequestRescue asks the root runtime to restart the Pritunl
	// service. It is not a reconnect and never becomes one: root revalidates
	// the request against its own observations before acting.
	ActionRequestRescue Action = "request_rescue"
)

type Reason string

const (
	ReasonNone                Reason = "none"
	ReasonSessionInactive     Reason = "session_inactive"
	ReasonLidClosed           Reason = "lid_closed"
	ReasonDarkWake            Reason = "dark_wake"
	ReasonWakeUnknown         Reason = "wake_unknown"
	ReasonWakeSettling        Reason = "wake_settling"
	ReasonOuterNotReady       Reason = "outer_not_ready"
	ReasonProfileConnected    Reason = "profile_connected"
	ReasonProfileNotConnected Reason = "profile_not_connected"
	ReasonProfileConnecting   Reason = "profile_connecting"
	ReasonRecoveryVerifying   Reason = "recovery_verifying"
	ReasonOTPWindowTooShort   Reason = "otp_window_too_short"
	ReasonRecoveryBackoff     Reason = "recovery_backoff"
	ReasonRecoveryBudget      Reason = "recovery_budget_exhausted"
	ReasonReconnectAllowed    Reason = "reconnect_allowed"
	// ReasonInnerBlackholed is a session that reports itself connected, with a
	// client address, while the path it should be carrying carries nothing.
	ReasonInnerBlackholed Reason = "inner_blackholed"

	// ReasonProfileUnreadable is the profile probe failing rather than the
	// profile reporting anything.
	//
	// It is not "not connected". Pritunl is the authority on its own session,
	// and a probe that could not reach it carries no statement from that
	// authority — reconnecting on it would act on nothing.
	ReasonProfileUnreadable Reason = "profile_unreadable"

	// ReasonServiceNotRunning is the service beneath the session being absent.
	//
	// It is a reason to ask root, not a reason to reconnect: the reconnect would
	// go through the service that is not there.
	ReasonServiceNotRunning Reason = "service_not_running"
)

// ServiceState is what was seen of the service beneath the session.
type ServiceState string

const (
	// ServiceUnspecified is a caller that said nothing about the service. It is
	// the zero value, and it asks for nothing.
	ServiceUnspecified ServiceState = ""
	// ServiceRunning was observed running.
	ServiceRunning ServiceState = "running"
	// ServiceStopped was observed not running, or could not be observed.
	ServiceStopped ServiceState = "stopped"
)

type OptionalInnerState string

const (
	OptionalInnerUnspecified OptionalInnerState = ""
	OptionalInnerUnknown     OptionalInnerState = "unknown"
	OptionalInnerReady       OptionalInnerState = "ready"
	OptionalInnerFailed      OptionalInnerState = "failed"
)

type Policy struct {
	Recovery        control.Policy
	WakeSettle      control.Tick
	ConnectingGrace control.Tick
	OTPPeriod       uint32
	OTPMinValid     uint32
}

type Observation struct {
	At         control.Tick
	Session    userobserve.SessionState
	Wake       userobserve.WakeObservation
	OuterReady bool
	Profile    userobserve.ProfileObservation
	// OptionalInner is diagnostic-only for reconnecting: Pritunl's Active
	// state plus a client address remains authoritative there, because a path
	// measurement is affected by the outer tunnel and the routes as much as by
	// Pritunl, and reconnecting on it would reconnect Pritunl for faults that
	// are not its.
	//
	// It is not diagnostic-only for asking root to restart a stale service. A
	// session reporting itself connected while carrying nothing is the one
	// state in which everything looks healthy and nothing works, and it is the
	// most recent real cause of a restart.
	OptionalInner       OptionalInnerState
	OTPSecondsRemaining uint32
	// Service is what was seen of the service beneath the session.
	//
	// It is three-valued on purpose. This field grounds a request for root to
	// restart a production service, so the value a caller gets by not setting
	// it must be the one that asks for nothing: an unset field is a caller that
	// said nothing, not a caller reporting a stopped service.
	//
	// ServiceStopped covers both "observed and not running" and "could not be
	// observed". Those are one answer to the only question asked here — is the
	// thing that would carry out a reconnect there — and a service that cannot
	// be found is not one that is running. Conflating them is safe because what
	// it grounds is a request: root reaches its own conclusion before
	// restarting anything, and refuses when it disagrees.
	//
	// Until this existed the service observation was collected and discarded,
	// so a service that was simply gone could never become a reason to ask.
	Service ServiceState
	// ProfileUnreadable says the profile probe did not run, as distinct from
	// running and reporting a session that is not connected.
	//
	// The zero value is "it was read", because until this existed every caller
	// had read it — a cycle whose profile probe failed returned before the
	// planner was consulted at all, which is how a supervised service stayed
	// absent for twenty-six minutes while the runtime reported HEALTHY.
	ProfileUnreadable bool
}

type Plan struct {
	ObserveOnly      bool
	State            control.State
	Action           Action
	Reason           Reason
	NextEvaluationAt control.Tick
	Snapshot         control.Snapshot
}

type Planner struct {
	policy             Policy
	machine            *control.Machine
	fullWakeSince      control.Tick
	trackingFullWake   bool
	connectingSince    control.Tick
	trackingConnecting bool
	lastObservationAt  control.Tick
	hasObservation     bool
	lastRescueAt       control.Tick
	hasRequestedRescue bool
}

var ErrInvalidInput = errors.New("invalid Pritunl planner input")

type SnapshotPersister func(control.Snapshot) error

func OTPSecondsRemaining(unixSeconds int64, period uint32) (uint32, error) {
	if unixSeconds < 0 || period == 0 {
		return 0, ErrInvalidInput
	}
	return period - uint32(unixSeconds%int64(period)), nil
}

func NewPlanner(policy Policy, snapshot control.Snapshot) (*Planner, error) {
	if policy.WakeSettle < 0 ||
		policy.ConnectingGrace <= 0 ||
		policy.OTPPeriod == 0 ||
		policy.OTPMinValid == 0 ||
		policy.OTPMinValid > policy.OTPPeriod {
		return nil, ErrInvalidInput
	}
	machine, err := control.NewMachine(policy.Recovery, snapshot)
	if err != nil {
		return nil, err
	}
	return &Planner{
		policy:  policy,
		machine: machine,
	}, nil
}

func (planner *Planner) Snapshot() control.Snapshot {
	if planner == nil || planner.machine == nil {
		return control.Snapshot{}
	}
	return planner.machine.Snapshot()
}

func (planner *Planner) Plan(observation Observation) (Plan, error) {
	if planner == nil ||
		planner.machine == nil ||
		observation.At < 0 ||
		!observation.OptionalInner.valid() ||
		observation.OTPSecondsRemaining > planner.policy.OTPPeriod {
		return Plan{}, ErrInvalidInput
	}
	if observation.At < planner.machine.Snapshot().LastTick ||
		(planner.hasObservation && observation.At < planner.lastObservationAt) {
		return Plan{}, control.ErrNonMonotonicTick
	}
	planner.lastObservationAt = observation.At
	planner.hasObservation = true

	switch {
	case observation.Session != userobserve.SessionActive:
		return planner.suspend(observation.At, ReasonSessionInactive)
	case observation.Wake.Lid == observe.LidStateClosed:
		return planner.suspend(observation.At, ReasonLidClosed)
	case observation.Wake.Wake == observe.WakeKindDark:
		return planner.suspend(observation.At, ReasonDarkWake)
	case observation.Wake.Lid != observe.LidStateOpen ||
		observation.Wake.Wake != observe.WakeKindFull:
		return planner.suspend(observation.At, ReasonWakeUnknown)
	}

	if !planner.trackingFullWake {
		planner.trackingFullWake = true
		planner.fullWakeSince = observation.At
	}
	if observation.At-planner.fullWakeSince < planner.policy.WakeSettle {
		planner.trackingConnecting = false
		if _, err := planner.step(observation.At, control.EventSuspend); err != nil {
			return Plan{}, err
		}
		plan := planner.result(ReasonWakeSettling, ActionNone, 0)
		plan.NextEvaluationAt = planner.fullWakeSince + planner.policy.WakeSettle
		return plan, nil
	}

	if !observation.OuterReady {
		return planner.result(ReasonOuterNotReady, ActionNone, 0), nil
	}
	if planner.machine.Snapshot().State == control.StateSafeMode {
		if observation.At < planner.machine.Snapshot().SafeUntil {
			return planner.result(
				ReasonRecoveryBudget,
				ActionNone,
				planner.machine.Snapshot().SafeUntil,
			), nil
		}
		if _, err := planner.step(observation.At, control.EventCooldownElapsed); err != nil {
			return Plan{}, err
		}
	}
	if planner.machine.Snapshot().State == control.StateSuspended {
		if _, err := planner.step(observation.At, control.EventDependenciesReady); err != nil {
			return Plan{}, err
		}
	}

	// A session that is not carrying traffic cannot be repaired by reconnecting
	// through a service that is not running: the reconnect goes through that
	// service. So the service's absence is asked about first, and it is asked
	// about as a request rather than acted on — root looks for itself.
	if !observation.Profile.Connected() && observation.Service == ServiceStopped &&
		planner.rescueRequestAllowed(observation.At) {
		if _, err := planner.step(observation.At, control.EventProbeFailed); err != nil {
			return Plan{}, err
		}
		planner.lastRescueAt = observation.At
		planner.hasRequestedRescue = true
		return planner.result(ReasonServiceNotRunning, ActionRequestRescue, 0), nil
	}

	if observation.ProfileUnreadable {
		// The state still moves: a probe did fail, and saying otherwise is how
		// the previous conclusion goes on being reported as the current one.
		// What does not follow is an action. Reconnecting needs Pritunl's
		// account of its own session, and there is none.
		if _, err := planner.step(observation.At, control.EventProbeFailed); err != nil {
			return Plan{}, err
		}
		return planner.result(ReasonProfileUnreadable, ActionNone, 0), nil
	}

	if observation.Profile.Connected() {
		planner.trackingConnecting = false
		decision, err := planner.step(observation.At, control.EventProbeSucceeded)
		if err != nil {
			return Plan{}, err
		}
		reason := ReasonProfileConnected
		if decision.To == control.StateRecovering {
			reason = ReasonRecoveryVerifying
		}
		// Connected and carrying nothing. The session is not reconnected on
		// this ground — Pritunl is the authority on its own session — but the
		// service beneath it may be stale, and root is asked to look.
		if observation.OptionalInner == OptionalInnerFailed &&
			planner.rescueRequestAllowed(observation.At) {
			planner.lastRescueAt = observation.At
			planner.hasRequestedRescue = true
			return planner.result(ReasonInnerBlackholed, ActionRequestRescue, 0), nil
		}
		return planner.result(reason, ActionNone, 0), nil
	}

	if observation.Profile.Connecting {
		if !planner.trackingConnecting {
			planner.trackingConnecting = true
			planner.connectingSince = observation.At
		}
		if observation.At-planner.connectingSince < planner.policy.ConnectingGrace {
			return planner.result(
				ReasonProfileConnecting,
				ActionNone,
				planner.connectingSince+planner.policy.ConnectingGrace,
			), nil
		}
	} else {
		planner.trackingConnecting = false
	}

	snapshot := planner.machine.Snapshot()
	if snapshot.State == control.StateRecovering &&
		observation.At-snapshot.RecoveringSince < planner.policy.ConnectingGrace {
		return planner.result(
			ReasonRecoveryVerifying,
			ActionNone,
			snapshot.RecoveringSince+planner.policy.ConnectingGrace,
		), nil
	}

	decision, err := planner.step(observation.At, control.EventProbeFailed)
	if err != nil {
		return Plan{}, err
	}
	switch decision.To {
	case control.StateHealthy:
		return planner.result(ReasonProfileNotConnected, ActionNone, 0), nil
	case control.StateSafeMode:
		return planner.result(
			ReasonRecoveryBudget,
			ActionNone,
			planner.machine.Snapshot().SafeUntil,
		), nil
	case control.StateDegraded:
	default:
		return planner.result(ReasonProfileNotConnected, ActionNone, 0), nil
	}

	if observation.OTPSecondsRemaining < planner.policy.OTPMinValid {
		return planner.result(
			ReasonOTPWindowTooShort,
			ActionNone,
			observation.At+control.Tick(observation.OTPSecondsRemaining)+1,
		), nil
	}

	decision, err = planner.step(observation.At, control.EventBeginRecovery)
	if err != nil {
		return Plan{}, err
	}
	if decision.RecoveryApproved {
		return planner.result(ReasonReconnectAllowed, ActionReconnect, 0), nil
	}
	if decision.To == control.StateSafeMode {
		return planner.result(
			ReasonRecoveryBudget,
			ActionNone,
			planner.machine.Snapshot().SafeUntil,
		), nil
	}
	return planner.result(
		ReasonRecoveryBackoff,
		ActionNone,
		planner.machine.Snapshot().NextActionAt,
	), nil
}

func (planner *Planner) Resume(
	expectedGeneration uint64,
	at control.Tick,
	persist SnapshotPersister,
) (control.Snapshot, error) {
	if planner == nil || planner.machine == nil || at < 0 || persist == nil {
		return control.Snapshot{}, ErrInvalidInput
	}
	before := planner.machine.Snapshot()
	if _, err := planner.machine.Step(
		expectedGeneration,
		at,
		control.EventOperatorResume,
	); err != nil {
		return control.Snapshot{}, err
	}
	after := planner.machine.Snapshot()
	if err := persist(after); err != nil {
		rollback, rollbackErr := control.NewMachine(planner.policy.Recovery, before)
		if rollbackErr != nil {
			return control.Snapshot{}, rollbackErr
		}
		planner.machine = rollback
		return control.Snapshot{}, err
	}
	return after, nil
}

func (planner *Planner) CompensateOperatorResume(
	expectedGeneration uint64,
	compensation control.Snapshot,
	persist SnapshotPersister,
) (control.Snapshot, error) {
	if planner == nil || planner.machine == nil || persist == nil {
		return control.Snapshot{}, ErrInvalidInput
	}
	current := planner.machine.Snapshot()
	if current.Generation != expectedGeneration {
		return control.Snapshot{}, control.ErrStaleGeneration
	}
	if current.State != control.StateDegraded ||
		compensation.State != control.StateSafeMode ||
		compensation.Generation != current.Generation+1 ||
		compensation.LastTick < current.LastTick {
		return control.Snapshot{}, ErrInvalidInput
	}
	return planner.replaceSnapshot(compensation, persist)
}

func (planner *Planner) EnterSafeMode(
	expectedGeneration uint64,
	at control.Tick,
	persist SnapshotPersister,
) (control.Snapshot, error) {
	if planner == nil || planner.machine == nil || persist == nil || at < 0 {
		return control.Snapshot{}, ErrInvalidInput
	}
	current := planner.machine.Snapshot()
	if current.Generation != expectedGeneration {
		return control.Snapshot{}, control.ErrStaleGeneration
	}
	if at < current.LastTick {
		return control.Snapshot{}, control.ErrNonMonotonicTick
	}
	safe := current
	safe.Generation++
	safe.State = control.StateSafeMode
	safe.Attempts = planner.policy.Recovery.ActionBudget
	safe.RecoveringSince = 0
	safe.NextActionAt = 0
	safe.SafeUntil = at + planner.policy.Recovery.Cooldown
	safe.LastTick = at
	return planner.replaceSnapshot(safe, persist)
}

func (planner *Planner) replaceSnapshot(
	snapshot control.Snapshot,
	persist SnapshotPersister,
) (control.Snapshot, error) {
	candidate, err := control.NewMachine(planner.policy.Recovery, snapshot)
	if err != nil {
		return control.Snapshot{}, err
	}
	if err := persist(snapshot); err != nil {
		return control.Snapshot{}, err
	}
	planner.machine = candidate
	return snapshot, nil
}

// rescueRequestAllowed spaces requests out by the recovery cooldown. A path
// that stays dead would otherwise ask for a restart every cycle, and a restart
// that did not help the first time does not help sixty seconds later.
func (planner *Planner) rescueRequestAllowed(at control.Tick) bool {
	if !planner.hasRequestedRescue {
		return true
	}
	return at >= planner.lastRescueAt+planner.policy.Recovery.Cooldown
}

func (state OptionalInnerState) valid() bool {
	switch state {
	case OptionalInnerUnspecified, OptionalInnerUnknown, OptionalInnerReady, OptionalInnerFailed:
		return true
	default:
		return false
	}
}

func (planner *Planner) suspend(at control.Tick, reason Reason) (Plan, error) {
	planner.trackingFullWake = false
	planner.trackingConnecting = false
	if _, err := planner.step(at, control.EventSuspend); err != nil {
		return Plan{}, err
	}
	return planner.result(reason, ActionNone, 0), nil
}

func (planner *Planner) step(at control.Tick, event control.Event) (control.Decision, error) {
	return planner.machine.Step(planner.machine.Snapshot().Generation, at, event)
}

func (planner *Planner) result(
	reason Reason,
	action Action,
	next control.Tick,
) Plan {
	snapshot := planner.machine.Snapshot()
	return Plan{
		ObserveOnly:      true,
		State:            snapshot.State,
		Action:           action,
		Reason:           reason,
		NextEvaluationAt: next,
		Snapshot:         snapshot,
	}
}
