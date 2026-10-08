// Package routeplan turns a decision about where routes belong into the
// operations that would put them there.
//
// Where a route belongs is internal/routeplace's answer, not this package's.
// This one can change the host, so the boundary guard in
// internal/connectivityreduce forbids anything holding a reconciliation
// proposal from importing it — and the read model holds proposals. Both read
// the decision from routeplace, so the plan and the scoped-routes fact cannot
// disagree about what the host is doing.
package routeplan

import (
	"net/netip"
	"sort"

	"github.com/mrAndreyIsachenko/hexroute/internal/routeplace"
	"github.com/mrAndreyIsachenko/hexroute/internal/safety"
)

// The decision's vocabulary, so this package's callers need not name two.
type (
	Role                 = routeplace.Role
	Target               = routeplace.Target
	Path                 = routeplace.Path
	ObservedRoute        = routeplace.ObservedRoute
	CodexState           = routeplace.CodexState
	Input                = routeplace.Input
	GitLabSSHObservation = routeplace.GitLabSSHObservation
	PolicyStatus         = routeplace.PolicyStatus
	Placement            = routeplace.Placement
)

const (
	RoleIngress       = routeplace.RoleIngress
	RoleCorporate     = routeplace.RoleCorporate
	RoleGitLabHTTPS   = routeplace.RoleGitLabHTTPS
	RoleCodexFallback = routeplace.RoleCodexFallback
	RoleInherited     = routeplace.RoleInherited

	PolicyUnknown   = routeplace.PolicyUnknown
	PolicyCompliant = routeplace.PolicyCompliant
	PolicyDegraded  = routeplace.PolicyDegraded
)

var (
	ErrInvalidInput       = routeplace.ErrInvalidInput
	ErrAmbiguousOwnership = routeplace.ErrAmbiguousOwnership
)

// EvaluateGitLabSSH is the decision's, re-exported where its one caller is.
func EvaluateGitLabSSH(observation GitLabSSHObservation) (PolicyStatus, error) {
	return routeplace.EvaluateGitLabSSH(observation)
}

type OperationKind string

const (
	OperationEnsureHostRoute      OperationKind = "ensure_host_route"
	OperationRemoveOwnedHostRoute OperationKind = "remove_owned_host_route"
)

type Reason string

const (
	ReasonMissingRoute     Reason = "missing_route"
	ReasonWrongPath        Reason = "wrong_path"
	ReasonFallbackRequired Reason = "fallback_required"
	ReasonFallbackRestored Reason = "fallback_restored"
)

type Operation struct {
	Kind        OperationKind
	Target      string
	Role        Role
	Destination netip.Addr
	Path        Path
	Reason      Reason
}

type Plan struct {
	ObserveOnly bool
	Operations  []Operation
	GitLabSSH   PolicyStatus
}

// Build proposes the operations that would put every route where its role asks
// for it. It performs none of them.
func Build(input Input) (Plan, error) {
	decisions, err := routeplace.Decide(input)
	if err != nil {
		return Plan{}, err
	}

	safetyPlan := safety.RoutePlan{}
	for _, decision := range decisions {
		if !decision.Active {
			continue
		}
		safetyRole := safety.RouteScoped
		if decision.Target.Role == RoleIngress {
			safetyRole = safety.RouteIngress
		}
		safetyPlan.Routes = append(safetyPlan.Routes, safety.Route{
			Role: safetyRole,
			Link: decision.Path.Link,
		})
	}
	if err := safety.ValidateRoutePlan(safetyPlan); err != nil {
		return Plan{}, err
	}

	plan := Plan{
		ObserveOnly: true,
		GitLabSSH:   PolicyUnknown,
	}
	if input.GitLabSSH != nil {
		status, err := EvaluateGitLabSSH(*input.GitLabSSH)
		if err != nil {
			return Plan{}, err
		}
		plan.GitLabSSH = status
	}

	// What to do about a state, and what to call it, is this package's. The
	// state itself is routeplace's, and the read model counts the same ones.
	for _, decision := range decisions {
		switch decision.State {
		case routeplace.StateUnasked, routeplace.StateInstalled:
			continue
		case routeplace.StateUnwanted:
			plan.Operations = append(plan.Operations, Operation{
				Kind:        OperationRemoveOwnedHostRoute,
				Target:      decision.Target.Name,
				Role:        decision.Target.Role,
				Destination: decision.Target.Destination,
				Reason:      ReasonFallbackRestored,
			})
		case routeplace.StateConflicting, routeplace.StateMissing:
			// A fallback route names its own cause either way: the reason it is
			// wanted is the fallback, not where it happens to be standing.
			reason := ReasonMissingRoute
			if decision.Present {
				reason = ReasonWrongPath
			}
			if decision.Target.Role == RoleCodexFallback {
				reason = ReasonFallbackRequired
			}
			plan.Operations = append(plan.Operations, Operation{
				Kind:        OperationEnsureHostRoute,
				Target:      decision.Target.Name,
				Role:        decision.Target.Role,
				Destination: decision.Target.Destination,
				Path:        decision.Path,
				Reason:      reason,
			})
		}
	}

	sort.Slice(plan.Operations, func(left, right int) bool {
		leftAddress := plan.Operations[left].Destination.String()
		rightAddress := plan.Operations[right].Destination.String()
		if leftAddress != rightAddress {
			return leftAddress < rightAddress
		}
		return plan.Operations[left].Target < plan.Operations[right].Target
	})
	return plan, nil
}
