package replay

import (
	"errors"
	"fmt"
)

var ErrDecisionDiverged = errors.New("root decision diverged from approved trace")

type RootPlanner struct {
	processExited       bool
	ingressThresholdHit bool
}

func (planner *RootPlanner) Step(event Event) (Action, error) {
	switch {
	case event.Component == ComponentSingBox && event.Reason == "process_exited":
		planner.processExited = true
	case event.Reason == "ingress_failure_threshold":
		planner.ingressThresholdHit = true
	case event.Reason == "outer_tls_ready":
		planner.processExited = false
		planner.ingressThresholdHit = false
	}

	switch {
	case event.Component == ComponentUser || event.Component == ComponentPritunl:
		return ActionNone, nil
	case event.State == StateSuspended &&
		(event.Reason == "dark_wake" || event.Reason == "physical_network_unavailable"):
		return ActionSkipProbe, nil
	case event.Component == ComponentRoute &&
		event.Kind == KindDecision &&
		event.State == StateRecovering &&
		event.Reason == "scoped_routes_required":
		return ActionApplyScopedRoutes, nil
	case event.Component == ComponentRoot &&
		event.Kind == KindDecision &&
		event.State == StateRecovering &&
		event.Reason == "alternate_ingress_ready":
		if !planner.ingressThresholdHit {
			return ActionNone, ErrDecisionDiverged
		}
		return ActionSelectNextIngress, nil
	case event.Component == ComponentRoot &&
		event.Kind == KindDecision &&
		event.State == StateRecovering &&
		event.Reason == "process_exit_recovery_allowed":
		if !planner.processExited {
			return ActionNone, ErrDecisionDiverged
		}
		return ActionRestartSingBox, nil
	default:
		return ActionNone, nil
	}
}

func CompareRoot(trace Trace) error {
	planner := &RootPlanner{}
	for _, event := range trace.Events {
		proposed, err := planner.Step(event)
		if err != nil {
			return fmt.Errorf("%w: trace=%s seq=%d", err, trace.Name, event.Sequence)
		}

		approved := event.Action
		if approved == ActionReconnectPritunl {
			if proposed != ActionNone {
				return fmt.Errorf(
					"%w: root captured user action trace=%s seq=%d",
					ErrDecisionDiverged,
					trace.Name,
					event.Sequence,
				)
			}
			continue
		}
		if proposed != approved {
			return fmt.Errorf(
				"%w: trace=%s seq=%d proposed=%s approved=%s",
				ErrDecisionDiverged,
				trace.Name,
				event.Sequence,
				proposed,
				approved,
			)
		}
	}
	return nil
}
