package control

import (
	"errors"
	"fmt"
)

const SnapshotSchemaVersion = 1

type Tick int64

type Event string

const (
	EventSuspend           Event = "suspend"
	EventDependenciesReady Event = "dependencies_ready"
	EventProbeSucceeded    Event = "probe_succeeded"
	EventProbeFailed       Event = "probe_failed"
	EventBeginRecovery     Event = "begin_recovery"
	EventCooldownElapsed   Event = "cooldown_elapsed"
	EventOperatorResume    Event = "operator_resume"
)

type Policy struct {
	FailureThreshold   uint32 `json:"failure_threshold"`
	ActionBudget       uint32 `json:"action_budget"`
	BaseBackoff        Tick   `json:"base_backoff"`
	MaxBackoff         Tick   `json:"max_backoff"`
	VerificationWindow Tick   `json:"verification_window"`
	Cooldown           Tick   `json:"cooldown"`
}

type Snapshot struct {
	SchemaVersion       uint32 `json:"schema_version"`
	Generation          uint64 `json:"generation"`
	State               State  `json:"state"`
	ConsecutiveFailures uint32 `json:"consecutive_failures"`
	Attempts            uint32 `json:"attempts"`
	LastTick            Tick   `json:"last_tick"`
	RecoveringSince     Tick   `json:"recovering_since"`
	NextActionAt        Tick   `json:"next_action_at"`
	SafeUntil           Tick   `json:"safe_until"`
}

type Decision struct {
	From             State  `json:"from"`
	To               State  `json:"to"`
	Generation       uint64 `json:"generation"`
	Reason           Reason `json:"reason"`
	RecoveryApproved bool   `json:"recovery_approved"`
}

type Machine struct {
	policy   Policy
	snapshot Snapshot
}

var (
	ErrInvalidPolicy      = errors.New("invalid recovery policy")
	ErrInvalidSnapshot    = errors.New("invalid state snapshot")
	ErrStaleGeneration    = errors.New("stale state generation")
	ErrNonMonotonicTick   = errors.New("non-monotonic event tick")
	ErrInvalidEvent       = errors.New("invalid state event")
	ErrResumePrecondition = errors.New("state is not in safe mode")
)

func NewSnapshot(state State) Snapshot {
	return Snapshot{
		SchemaVersion: SnapshotSchemaVersion,
		State:         state,
	}
}

func NewMachine(policy Policy, snapshot Snapshot) (*Machine, error) {
	if err := validatePolicy(policy); err != nil {
		return nil, err
	}
	if err := validateSnapshot(snapshot); err != nil {
		return nil, err
	}
	return &Machine{
		policy:   policy,
		snapshot: snapshot,
	}, nil
}

func validateSnapshot(snapshot Snapshot) error {
	if snapshot.SchemaVersion != SnapshotSchemaVersion ||
		!snapshot.State.Valid() ||
		snapshot.LastTick < 0 ||
		snapshot.RecoveringSince < 0 ||
		snapshot.NextActionAt < 0 ||
		snapshot.SafeUntil < 0 {
		return ErrInvalidSnapshot
	}
	return nil
}

func (machine *Machine) Snapshot() Snapshot {
	return machine.snapshot
}

func (machine *Machine) Step(expectedGeneration uint64, at Tick, event Event) (Decision, error) {
	if expectedGeneration != machine.snapshot.Generation {
		return Decision{}, ErrStaleGeneration
	}
	if at < machine.snapshot.LastTick {
		return Decision{}, ErrNonMonotonicTick
	}

	before := machine.snapshot
	decision := Decision{
		From:   before.State,
		To:     before.State,
		Reason: ReasonNone,
	}

	switch event {
	case EventSuspend:
		machine.snapshot.State = StateSuspended
		machine.snapshot.ConsecutiveFailures = 0
		machine.snapshot.RecoveringSince = 0
		decision.Reason = ReasonIntentionalSleep

	case EventDependenciesReady:
		if machine.snapshot.State == StateSuspended {
			machine.resetHealthy()
			decision.Reason = ReasonDependenciesReady
		}

	case EventProbeSucceeded:
		switch machine.snapshot.State {
		case StateHealthy:
			machine.snapshot.ConsecutiveFailures = 0
			decision.Reason = ReasonProbeSucceeded
		case StateDegraded:
			machine.resetHealthy()
			decision.Reason = ReasonProbeSucceeded
		case StateRecovering:
			if at-machine.snapshot.RecoveringSince >= machine.policy.VerificationWindow {
				machine.resetHealthy()
				decision.Reason = ReasonVerificationPassed
			}
		case StateSuspended, StateSafeMode:
			// Success while suspended or in SAFE_MODE cannot silently re-enable mutations.
		}

	case EventProbeFailed:
		switch machine.snapshot.State {
		case StateSuspended, StateSafeMode:
			// Probe failures in these states do not consume thresholds or budgets.
		case StateHealthy:
			machine.snapshot.ConsecutiveFailures++
			decision.Reason = ReasonProbeFailed
			if machine.snapshot.ConsecutiveFailures >= machine.policy.FailureThreshold {
				machine.snapshot.State = StateDegraded
				machine.snapshot.NextActionAt = at
				decision.Reason = ReasonFailureThreshold
			}
		case StateDegraded:
			machine.snapshot.ConsecutiveFailures++
			decision.Reason = ReasonProbeFailed
		case StateRecovering:
			machine.snapshot.ConsecutiveFailures++
			machine.snapshot.RecoveringSince = 0
			if machine.snapshot.Attempts >= machine.policy.ActionBudget {
				machine.snapshot.State = StateSafeMode
				machine.snapshot.SafeUntil = at + machine.policy.Cooldown
				decision.Reason = ReasonRecoveryBudget
			} else {
				machine.snapshot.State = StateDegraded
				decision.Reason = ReasonProbeFailed
			}
		}

	case EventBeginRecovery:
		if machine.snapshot.State == StateDegraded && at >= machine.snapshot.NextActionAt {
			if machine.snapshot.Attempts >= machine.policy.ActionBudget {
				machine.snapshot.State = StateSafeMode
				machine.snapshot.SafeUntil = at + machine.policy.Cooldown
				decision.Reason = ReasonRecoveryBudget
			} else {
				machine.snapshot.Attempts++
				machine.snapshot.State = StateRecovering
				machine.snapshot.RecoveringSince = at
				machine.snapshot.NextActionAt = at + machine.backoff(machine.snapshot.Attempts)
				decision.RecoveryApproved = true
				decision.Reason = ReasonRecoveryAllowed
			}
		}

	case EventCooldownElapsed:
		if machine.snapshot.State == StateSafeMode && at >= machine.snapshot.SafeUntil {
			machine.snapshot.State = StateDegraded
			machine.snapshot.Attempts = 0
			machine.snapshot.NextActionAt = at
			machine.snapshot.SafeUntil = 0
			decision.Reason = ReasonCooldownElapsed
		}

	case EventOperatorResume:
		if machine.snapshot.State != StateSafeMode {
			return Decision{}, ErrResumePrecondition
		}
		machine.snapshot.State = StateDegraded
		machine.snapshot.Attempts = 0
		machine.snapshot.RecoveringSince = 0
		machine.snapshot.NextActionAt = at
		machine.snapshot.SafeUntil = 0
		decision.Reason = ReasonOperatorResume

	default:
		return Decision{}, fmt.Errorf("%w: %q", ErrInvalidEvent, event)
	}

	machine.snapshot.LastTick = at
	if machine.snapshot != before {
		machine.snapshot.Generation++
	}

	decision.To = machine.snapshot.State
	decision.Generation = machine.snapshot.Generation
	return decision, nil
}

func (machine *Machine) resetHealthy() {
	machine.snapshot.State = StateHealthy
	machine.snapshot.ConsecutiveFailures = 0
	machine.snapshot.Attempts = 0
	machine.snapshot.RecoveringSince = 0
	machine.snapshot.NextActionAt = 0
	machine.snapshot.SafeUntil = 0
}

func (machine *Machine) backoff(attempt uint32) Tick {
	backoff := machine.policy.BaseBackoff
	for current := uint32(1); current < attempt; current++ {
		if backoff >= machine.policy.MaxBackoff/2 {
			return machine.policy.MaxBackoff
		}
		backoff *= 2
	}
	if backoff > machine.policy.MaxBackoff {
		return machine.policy.MaxBackoff
	}
	return backoff
}

func validatePolicy(policy Policy) error {
	if policy.FailureThreshold == 0 ||
		policy.ActionBudget == 0 ||
		policy.BaseBackoff <= 0 ||
		policy.MaxBackoff < policy.BaseBackoff ||
		policy.VerificationWindow < 0 ||
		policy.Cooldown <= 0 {
		return ErrInvalidPolicy
	}
	return nil
}
