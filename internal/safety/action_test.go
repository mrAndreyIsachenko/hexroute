package safety

import (
	"errors"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
)

func TestValidateActionRejectsEveryAdGuardMutation(t *testing.T) {
	for _, kind := range []control.ActionKind{
		control.ActionRestart,
		control.ActionKind("stop"),
		control.ActionKind("disable"),
		control.ActionKind("reconfigure"),
	} {
		t.Run(string(kind), func(t *testing.T) {
			err := ValidateAction(control.Action{
				Kind:   kind,
				Target: control.ActionTarget("adguard"),
			})
			if !errors.Is(err, ErrForbiddenTarget) {
				t.Fatalf("ValidateAction() error = %v, want %v", err, ErrForbiddenTarget)
			}
		})
	}
}

func TestValidateActionRejectsArbitraryAction(t *testing.T) {
	err := ValidateAction(control.Action{
		Kind:   control.ActionKind("run_shell"),
		Target: control.TargetSingBox,
	})
	if !errors.Is(err, ErrUnknownAction) {
		t.Fatalf("ValidateAction() error = %v, want %v", err, ErrUnknownAction)
	}
}

func TestValidateActionAllowsKnownScopedActions(t *testing.T) {
	for key := range allowedActions {
		action := control.Action{
			Kind:       key.kind,
			Target:     key.target,
			Generation: 42,
			Reason:     control.ReasonProbeFailed,
		}
		if err := ValidateAction(action); err != nil {
			t.Fatalf("ValidateAction(%+v) unexpected error: %v", action, err)
		}
	}
}
