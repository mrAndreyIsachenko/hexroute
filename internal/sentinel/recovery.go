package sentinel

import (
	"context"
	"errors"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
)

type RecoveryAction string

const (
	RecoveryActionNone        RecoveryAction = "none"
	RecoveryActionRestartRoot RecoveryAction = "restart_hexrouted"
)

type RecoveryPhase string

const (
	RecoveryMonitoring RecoveryPhase = "monitoring"
	RecoveryVerifying  RecoveryPhase = "verifying"
	RecoveryCooldown   RecoveryPhase = "cooldown"
)

type RecoveryAuthority string

const (
	RecoveryObserveOnly RecoveryAuthority = "observe-only"
	RecoveryActive      RecoveryAuthority = "active"
)

type RecoveryPolicy struct {
	VerificationWindow control.Tick
	Cooldown           control.Tick
}

type RecoveryPlan struct {
	ObserveOnly    bool
	Phase          RecoveryPhase
	Action         RecoveryAction
	Verified       bool
	VerifyDeadline control.Tick
	CooldownUntil  control.Tick
}

type RecoveryPlanner struct {
	policy            RecoveryPolicy
	phase             RecoveryPhase
	verifyDeadline    control.Tick
	cooldownUntil     control.Tick
	lastObservationAt control.Tick
	initialized       bool
}

type RootRestarter interface {
	RestartHexrouted(context.Context) error
}

type RecoveryResult struct {
	Plan      RecoveryPlan
	Attempted bool
}

type RecoveryController struct {
	authority RecoveryAuthority
	planner   *RecoveryPlanner
	restarter RootRestarter
}

var ErrInvalidRecovery = errors.New("invalid sentinel recovery input")

func NewRecoveryPlanner(policy RecoveryPolicy) (*RecoveryPlanner, error) {
	if policy.VerificationWindow <= 0 ||
		policy.VerificationWindow > 300 ||
		policy.Cooldown < 600 ||
		policy.Cooldown > 86400 {
		return nil, ErrInvalidRecovery
	}
	return &RecoveryPlanner{
		policy: policy,
		phase:  RecoveryMonitoring,
	}, nil
}

func NewRecoveryController(
	authority RecoveryAuthority,
	planner *RecoveryPlanner,
	restarter RootRestarter,
) (*RecoveryController, error) {
	if planner == nil ||
		(authority != RecoveryObserveOnly && authority != RecoveryActive) ||
		(authority == RecoveryActive && restarter == nil) {
		return nil, ErrInvalidRecovery
	}
	return &RecoveryController{
		authority: authority,
		planner:   planner,
		restarter: restarter,
	}, nil
}

func (planner *RecoveryPlanner) Evaluate(
	at control.Tick,
	evidence Decision,
) (RecoveryPlan, error) {
	if planner == nil ||
		at < 0 ||
		evidence.Action != ActionNone ||
		!evidence.ObserveOnly ||
		evidence.EvidenceReady != (evidence.HeartbeatStale && evidence.DataPathBroken) ||
		(planner.initialized && at < planner.lastObservationAt) {
		return RecoveryPlan{}, ErrInvalidRecovery
	}
	planner.initialized = true
	planner.lastObservationAt = at

	if planner.phase == RecoveryCooldown && at >= planner.cooldownUntil {
		planner.phase = RecoveryMonitoring
		planner.cooldownUntil = 0
	}
	switch planner.phase {
	case RecoveryMonitoring:
		if evidence.EvidenceReady {
			planner.phase = RecoveryVerifying
			planner.verifyDeadline = at + planner.policy.VerificationWindow
			return planner.result(RecoveryActionRestartRoot, false), nil
		}
	case RecoveryVerifying:
		if !evidence.HeartbeatStale && !evidence.DataPathBroken {
			planner.enterCooldown(at)
			return planner.result(RecoveryActionNone, true), nil
		}
		if at >= planner.verifyDeadline {
			planner.enterCooldown(at)
		}
	case RecoveryCooldown:
	default:
		return RecoveryPlan{}, ErrInvalidRecovery
	}
	return planner.result(RecoveryActionNone, false), nil
}

func (planner *RecoveryPlanner) RestartFailed(at control.Tick) (RecoveryPlan, error) {
	if planner == nil ||
		planner.phase != RecoveryVerifying ||
		at < planner.lastObservationAt {
		return RecoveryPlan{}, ErrInvalidRecovery
	}
	planner.lastObservationAt = at
	planner.enterCooldown(at)
	return planner.result(RecoveryActionNone, false), nil
}

func (planner *RecoveryPlanner) enterCooldown(at control.Tick) {
	planner.phase = RecoveryCooldown
	planner.verifyDeadline = 0
	planner.cooldownUntil = at + planner.policy.Cooldown
}

func (planner *RecoveryPlanner) result(
	action RecoveryAction,
	verified bool,
) RecoveryPlan {
	return RecoveryPlan{
		ObserveOnly:    true,
		Phase:          planner.phase,
		Action:         action,
		Verified:       verified,
		VerifyDeadline: planner.verifyDeadline,
		CooldownUntil:  planner.cooldownUntil,
	}
}

func (controller *RecoveryController) Handle(
	ctx context.Context,
	at control.Tick,
	evidence Decision,
) (RecoveryResult, error) {
	if controller == nil || ctx == nil || controller.planner == nil {
		return RecoveryResult{}, ErrInvalidRecovery
	}
	plan, err := controller.planner.Evaluate(at, evidence)
	if err != nil {
		return RecoveryResult{}, err
	}
	plan.ObserveOnly = controller.authority == RecoveryObserveOnly
	result := RecoveryResult{Plan: plan}
	if plan.Action != RecoveryActionRestartRoot ||
		controller.authority == RecoveryObserveOnly {
		return result, nil
	}
	result.Attempted = true
	if err := controller.restarter.RestartHexrouted(ctx); err != nil {
		cooldown, transitionErr := controller.planner.RestartFailed(at)
		cooldown.ObserveOnly = false
		result.Plan = cooldown
		if transitionErr != nil {
			return result, transitionErr
		}
		return result, err
	}
	return result, nil
}
