package policycontrol

import (
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/policyclock"
)

// These ask the real handler and the real evaluator.
//
// The daemon's own test uses a stand-in authority, which is right for what it
// checks and cannot check this: whether the question the daemon sends is one
// the evaluator will consider at all. On 2026-09-14 it was not — the daemon
// sent generation zero and an empty digest, every answer came back
// `invalid_request`, and nothing that ran in `make check` could see it.

const tunnelTestPlan = "synthetic-tunnel-plan"

func activeRootHandler(t *testing.T, payload policy.DomainPayload) *Handler {
	t.Helper()
	guard, err := policyclock.NewGuard(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	wall := time.Date(2030, 1, 1, 1, 30, 0, 0, time.UTC)
	var monotonic time.Duration
	return &Handler{
		domain: policy.DomainRoot,
		now: func() time.Time {
			return wall.Add(monotonic)
		},
		monotonicNow: func() time.Duration {
			monotonic += time.Millisecond
			return monotonic
		},
		clockGuard: guard,
		status: policy.Status{
			Schema: policy.PolicyStatusSchema, Domain: policy.DomainRoot,
			State: policy.PolicyActive, BundleGeneration: 4, PolicyGeneration: 4,
			ManifestSHA256: policy.SHA256Hex([]byte("synthetic-manifest")),
			ActivatedAt:    "2030-01-01T01:00:00Z", Reason: policy.ReasonNone,
		},
		authorizationSuspension: policy.AuthorizationSuspension{
			Schema: policy.AuthorizationSuspensionSchema, Reason: policy.ReasonNone,
		},
		activePayload: payload,
		hasActive:     true,
	}
}

func rootPayload(rules []policy.Rule, leases []policy.AuthorizationLease) policy.DomainPayload {
	return policy.DomainPayload{
		Schema: policy.DomainPayloadSchema, Domain: policy.DomainRoot,
		PolicyGeneration: 4, Rules: rules, Leases: leases,
	}
}

// The question as the daemon used to send it is refused as malformed, and
// would be refused so under any generation.
func TestTunnelQuestionWithoutGenerationOrDigestIsMalformed(t *testing.T) {
	handler := activeRootHandler(t, rootPayload(nil, nil))
	answer := handler.AuthorizeTunnelOwnership("tunnel", 0, "")
	if answer.Allowed || answer.Reason != policy.ActionInvalidRequest {
		t.Fatalf("answer = %+v, want a malformed-request refusal", answer)
	}
}

// A well-formed question under a generation that grants nothing is refused by
// policy — and says so, rather than calling the question malformed.
func TestTunnelQuestionUnderAGenerationGrantingNothingIsAPolicyRefusal(t *testing.T) {
	handler := activeRootHandler(t, rootPayload(nil, nil))
	answer := handler.AuthorizeTunnelOwnership(
		"tunnel", 7, policy.SHA256Hex([]byte(tunnelTestPlan)))
	if answer.Allowed {
		t.Fatal("a generation granting nothing authorized the tunnel")
	}
	if answer.Reason != policy.ActionSelectorMismatch {
		t.Fatalf("reason = %q, want %q", answer.Reason, policy.ActionSelectorMismatch)
	}
}

// The same question under a generation that grants the capability is
// authorized. This is the turn the daemon's records were promised to make, and
// could not while the question was malformed.
func TestTunnelQuestionUnderAGrantingGenerationIsAuthorized(t *testing.T) {
	selector := "root.tunnel-synthetic-selector"
	handler := activeRootHandler(t, rootPayload(
		[]policy.Rule{{
			ID: "root.tunnel-synthetic", Effect: policy.EffectAllow,
			Selector: policy.Selector{
				ID: selector, Kind: policy.SelectorAction,
				Action: &policy.ActionSelector{
					Capability: policy.CapabilityTunnelOwnership, Target: "tunnel",
				},
			},
		}},
		[]policy.AuthorizationLease{{
			ID: "root.tunnel-synthetic-lease", Domain: policy.DomainRoot,
			Capability:  policy.CapabilityTunnelOwnership,
			SelectorIDs: []string{selector},
			IssuedAt:    "2030-01-01T01:00:00Z", ExpiresAt: "2030-01-01T02:00:00Z",
		}},
	))
	answer := handler.AuthorizeTunnelOwnership(
		"tunnel", 7, policy.SHA256Hex([]byte(tunnelTestPlan)))
	if !answer.Allowed || answer.Reason != policy.ActionAuthorized {
		t.Fatalf("answer = %+v, want authorized", answer)
	}
}
