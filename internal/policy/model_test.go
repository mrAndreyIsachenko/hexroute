package policy

import (
	"errors"
	"strings"
	"testing"
)

const (
	testDigest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	testTime   = "2026-08-02T09:00:00Z"
	testExpiry = "2026-08-02T10:00:00Z"
	testUUID   = "123e4567-e89b-42d3-a456-426614174000"
)

func TestManifestValidate(t *testing.T) {
	manifest := validManifest()
	if err := manifest.Validate(); err != nil {
		t.Fatalf("valid manifest: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*Manifest)
	}{
		{name: "schema", mutate: func(value *Manifest) { value.Schema = "wrong" }},
		{name: "generation", mutate: func(value *Manifest) { value.ParentBundleGeneration = value.BundleGeneration }},
		{name: "digest", mutate: func(value *Manifest) { value.StaticSHA256 = "ABC" }},
		{name: "clock", mutate: func(value *Manifest) { value.NotBefore = "2026-08-02T08:00:00Z" }},
		{name: "timezone", mutate: func(value *Manifest) { value.IssuedAt = "2026-08-02T12:00:00+03:00" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := manifest
			test.mutate(&candidate)
			if !errors.Is(candidate.Validate(), ErrInvalidManifest) {
				t.Fatal("invalid manifest should be rejected")
			}
		})
	}
}

func TestDomainPayloadValidate(t *testing.T) {
	payload := validPayload()
	if err := payload.Validate(); err != nil {
		t.Fatalf("valid payload: %v", err)
	}

	t.Run("selector union", func(t *testing.T) {
		candidate := validPayload()
		candidate.Rules[0].Selector.Route = &RouteSelector{
			Prefix: "192.0.2.0/24",
			Path:   PathPhysical,
		}
		if !errors.Is(candidate.Validate(), ErrInvalidDomainPayload) {
			t.Fatal("selector with multiple bodies should be rejected")
		}
	})

	t.Run("unknown lease selector", func(t *testing.T) {
		candidate := validPayload()
		candidate.Leases[0].SelectorIDs = []string{"missing-selector"}
		if !errors.Is(candidate.Validate(), ErrInvalidDomainPayload) {
			t.Fatal("lease referencing an unknown selector should be rejected")
		}
	})

	t.Run("duplicate rule", func(t *testing.T) {
		candidate := validPayload()
		candidate.Rules = append(candidate.Rules, candidate.Rules[0])
		if !errors.Is(candidate.Validate(), ErrInvalidDomainPayload) {
			t.Fatal("duplicate rules should be rejected")
		}
	})
}

func TestSelectorValidation(t *testing.T) {
	tests := []struct {
		name     string
		selector Selector
	}{
		{
			name: "endpoint",
			selector: Selector{
				ID:   "endpoint-one",
				Kind: SelectorEndpoint,
				Endpoint: &EndpointSelector{
					Host:     "gateway.example.test",
					Ports:    []PortRange{{First: 443, Last: 443}},
					Protocol: ProtocolTCP,
					TLS:      TLSRequired,
					Path:     PathManagedTUN,
				},
			},
		},
		{
			name: "route",
			selector: Selector{
				ID: "route-one", Kind: SelectorRoute,
				Route: &RouteSelector{Prefix: "192.0.2.0/24", Path: PathPhysical},
			},
		},
		{
			name: "action",
			selector: Selector{
				ID: "resume-one", Kind: SelectorAction,
				Action: &ActionSelector{Capability: CapabilityOperatorResume, Target: "pritunl"},
			},
		},
		{
			name: "credential",
			selector: Selector{
				ID: "credential-one", Kind: SelectorCredential,
				Credential: &CredentialSelector{Reference: "pritunl-login", Owner: DomainUser},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.selector.Validate(); err != nil {
				t.Fatalf("valid selector: %v", err)
			}
		})
	}

	invalid := tests[0].selector
	invalid.Endpoint.Host = "Bad Host"
	if !errors.Is(invalid.Validate(), ErrInvalidSelector) {
		t.Fatal("invalid endpoint host should be rejected")
	}
}

func TestActionLeaseValidate(t *testing.T) {
	lease := ActionLease{
		Schema:                 ActionLeaseSchema,
		ActionID:               testUUID,
		Domain:                 DomainUser,
		Capability:             CapabilityOperatorResume,
		BundleGeneration:       3,
		DomainPolicyGeneration: 2,
		ControlStateGeneration: 8,
		Target:                 "pritunl",
		PlanSHA256:             testDigest,
		IssuedAt:               testTime,
		ExpiresAt:              testExpiry,
		IssuedMonotonicNS:      10,
		ExpiresMonotonicNS:     20,
		BootID:                 testUUID,
		Nonce:                  "123e4567-e89b-42d3-a456-426614174001",
		Status:                 LeasePending,
	}
	if err := lease.Validate(); err != nil {
		t.Fatalf("valid action lease: %v", err)
	}

	lease.ExpiresMonotonicNS = lease.IssuedMonotonicNS
	if !errors.Is(lease.Validate(), ErrInvalidActionLease) {
		t.Fatal("non-increasing monotonic lease should be rejected")
	}
}

func TestStatusValidate(t *testing.T) {
	active := Status{
		Schema: PolicyStatusSchema, Domain: DomainRoot, State: PolicyActive,
		BundleGeneration: 4, PolicyGeneration: 3, ManifestSHA256: testDigest,
		ActivatedAt: testTime, Reason: ReasonNone,
	}
	if err := active.Validate(); err != nil {
		t.Fatalf("valid active status: %v", err)
	}

	none := Status{
		Schema: PolicyStatusSchema, Domain: DomainUser, State: PolicyNone,
		Reason: ReasonNoValidGeneration,
	}
	if err := none.Validate(); err != nil {
		t.Fatalf("valid empty status: %v", err)
	}
	prepared := Status{
		Schema: PolicyStatusSchema, Domain: DomainUser, State: PolicyPrepared,
		BundleGeneration: 5, PolicyGeneration: 4, ManifestSHA256: testDigest,
		Reason: ReasonNone,
	}
	if err := prepared.Validate(); err != nil {
		t.Fatalf("valid prepared status: %v", err)
	}

	active.Reason = ReasonClockAnomaly
	if !errors.Is(active.Validate(), ErrInvalidStatus) {
		t.Fatal("active status with failure reason should be rejected")
	}
}

func TestAuthorizationSuspensionValidate(t *testing.T) {
	clear := AuthorizationSuspension{
		Schema: AuthorizationSuspensionSchema,
		Reason: ReasonNone,
	}
	if err := clear.Validate(); err != nil {
		t.Fatalf("valid clear suspension: %v", err)
	}
	for _, reason := range []PolicyReason{
		ReasonCorruption,
		ReasonInvalidSignature,
		ReasonDigestMismatch,
		ReasonDomainMismatch,
		ReasonClockAnomaly,
		ReasonIPCOwnership,
	} {
		suspended := AuthorizationSuspension{
			Schema:    AuthorizationSuspensionSchema,
			Suspended: true,
			Reason:    reason,
			Since:     testTime,
		}
		if err := suspended.Validate(); err != nil {
			t.Fatalf("valid suspension for %s: %v", reason, err)
		}
	}
	invalid := clear
	invalid.Suspended = true
	invalid.Reason = ReasonUnsupportedSchema
	invalid.Since = testTime
	if !errors.Is(invalid.Validate(), ErrInvalidStatus) {
		t.Fatal("non-suspension reason should be rejected")
	}
}

func TestIdentifierBounds(t *testing.T) {
	selector := validPayload().Rules[0].Selector
	selector.ID = "a" + strings.Repeat("b", MaxIdentifierBytes)
	if !errors.Is(selector.Validate(), ErrInvalidSelector) {
		t.Fatal("oversized identifier should be rejected")
	}
	if Capability("restart").Valid() {
		t.Fatal("unapproved capability should not be valid")
	}
	if PolicyState("broken").Valid() {
		t.Fatal("unknown policy state should not be valid")
	}
}

func validManifest() Manifest {
	return Manifest{
		Schema: ManifestSchema, PolicySchema: 1, CompilerVersion: "v0.1.0",
		CompilerSHA256: testDigest, BundleGeneration: 2, ParentBundleGeneration: 1,
		Root:         DomainReference{Generation: 1, PayloadSHA256: testDigest},
		User:         DomainReference{Generation: 2, PayloadSHA256: testDigest},
		StaticSHA256: testDigest, SignerFingerprint: testDigest,
		IssuedAt: testTime, NotBefore: testTime, ExpiresAt: testExpiry,
	}
}

func validPayload() DomainPayload {
	return DomainPayload{
		Schema: DomainPayloadSchema, Domain: DomainUser,
		PolicyGeneration: 2,
		Rules: []Rule{{
			ID: "user.allow-resume", Effect: EffectAllow,
			Selector: Selector{
				ID: "user.resume-pritunl", Kind: SelectorAction,
				Action: &ActionSelector{Capability: CapabilityOperatorResume, Target: "pritunl"},
			},
		}},
		Leases: []AuthorizationLease{{
			ID: "user.night-recovery", Domain: DomainUser,
			Capability:  CapabilityOperatorResume,
			SelectorIDs: []string{"user.resume-pritunl"},
			IssuedAt:    testTime, ExpiresAt: testExpiry,
		}},
	}
}
