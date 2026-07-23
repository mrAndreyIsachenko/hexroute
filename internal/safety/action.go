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

var allowedActions = map[control.Action]struct{}{
	{Kind: control.ActionRestart, Target: control.TargetSingBox}:                {},
	{Kind: control.ActionApplyScopedRoutes, Target: control.TargetScopedRoutes}: {},
	{Kind: control.ActionSelectIngress, Target: control.TargetIngress}:          {},
	{Kind: control.ActionRestart, Target: control.TargetPritunlService}:         {},
}

func ValidateAction(action control.Action) error {
	if action.Target == control.ActionTarget("adguard") {
		return ErrForbiddenTarget
	}
	if _, ok := allowedActions[action]; !ok {
		return fmt.Errorf("%w: kind=%q target=%q", ErrUnknownAction, action.Kind, action.Target)
	}
	return nil
}
