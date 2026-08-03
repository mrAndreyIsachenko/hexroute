package policy

import "time"

type ActionAuthorizationReason string

const (
	ActionAuthorized              ActionAuthorizationReason = "authorized"
	ActionInvalidRequest          ActionAuthorizationReason = "invalid_request"
	ActionInactivePolicy          ActionAuthorizationReason = "inactive_policy"
	ActionAuthorizationSuspended  ActionAuthorizationReason = "authorization_suspended"
	ActionDomainMismatch          ActionAuthorizationReason = "domain_mismatch"
	ActionGenerationMismatch      ActionAuthorizationReason = "generation_mismatch"
	ActionSelectorMismatch        ActionAuthorizationReason = "selector_mismatch"
	ActionAuthorizationLeaseEnded ActionAuthorizationReason = "authorization_lease_inactive"
	ActionExplicitlyDenied        ActionAuthorizationReason = "explicitly_denied"
)

type ActionAuthorizationState struct {
	Status                 Status
	Suspension             AuthorizationSuspension
	Payload                DomainPayload
	ControlStateGeneration uint64
}

type ActionAuthorizationRequest struct {
	Domain                 Domain
	Capability             Capability
	BundleGeneration       uint64
	DomainPolicyGeneration uint64
	ControlStateGeneration uint64
	Target                 string
	PlanSHA256             string
}

type ActionAuthorizationDecision struct {
	Allowed bool
	Reason  ActionAuthorizationReason
}

// EvaluateActionAuthorization evaluates only already-compiled effective policy.
// It has no store, process, route, network or credential side effects.
func EvaluateActionAuthorization(
	state ActionAuthorizationState,
	request ActionAuthorizationRequest,
	at time.Time,
) ActionAuthorizationDecision {
	if state.Status.Validate() != nil || state.Suspension.Validate() != nil ||
		state.ControlStateGeneration == 0 || !validAuthorizationTime(at) {
		return denyAction(ActionInvalidRequest)
	}
	if state.Status.State == PolicyDomainMismatch ||
		state.Suspension.Reason == ReasonDomainMismatch {
		return denyAction(ActionDomainMismatch)
	}
	if state.Suspension.Suspended || state.Status.State == PolicyAuthorizationSuspended {
		return denyAction(ActionAuthorizationSuspended)
	}
	if state.Status.State != PolicyActive {
		return denyAction(ActionInactivePolicy)
	}
	if state.Payload.Validate() != nil || !validActionAuthorizationRequest(request) {
		return denyAction(ActionInvalidRequest)
	}
	if state.Status.Domain != request.Domain || state.Payload.Domain != request.Domain {
		return denyAction(ActionDomainMismatch)
	}
	if state.Status.BundleGeneration != request.BundleGeneration ||
		state.Status.PolicyGeneration != request.DomainPolicyGeneration ||
		state.Payload.PolicyGeneration != request.DomainPolicyGeneration ||
		state.ControlStateGeneration != request.ControlStateGeneration {
		return denyAction(ActionGenerationMismatch)
	}

	matchingAllows := make(map[string]struct{})
	for _, rule := range state.Payload.Rules {
		if rule.Selector.Kind != SelectorAction || rule.Selector.Action == nil ||
			rule.Selector.Action.Capability != request.Capability ||
			rule.Selector.Action.Target != request.Target {
			continue
		}
		if rule.Effect == EffectDeny {
			return denyAction(ActionExplicitlyDenied)
		}
		matchingAllows[rule.Selector.ID] = struct{}{}
	}
	if len(matchingAllows) == 0 {
		return denyAction(ActionSelectorMismatch)
	}
	for _, lease := range state.Payload.Leases {
		if lease.Domain != request.Domain || lease.Capability != request.Capability ||
			!authorizationLeaseActive(lease, at) {
			continue
		}
		for _, selectorID := range lease.SelectorIDs {
			if _, matches := matchingAllows[selectorID]; matches {
				return ActionAuthorizationDecision{Allowed: true, Reason: ActionAuthorized}
			}
		}
	}
	return denyAction(ActionAuthorizationLeaseEnded)
}

func validActionAuthorizationRequest(request ActionAuthorizationRequest) bool {
	return request.Domain.Valid() && request.Capability.Valid() &&
		request.BundleGeneration > 0 && request.DomainPolicyGeneration > 0 &&
		request.ControlStateGeneration > 0 && validTarget(request.Target) &&
		validSHA256(request.PlanSHA256)
}

func validAuthorizationTime(value time.Time) bool {
	return !value.IsZero() && value.UTC().Year() >= 1 && value.UTC().Year() <= 9999
}

func authorizationLeaseActive(lease AuthorizationLease, at time.Time) bool {
	issuedAt, issuedOK := parseCanonicalUTC(lease.IssuedAt)
	expiresAt, expiresOK := parseCanonicalUTC(lease.ExpiresAt)
	if !issuedOK || !expiresOK {
		return false
	}
	at = at.UTC()
	return !at.Before(issuedAt) && at.Before(expiresAt)
}

func denyAction(reason ActionAuthorizationReason) ActionAuthorizationDecision {
	return ActionAuthorizationDecision{Reason: reason}
}
