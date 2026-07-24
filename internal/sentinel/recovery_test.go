package sentinel

import (
	"context"
	"errors"
	"testing"
)

type fakeRootRestarter struct {
	calls int
	err   error
}

func (restarter *fakeRootRestarter) RestartHexrouted(context.Context) error {
	restarter.calls++
	return restarter.err
}

func recoveryPolicy() RecoveryPolicy {
	return RecoveryPolicy{
		VerificationWindow: 60,
		Cooldown:           1800,
	}
}

func dualFailure() Decision {
	return Decision{
		ObserveOnly:    true,
		HeartbeatStale: true,
		DataPathBroken: true,
		EvidenceReady:  true,
		Action:         ActionNone,
	}
}

func healthyEvidence() Decision {
	return Decision{
		ObserveOnly: true,
		Action:      ActionNone,
	}
}

func TestRecoveryPlannerAllowsOneRestartThenVerifiesAndCoolsDown(t *testing.T) {
	planner, _ := NewRecoveryPlanner(recoveryPolicy())
	first, err := planner.Evaluate(100, dualFailure())
	if err != nil {
		t.Fatalf("first Evaluate() error: %v", err)
	}
	if first.Action != RecoveryActionRestartRoot ||
		first.Phase != RecoveryVerifying ||
		first.VerifyDeadline != 160 {
		t.Fatalf("first Evaluate() = %+v", first)
	}

	persistent, err := planner.Evaluate(110, dualFailure())
	if err != nil {
		t.Fatalf("persistent Evaluate() error: %v", err)
	}
	if persistent.Action != RecoveryActionNone ||
		persistent.Phase != RecoveryVerifying {
		t.Fatalf("persistent Evaluate() = %+v", persistent)
	}

	verified, err := planner.Evaluate(120, healthyEvidence())
	if err != nil {
		t.Fatalf("verified Evaluate() error: %v", err)
	}
	if verified.Action != RecoveryActionNone ||
		!verified.Verified ||
		verified.Phase != RecoveryCooldown ||
		verified.CooldownUntil != 1920 {
		t.Fatalf("verified Evaluate() = %+v", verified)
	}

	duringCooldown, err := planner.Evaluate(500, dualFailure())
	if err != nil {
		t.Fatalf("cooldown Evaluate() error: %v", err)
	}
	if duringCooldown.Action != RecoveryActionNone ||
		duringCooldown.Phase != RecoveryCooldown {
		t.Fatalf("cooldown Evaluate() = %+v", duringCooldown)
	}
}

func TestRecoveryVerificationTimeoutEntersCooldownWithoutRetry(t *testing.T) {
	planner, _ := NewRecoveryPlanner(recoveryPolicy())
	_, _ = planner.Evaluate(100, dualFailure())
	timedOut, err := planner.Evaluate(160, dualFailure())
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	if timedOut.Action != RecoveryActionNone ||
		timedOut.Phase != RecoveryCooldown ||
		timedOut.CooldownUntil != 1960 {
		t.Fatalf("Evaluate() = %+v", timedOut)
	}
}

func TestObserveOnlyControllerNeverInvokesRestarter(t *testing.T) {
	planner, _ := NewRecoveryPlanner(recoveryPolicy())
	restarter := &fakeRootRestarter{}
	controller, err := NewRecoveryController(
		RecoveryObserveOnly,
		planner,
		restarter,
	)
	if err != nil {
		t.Fatalf("NewRecoveryController() error: %v", err)
	}
	result, err := controller.Handle(context.Background(), 100, dualFailure())
	if err != nil {
		t.Fatalf("Handle() error: %v", err)
	}
	if result.Attempted ||
		result.Plan.Action != RecoveryActionRestartRoot ||
		restarter.calls != 0 {
		t.Fatalf("Handle() result=%+v calls=%d", result, restarter.calls)
	}
}

func TestActiveControllerInvokesRestarterAtMostOnce(t *testing.T) {
	planner, _ := NewRecoveryPlanner(recoveryPolicy())
	restarter := &fakeRootRestarter{}
	controller, _ := NewRecoveryController(RecoveryActive, planner, restarter)

	first, err := controller.Handle(context.Background(), 100, dualFailure())
	if err != nil {
		t.Fatalf("first Handle() error: %v", err)
	}
	second, err := controller.Handle(context.Background(), 110, dualFailure())
	if err != nil {
		t.Fatalf("second Handle() error: %v", err)
	}
	if !first.Attempted ||
		first.Plan.ObserveOnly ||
		second.Attempted ||
		restarter.calls != 1 {
		t.Fatalf(
			"Handle() first=%+v second=%+v calls=%d",
			first,
			second,
			restarter.calls,
		)
	}
}

func TestFailedRestartImmediatelyEntersLongCooldown(t *testing.T) {
	planner, _ := NewRecoveryPlanner(recoveryPolicy())
	restarter := &fakeRootRestarter{err: errors.New("restart failed")}
	controller, _ := NewRecoveryController(RecoveryActive, planner, restarter)

	result, err := controller.Handle(context.Background(), 100, dualFailure())
	if err == nil {
		t.Fatal("Handle() error = nil")
	}
	if !result.Attempted ||
		result.Plan.ObserveOnly ||
		result.Plan.Phase != RecoveryCooldown ||
		result.Plan.CooldownUntil != 1900 ||
		restarter.calls != 1 {
		t.Fatalf("Handle() result=%+v calls=%d", result, restarter.calls)
	}
	next, nextErr := controller.Handle(context.Background(), 101, dualFailure())
	if nextErr != nil || next.Attempted || restarter.calls != 1 {
		t.Fatalf(
			"next Handle() result=%+v error=%v calls=%d",
			next,
			nextErr,
			restarter.calls,
		)
	}
}
