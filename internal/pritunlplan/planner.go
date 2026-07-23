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
)

type Policy struct {
	Recovery        control.Policy
	WakeSettle      control.Tick
	ConnectingGrace control.Tick
	OTPPeriod       uint32
	OTPMinValid     uint32
}

type Observation struct {
	At                  control.Tick
	Session             userobserve.SessionState
	Wake                userobserve.WakeObservation
	OuterReady          bool
	Profile             userobserve.ProfileObservation
	OTPSecondsRemaining uint32
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
}

var ErrInvalidInput = errors.New("invalid Pritunl planner input")

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

func (planner *Planner) Plan(observation Observation) (Plan, error) {
	if planner == nil ||
		planner.machine == nil ||
		observation.At < 0 ||
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
