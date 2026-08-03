package policy_test

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/actionlease"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

const (
	testBootID metadata.UUID = "123e4567-e89b-42d3-a456-426614174000"
	testTarget               = "synthetic-target"
)

func TestEvaluateOperatorResumeRequiresExactPolicyBinding(t *testing.T) {
	at := time.Date(2030, time.January, 1, 1, 30, 0, 0, time.UTC)
	state, request := authorizedResumeFixture()

	tests := []struct {
		name   string
		mutate func(*policy.ActionAuthorizationState, *policy.ActionAuthorizationRequest)
		reason policy.ActionAuthorizationReason
	}{
		{name: "exact match", reason: policy.ActionAuthorized},
		{
			name: "target mismatch",
			mutate: func(_ *policy.ActionAuthorizationState, request *policy.ActionAuthorizationRequest) {
				request.Target = "synthetic-other"
			},
			reason: policy.ActionSelectorMismatch,
		},
		{
			name: "wrong privilege domain",
			mutate: func(_ *policy.ActionAuthorizationState, request *policy.ActionAuthorizationRequest) {
				request.Domain = policy.DomainRoot
			},
			reason: policy.ActionDomainMismatch,
		},
		{
			name: "bundle generation mismatch",
			mutate: func(_ *policy.ActionAuthorizationState, request *policy.ActionAuthorizationRequest) {
				request.BundleGeneration--
			},
			reason: policy.ActionGenerationMismatch,
		},
		{
			name: "domain policy generation mismatch",
			mutate: func(_ *policy.ActionAuthorizationState, request *policy.ActionAuthorizationRequest) {
				request.DomainPolicyGeneration--
			},
			reason: policy.ActionGenerationMismatch,
		},
		{
			name: "control state generation mismatch",
			mutate: func(_ *policy.ActionAuthorizationState, request *policy.ActionAuthorizationRequest) {
				request.ControlStateGeneration--
			},
			reason: policy.ActionGenerationMismatch,
		},
		{
			name: "cross-domain activation mismatch",
			mutate: func(state *policy.ActionAuthorizationState, _ *policy.ActionAuthorizationRequest) {
				state.Status.State = policy.PolicyDomainMismatch
				state.Status.Reason = policy.ReasonDomainMismatch
			},
			reason: policy.ActionDomainMismatch,
		},
		{
			name: "no active policy",
			mutate: func(state *policy.ActionAuthorizationState, _ *policy.ActionAuthorizationRequest) {
				state.Status = policy.Status{
					Schema: policy.PolicyStatusSchema, Domain: policy.DomainUser,
					State: policy.PolicyNone, Reason: policy.ReasonNoValidGeneration,
				}
				state.Payload = policy.DomainPayload{}
			},
			reason: policy.ActionInactivePolicy,
		},
		{
			name: "authorization lease expired",
			mutate: func(state *policy.ActionAuthorizationState, _ *policy.ActionAuthorizationRequest) {
				state.Payload.Leases[0].ExpiresAt = at.Format(time.RFC3339Nano)
			},
			reason: policy.ActionAuthorizationLeaseEnded,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidateState := cloneAuthorizationState(state)
			candidateRequest := request
			if test.mutate != nil {
				test.mutate(&candidateState, &candidateRequest)
			}
			decision := policy.EvaluateActionAuthorization(candidateState, candidateRequest, at)
			if decision.Reason != test.reason || decision.Allowed != (test.reason == policy.ActionAuthorized) {
				t.Fatalf("decision = %+v, want reason %q", decision, test.reason)
			}
		})
	}
}

func TestPolicyAuthorizedOperatorResumeLeaseIsConsumedOnce(t *testing.T) {
	at := time.Date(2030, time.January, 1, 1, 30, 0, 0, time.UTC)
	state, request := authorizedResumeFixture()
	decision := policy.EvaluateActionAuthorization(state, request, at)
	if !decision.Allowed {
		t.Fatalf("authorization decision = %+v", decision)
	}

	store := &actionLeaseMemoryStore{domain: policy.DomainUser}
	lease, err := actionlease.IssueAndPersist(
		store,
		actionlease.IssueInput{
			Domain: request.Domain, Capability: request.Capability,
			BundleGeneration:       request.BundleGeneration,
			DomainPolicyGeneration: request.DomainPolicyGeneration,
			ControlStateGeneration: request.ControlStateGeneration,
			Target:                 request.Target, PlanSHA256: request.PlanSHA256,
			BootID: testBootID,
		},
		fixedActionClock{wall: at, monotonic: 10 * time.Second},
		bytes.NewReader(actionRandom()),
	)
	if err != nil {
		t.Fatal(err)
	}
	current := actionlease.CurrentAuthorization{
		Domain: request.Domain, Capability: request.Capability,
		BundleGeneration:       request.BundleGeneration,
		DomainPolicyGeneration: request.DomainPolicyGeneration,
		ControlStateGeneration: request.ControlStateGeneration,
		Target:                 request.Target, PlanSHA256: request.PlanSHA256,
		BootID: testBootID, MonotonicNS: int64(11 * time.Second), ObservedAt: at,
	}
	guard, err := actionlease.NewGuard(store, lease.ActionID)
	if err != nil {
		t.Fatal(err)
	}
	if err := guard.BeforeStep(current); err != nil {
		t.Fatalf("BeforeStep() error: %v", err)
	}
	if err := guard.Commit(current); err != nil {
		t.Fatalf("Commit() error: %v", err)
	}
	replay, err := actionlease.NewGuard(store, lease.ActionID)
	if err != nil {
		t.Fatal(err)
	}
	if err := replay.BeforeStep(current); !errors.Is(err, actionlease.ErrLeaseReplay) {
		t.Fatalf("replay error = %v, want %v", err, actionlease.ErrLeaseReplay)
	}
}

func TestGenerationRejectedOperatorResumeLeaseCannotReplay(t *testing.T) {
	at := time.Date(2030, time.January, 1, 1, 30, 0, 0, time.UTC)
	_, request := authorizedResumeFixture()
	store := &actionLeaseMemoryStore{domain: policy.DomainUser}
	lease, err := actionlease.IssueAndPersist(
		store,
		actionlease.IssueInput{
			Domain: request.Domain, Capability: request.Capability,
			BundleGeneration:       request.BundleGeneration,
			DomainPolicyGeneration: request.DomainPolicyGeneration,
			ControlStateGeneration: request.ControlStateGeneration,
			Target:                 request.Target, PlanSHA256: request.PlanSHA256,
			BootID: testBootID,
		},
		fixedActionClock{wall: at, monotonic: 10 * time.Second},
		bytes.NewReader(actionRandom()),
	)
	if err != nil {
		t.Fatal(err)
	}
	current := actionlease.CurrentAuthorization{
		Domain: request.Domain, Capability: request.Capability,
		BundleGeneration:       request.BundleGeneration + 1,
		DomainPolicyGeneration: request.DomainPolicyGeneration,
		ControlStateGeneration: request.ControlStateGeneration,
		Target:                 request.Target, PlanSHA256: request.PlanSHA256,
		BootID: testBootID, MonotonicNS: int64(11 * time.Second), ObservedAt: at,
	}
	guard, err := actionlease.NewGuard(store, lease.ActionID)
	if err != nil {
		t.Fatal(err)
	}
	if err := guard.BeforeStep(current); !errors.Is(err, actionlease.ErrLeaseStale) {
		t.Fatalf("stale error = %v, want %v", err, actionlease.ErrLeaseStale)
	}
	current.BundleGeneration = request.BundleGeneration
	second, err := actionlease.NewGuard(store, lease.ActionID)
	if err != nil {
		t.Fatal(err)
	}
	if err := second.BeforeStep(current); !errors.Is(err, actionlease.ErrLeaseReplay) {
		t.Fatalf("second error = %v, want %v", err, actionlease.ErrLeaseReplay)
	}
}

func authorizedResumeFixture() (policy.ActionAuthorizationState, policy.ActionAuthorizationRequest) {
	planDigest := policy.SHA256Hex([]byte("synthetic-resume-plan"))
	manifestDigest := policy.SHA256Hex([]byte("synthetic-manifest"))
	return policy.ActionAuthorizationState{
			Status: policy.Status{
				Schema: policy.PolicyStatusSchema, Domain: policy.DomainUser,
				State: policy.PolicyActive, BundleGeneration: 9, PolicyGeneration: 5,
				ManifestSHA256: manifestDigest, ActivatedAt: "2030-01-01T01:00:00Z",
				Reason: policy.ReasonNone,
			},
			Suspension: policy.AuthorizationSuspension{
				Schema: policy.AuthorizationSuspensionSchema, Reason: policy.ReasonNone,
			},
			Payload: policy.DomainPayload{
				Schema: policy.DomainPayloadSchema, Domain: policy.DomainUser, PolicyGeneration: 5,
				Rules: []policy.Rule{{
					ID: "user.resume-synthetic", Effect: policy.EffectAllow,
					Selector: policy.Selector{
						ID: "user.resume-synthetic-selector", Kind: policy.SelectorAction,
						Action: &policy.ActionSelector{
							Capability: policy.CapabilityOperatorResume, Target: testTarget,
						},
					},
				}},
				Leases: []policy.AuthorizationLease{{
					ID: "user.resume-synthetic-lease", Domain: policy.DomainUser,
					Capability:  policy.CapabilityOperatorResume,
					SelectorIDs: []string{"user.resume-synthetic-selector"},
					IssuedAt:    "2030-01-01T01:00:00Z", ExpiresAt: "2030-01-01T02:00:00Z",
				}},
			},
			ControlStateGeneration: 7,
		}, policy.ActionAuthorizationRequest{
			Domain: policy.DomainUser, Capability: policy.CapabilityOperatorResume,
			BundleGeneration: 9, DomainPolicyGeneration: 5, ControlStateGeneration: 7,
			Target: testTarget, PlanSHA256: planDigest,
		}
}

func cloneAuthorizationState(source policy.ActionAuthorizationState) policy.ActionAuthorizationState {
	cloned := source
	cloned.Payload.Rules = append([]policy.Rule(nil), source.Payload.Rules...)
	cloned.Payload.Leases = append([]policy.AuthorizationLease(nil), source.Payload.Leases...)
	for index := range cloned.Payload.Leases {
		cloned.Payload.Leases[index].SelectorIDs = append(
			[]string(nil),
			source.Payload.Leases[index].SelectorIDs...,
		)
	}
	return cloned
}

func actionRandom() []byte {
	random := make([]byte, 32)
	for index := range random {
		random[index] = byte(index + 1)
	}
	return random
}

type fixedActionClock struct {
	wall      time.Time
	monotonic time.Duration
}

func (clock fixedActionClock) WallNow() time.Time          { return clock.wall }
func (clock fixedActionClock) MonotonicNow() time.Duration { return clock.monotonic }

var errActionLeaseRecord = errors.New("action lease record conflict")

type actionLeaseMemoryStore struct {
	domain  policy.Domain
	lease   policy.ActionLease
	claim   *policy.ActionLeaseExecutionClaim
	outcome *policy.ActionLeaseOutcome
}

func (store *actionLeaseMemoryStore) Domain() policy.Domain { return store.domain }

func (store *actionLeaseMemoryStore) PersistActionLease(lease policy.ActionLease) error {
	if store.lease.ActionID != "" {
		return errActionLeaseRecord
	}
	store.lease = lease
	return nil
}

func (store *actionLeaseMemoryStore) ReadActionLeaseState(
	actionID metadata.UUID,
) (policy.ActionLease, *policy.ActionLeaseOutcome, error) {
	if store.lease.ActionID != actionID {
		return policy.ActionLease{}, nil, errActionLeaseRecord
	}
	return store.lease, store.outcome, nil
}

func (store *actionLeaseMemoryStore) ReadActionLeaseExecutionClaim(
	actionID metadata.UUID,
) (policy.ActionLeaseExecutionClaim, error) {
	if store.lease.ActionID != actionID || store.claim == nil {
		return policy.ActionLeaseExecutionClaim{}, errActionLeaseRecord
	}
	return *store.claim, nil
}

func (store *actionLeaseMemoryStore) PersistActionLeaseExecutionClaim(
	claim policy.ActionLeaseExecutionClaim,
) error {
	if store.claim != nil {
		if *store.claim == claim {
			return nil
		}
		return errActionLeaseRecord
	}
	store.claim = &claim
	return nil
}

func (store *actionLeaseMemoryStore) PersistActionLeaseOutcome(
	outcome policy.ActionLeaseOutcome,
) error {
	if store.outcome != nil {
		return errActionLeaseRecord
	}
	store.outcome = &outcome
	return nil
}
