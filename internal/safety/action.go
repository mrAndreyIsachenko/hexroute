package safety

import (
	"errors"
	"fmt"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
)

var (
	ErrForbiddenTarget = errors.New("action target is forbidden")
	ErrUnknownAction   = errors.New("action is not allowlisted")
)

type actionKey struct {
	kind   control.ActionKind
	target control.ActionTarget
}

var allowedActions = map[actionKey]struct{}{
	{kind: control.ActionRestart, target: control.TargetSingBox}:                {},
	{kind: control.ActionApplyScopedRoutes, target: control.TargetScopedRoutes}: {},
	{kind: control.ActionSelectIngress, target: control.TargetIngress}:          {},
	{kind: control.ActionRestart, target: control.TargetPritunlService}:         {},
}

func ValidateAction(action control.Action) error {
	if action.Target == control.ActionTarget("adguard") {
		return ErrForbiddenTarget
	}
	if _, ok := allowedActions[actionKey{kind: action.Kind, target: action.Target}]; !ok {
		return fmt.Errorf("%w: kind=%q target=%q", ErrUnknownAction, action.Kind, action.Target)
	}
	return nil
}
