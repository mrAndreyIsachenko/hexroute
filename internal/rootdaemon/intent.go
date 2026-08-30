package rootdaemon

import (
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivity"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityhost"
	"github.com/mrAndreyIsachenko/hexroute/internal/routeplan"
)

// plannerIntents reduces the route planner's operations to what a comparison
// needs: which component, and in which direction.
//
// The projection happens here because this package holds the plan and the
// comparison must not. A recorder that could reach a route plan would be a
// recorder that could carry one to something able to apply it.
//
// Every route operation is about scoped routes. The route planner has no
// opinion about DNS, transports, relays or the user domain, and saying nothing
// about them is the truthful projection — a comparison that read absence as
// agreement would report a consensus neither planner reached.
func plannerIntents(plan routeplan.Plan) []connectivityhost.PlannerIntent {
	if len(plan.Operations) == 0 {
		return nil
	}
	intent := connectivityhost.PlannerIntent{
		Component: connectivity.ComponentScopedRoutes,
	}
	for _, operation := range plan.Operations {
		switch operation.Kind {
		case routeplan.OperationEnsureHostRoute:
			intent.Establish = true
		case routeplan.OperationRemoveOwnedHostRoute:
			intent.Withdraw = true
		}
	}
	if !intent.Establish && !intent.Withdraw {
		return nil
	}
	return []connectivityhost.PlannerIntent{intent}
}
