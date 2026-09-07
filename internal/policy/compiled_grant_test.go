package policy

import (
	"testing"
	"time"
)

// grantSource is the generation the Pritunl cutover needs, expressed as an
// operator would write it: one capability, both domains, each covered by a
// lease naming its own selector.
func grantSource(t *testing.T) OperatorSource {
	t.Helper()
	envelope := DefaultSafetyEnvelope()
	digest, err := envelope.SHA256()
	if err != nil {
		t.Fatal(err)
	}
	lease := func(prefix string, domain Domain) AuthorizationLease {
		return AuthorizationLease{
			ID: prefix + ".lease-recovery", Domain: domain,
			Capability:  CapabilityPritunlRecovery,
			SelectorIDs: []string{prefix + ".recover-pritunl"},
			IssuedAt:    testTime, ExpiresAt: testExpiry,
		}
	}
	return OperatorSource{
		Schema: OperatorSourceSchema, PolicySchema: 1,
		BundleGeneration: 2, ParentBundleGeneration: 1,
		StaticSHA256: digest,
		IssuedAt:     testTime, NotBefore: testTime, ExpiresAt: testExpiry,
		Root: DomainSource{
			PolicyGeneration: 1,
			Rules: []Rule{capabilityRule("root.allow-recovery", "root.recover-pritunl",
				EffectAllow, CapabilityPritunlRecovery, "pritunl")},
			Leases: []AuthorizationLease{lease("root", DomainRoot)},
		},
		User: DomainSource{
			PolicyGeneration: 1,
			Rules: []Rule{capabilityRule("user.allow-recovery", "user.recover-pritunl",
				EffectAllow, CapabilityPritunlRecovery, "pritunl")},
			Leases: []AuthorizationLease{lease("user", DomainUser)},
		},
	}
}

func activeStateFor(payload DomainPayload) ActionAuthorizationState {
	return ActionAuthorizationState{
		Status: Status{
			Schema: PolicyStatusSchema, Domain: payload.Domain, State: PolicyActive,
			BundleGeneration: 2, PolicyGeneration: payload.PolicyGeneration,
			ManifestSHA256: testDigest, ActivatedAt: testTime, Reason: ReasonNone,
		},
		Suspension: AuthorizationSuspension{
			Schema: AuthorizationSuspensionSchema, Reason: ReasonNone,
		},
		Payload:                payload,
		ControlStateGeneration: 7,
	}
}

func recoveryRequest(domain Domain, payload DomainPayload) ActionAuthorizationRequest {
	return ActionAuthorizationRequest{
		Domain: domain, Capability: CapabilityPritunlRecovery,
		BundleGeneration: 2, DomainPolicyGeneration: payload.PolicyGeneration,
		ControlStateGeneration: 7, Target: "pritunl", PlanSHA256: testDigest,
	}
}

// TestACompiledGrantAuthorizesBothHalves crosses the boundary that hid the
// defect this change repairs.
//
// The capability was defined, permitted by the envelope, asked for by both
// runtimes and written into a runbook — and never compiled, because the one
// test that named it evaluated authorization against a payload written by hand.
// Every part was proven; the path between them was not.
//
// So this compiles the generation and hands what the compiler emitted to the
// evaluator, in both domains, as one path.
func TestACompiledGrantAuthorizesBothHalves(t *testing.T) {
	snapshot, err := ComposeEffectiveSnapshot(grantSource(t), DefaultSafetyEnvelope())
	if err != nil {
		t.Fatalf("the generation the cutover needs does not compile: %v", err)
	}
	at, err := time.Parse(time.RFC3339, "2026-08-02T09:30:00Z")
	if err != nil {
		t.Fatal(err)
	}

	for _, payload := range []DomainPayload{snapshot.Root, snapshot.User} {
		t.Run(string(payload.Domain), func(t *testing.T) {
			decision := EvaluateActionAuthorization(
				activeStateFor(payload), recoveryRequest(payload.Domain, payload), at,
			)
			if !decision.Allowed || decision.Reason != ActionAuthorized {
				t.Fatalf("the compiled payload authorized nothing in %s: %+v",
					payload.Domain, decision)
			}
		})
	}
}

// TestACompiledGrantAuthorizesNothingWithoutItsLease is the other half of the
// same path. Deny-by-default is what makes a grant mean anything, so the
// negative has to travel the same route as the positive.
//
// The refusal turns out to be stronger than a refusal at evaluation. An allow
// rule no lease covers does not survive compilation at all — leases narrow by
// intersection, and intersecting with nothing leaves nothing — so the payload
// handed to the evaluator carries no rule to match. A grant that was never
// leased cannot be shipped, let alone acted on.
func TestACompiledGrantAuthorizesNothingWithoutItsLease(t *testing.T) {
	source := grantSource(t)
	source.User.Leases = nil
	snapshot, err := ComposeEffectiveSnapshot(source, DefaultSafetyEnvelope())
	if err != nil {
		t.Fatalf("compose without a lease: %v", err)
	}
	if len(snapshot.User.Rules) != 0 {
		t.Fatalf("an unleased allow rule survived compilation: %+v", snapshot.User.Rules)
	}
	at, err := time.Parse(time.RFC3339, "2026-08-02T09:30:00Z")
	if err != nil {
		t.Fatal(err)
	}
	decision := EvaluateActionAuthorization(
		activeStateFor(snapshot.User), recoveryRequest(DomainUser, snapshot.User), at,
	)
	if decision.Allowed || decision.Reason != ActionSelectorMismatch {
		t.Fatalf("decision = %+v, want a refusal for having no rule to match", decision)
	}

	// And a leased grant stops authorizing once its lease's own window closes,
	// without the generation changing at all.
	full, err := ComposeEffectiveSnapshot(grantSource(t), DefaultSafetyEnvelope())
	if err != nil {
		t.Fatal(err)
	}
	expired, err := time.Parse(time.RFC3339, "2026-08-02T11:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	lapsed := EvaluateActionAuthorization(
		activeStateFor(full.User), recoveryRequest(DomainUser, full.User), expired,
	)
	if lapsed.Allowed || lapsed.Reason != ActionAuthorizationLeaseEnded {
		t.Fatalf("decision = %+v, want the refusal to name the lapsed lease", lapsed)
	}
}
