package policycontrol

import (
	"context"
	"crypto/rand"
	"errors"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/actionlease"
	"github.com/mrAndreyIsachenko/hexroute/internal/actionplan"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/resumeplan"
)

type actionLeaseStore interface {
	actionlease.Store
	actionlease.ExecutionStore
}

type fixedAuthorizationClock struct {
	wall      time.Time
	monotonic time.Duration
}

func (clock fixedAuthorizationClock) WallNow() time.Time          { return clock.wall }
func (clock fixedAuthorizationClock) MonotonicNow() time.Duration { return clock.monotonic }

type operatorResumeAuthorizationSource struct {
	handler                *Handler
	domain                 policy.Domain
	target                 string
	planSHA256             string
	controlStateGeneration uint64
	bootID                 metadata.UUID
}

var ErrOperatorResumeUnauthorized = errors.New("operator resume is not authorized")

// AuthorizeOperatorResume issues a durable one-time lease for one exact,
// already-built state-only resume plan. It grants no process or network
// capability.
func (handler *Handler) AuthorizeOperatorResume(
	plan resumeplan.Plan,
	bootID metadata.UUID,
) (actionplan.LeaseGuard, actionplan.AuthorizationSource, error) {
	if handler == nil || plan.Digest() == "" || plan.ActionPlan().Target() == "" ||
		plan.Before().Generation == 0 {
		return nil, nil, ErrOperatorResumeUnauthorized
	}
	if _, err := metadata.ParseUUID(string(bootID)); err != nil {
		return nil, nil, ErrOperatorResumeUnauthorized
	}
	handler.mu.Lock()
	defer handler.mu.Unlock()
	store, ok := handler.store.(actionLeaseStore)
	if !ok {
		return nil, nil, ErrOperatorResumeUnauthorized
	}
	handler.refreshAuthorizationLocked()
	now, err := handler.checkedNowLocked()
	if err != nil {
		return nil, nil, ErrOperatorResumeUnauthorized
	}
	monotonic := handler.monotonicNow()
	request := handler.operatorResumeRequestLocked(
		plan.ActionPlan().Target(),
		plan.Before().Generation,
		plan.Digest(),
	)
	decision := handler.evaluateActionLocked(request, now)
	if !decision.Allowed {
		return nil, nil, ErrOperatorResumeUnauthorized
	}
	lease, err := actionlease.IssueAndPersist(
		store,
		actionlease.IssueInput{
			Domain: handler.domain, Capability: policy.CapabilityOperatorResume,
			BundleGeneration:       request.BundleGeneration,
			DomainPolicyGeneration: request.DomainPolicyGeneration,
			ControlStateGeneration: request.ControlStateGeneration,
			Target:                 request.Target, PlanSHA256: request.PlanSHA256,
			BootID: bootID,
		},
		fixedAuthorizationClock{wall: now, monotonic: monotonic},
		rand.Reader,
	)
	if err != nil {
		return nil, nil, err
	}
	guard, err := actionlease.NewGuard(store, lease.ActionID)
	if err != nil {
		return nil, nil, err
	}
	source := &operatorResumeAuthorizationSource{
		handler: handler, domain: handler.domain,
		target: request.Target, planSHA256: request.PlanSHA256,
		controlStateGeneration: request.ControlStateGeneration,
		bootID:                 bootID,
	}
	return guard, source, nil
}

func (source *operatorResumeAuthorizationSource) Current(
	ctx context.Context,
) (actionlease.CurrentAuthorization, error) {
	if source == nil || source.handler == nil || ctx == nil || ctx.Err() != nil {
		return actionlease.CurrentAuthorization{}, ErrOperatorResumeUnauthorized
	}
	handler := source.handler
	handler.mu.Lock()
	defer handler.mu.Unlock()
	handler.refreshAuthorizationLocked()
	now, err := handler.checkedNowLocked()
	if err != nil {
		return actionlease.CurrentAuthorization{}, ErrOperatorResumeUnauthorized
	}
	request := handler.operatorResumeRequestLocked(
		source.target,
		source.controlStateGeneration,
		source.planSHA256,
	)
	if source.domain != handler.domain || !handler.evaluateActionLocked(request, now).Allowed {
		return actionlease.CurrentAuthorization{}, ErrOperatorResumeUnauthorized
	}
	return actionlease.CurrentAuthorization{
		Domain: handler.domain, Capability: policy.CapabilityOperatorResume,
		BundleGeneration:       request.BundleGeneration,
		DomainPolicyGeneration: request.DomainPolicyGeneration,
		ControlStateGeneration: source.controlStateGeneration,
		Target:                 source.target, PlanSHA256: source.planSHA256,
		BootID: source.bootID, MonotonicNS: handler.monotonicNow().Nanoseconds(),
		ObservedAt: now,
	}, nil
}

func (handler *Handler) operatorResumeRequestLocked(
	target string,
	controlStateGeneration uint64,
	planSHA256 string,
) policy.ActionAuthorizationRequest {
	return policy.ActionAuthorizationRequest{
		Domain: handler.domain, Capability: policy.CapabilityOperatorResume,
		BundleGeneration:       handler.status.BundleGeneration,
		DomainPolicyGeneration: handler.status.PolicyGeneration,
		ControlStateGeneration: controlStateGeneration,
		Target:                 target, PlanSHA256: planSHA256,
	}
}

func (handler *Handler) evaluateActionLocked(
	request policy.ActionAuthorizationRequest,
	at time.Time,
) policy.ActionAuthorizationDecision {
	return policy.EvaluateActionAuthorization(
		policy.ActionAuthorizationState{
			Status: handler.status, Suspension: handler.authorizationSuspension,
			Payload:                handler.activePayload,
			ControlStateGeneration: request.ControlStateGeneration,
		},
		request,
		at,
	)
}
