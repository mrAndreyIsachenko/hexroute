package policy

import (
	"errors"
	"testing"
)

func TestDefaultSafetyEnvelope(t *testing.T) {
	envelope := DefaultSafetyEnvelope()
	if err := envelope.Validate(); err != nil {
		t.Fatalf("default envelope: %v", err)
	}
	digest, err := envelope.SHA256()
	if err != nil || !validSHA256(digest) {
		t.Fatalf("default envelope digest: %q, %v", digest, err)
	}

	missing := envelope
	missing.ProtectedFields = missing.ProtectedFields[1:]
	if !errors.Is(missing.Validate(), ErrInvalidSafetyEnvelope) {
		t.Fatal("missing protected field should invalidate envelope")
	}
}

func TestSafetyEnvelopeCompiledDenyValidation(t *testing.T) {
	envelope := DefaultSafetyEnvelope()
	envelope.User.DeniedSelectors = []Selector{{
		ID: "compiled.user.resume-pritunl", Kind: SelectorAction,
		Action: &ActionSelector{Capability: CapabilityOperatorResume, Target: "pritunl"},
	}}
	if err := envelope.Validate(); err != nil {
		t.Fatalf("valid compiled deny: %v", err)
	}

	invalid := envelope
	invalid.User.DeniedSelectors = append([]Selector(nil), envelope.User.DeniedSelectors...)
	invalid.User.DeniedSelectors[0].ID = "user.dynamic-deny"
	if !errors.Is(invalid.Validate(), ErrInvalidSafetyEnvelope) {
		t.Fatal("dynamic namespace must not define a compiled deny")
	}
}

func TestValidateAgainstEnvelope(t *testing.T) {
	envelope := DefaultSafetyEnvelope()
	source := validEnvelopeSource(t, envelope)
	if err := ValidateAgainstEnvelope(source, envelope); err != nil {
		t.Fatalf("valid source against envelope: %v", err)
	}

	t.Run("cross-domain namespace", func(t *testing.T) {
		candidate := source
		candidate.User.Rules = append([]Rule(nil), source.User.Rules...)
		candidate.User.Rules[0].ID = "root.allow-user-resume"
		if !errors.Is(ValidateAgainstEnvelope(candidate, envelope), ErrOutsideSafetyEnvelope) {
			t.Fatal("cross-domain namespace should be rejected")
		}
	})

	t.Run("target expansion", func(t *testing.T) {
		candidate := source
		candidate.User.Rules = append([]Rule(nil), source.User.Rules...)
		action := *candidate.User.Rules[0].Selector.Action
		action.Target = "routes"
		candidate.User.Rules[0].Selector.Action = &action
		if !errors.Is(ValidateAgainstEnvelope(candidate, envelope), ErrOutsideSafetyEnvelope) {
			t.Fatal("user target expansion should be rejected")
		}
	})

	t.Run("static digest", func(t *testing.T) {
		candidate := source
		candidate.StaticSHA256 = testDigest
		if !errors.Is(ValidateAgainstEnvelope(candidate, envelope), ErrOutsideSafetyEnvelope) {
			t.Fatal("wrong static digest should be rejected")
		}
	})
}

func validEnvelopeSource(t *testing.T, envelope SafetyEnvelope) OperatorSource {
	t.Helper()
	digest, err := envelope.SHA256()
	if err != nil {
		t.Fatal(err)
	}
	return OperatorSource{
		Schema: OperatorSourceSchema, PolicySchema: 1,
		BundleGeneration: 2, ParentBundleGeneration: 1,
		StaticSHA256: digest, IssuedAt: testTime, NotBefore: testTime, ExpiresAt: testExpiry,
		Root: DomainSource{
			PolicyGeneration: 1,
			Rules: []Rule{{
				ID: "root.deny-resume", Effect: EffectDeny,
				Selector: Selector{
					ID: "root.resume-routes", Kind: SelectorAction,
					Action: &ActionSelector{Capability: CapabilityOperatorResume, Target: "routes"},
				},
			}},
		},
		User: DomainSource{
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
		},
	}
}
