package policy

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestComposeEffectiveSnapshotIntersectsLeasesAndDeduplicates(t *testing.T) {
	envelope := DefaultSafetyEnvelope()
	source := validEnvelopeSource(t, envelope)
	source.Root.Rules = append(source.Root.Rules, Rule{
		ID: "root.unleased-network", Effect: EffectAllow,
		Selector: Selector{
			ID: "root.resume-network", Kind: SelectorAction,
			Action: &ActionSelector{Capability: CapabilityOperatorResume, Target: "network"},
		},
	})
	source.User.Rules = append(source.User.Rules, Rule{
		ID: "user.allow-resume-copy", Effect: EffectAllow,
		Selector: Selector{
			ID: "user.resume-pritunl-copy", Kind: SelectorAction,
			Action: &ActionSelector{Capability: CapabilityOperatorResume, Target: "pritunl"},
		},
	})
	source.User.Leases = append(source.User.Leases, AuthorizationLease{
		ID: "user.night-recovery-copy", Domain: DomainUser,
		Capability: CapabilityOperatorResume, SelectorIDs: []string{"user.resume-pritunl-copy"},
		IssuedAt: testTime, ExpiresAt: testExpiry,
	})

	snapshot, err := ComposeEffectiveSnapshot(source, envelope)
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	if len(snapshot.Root.Rules) != 1 || snapshot.Root.Rules[0].Effect != EffectDeny {
		t.Fatalf("unleased allow was not removed: %#v", snapshot.Root.Rules)
	}
	if len(snapshot.User.Rules) != 1 || len(snapshot.User.Leases) != 1 {
		t.Fatalf("semantic duplicates were not eliminated: rules=%d leases=%d", len(snapshot.User.Rules), len(snapshot.User.Leases))
	}
	if snapshot.User.Leases[0].SelectorIDs[0] != snapshot.User.Rules[0].Selector.ID {
		t.Fatal("deduplicated lease was not remapped to the retained selector")
	}
}

func TestComposeEffectiveSnapshotCompiledDenyWins(t *testing.T) {
	envelope := DefaultSafetyEnvelope()
	envelope.User.DeniedSelectors = []Selector{{
		ID: "compiled.user.resume-pritunl", Kind: SelectorAction,
		Action: &ActionSelector{Capability: CapabilityOperatorResume, Target: "pritunl"},
	}}
	source := validEnvelopeSource(t, envelope)

	snapshot, err := ComposeEffectiveSnapshot(source, envelope)
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	if len(snapshot.User.Rules) != 1 || snapshot.User.Rules[0].Effect != EffectDeny ||
		!strings.HasPrefix(snapshot.User.Rules[0].Selector.ID, "compiled.user.") {
		t.Fatalf("compiled deny did not replace overlapping source authorization: %#v", snapshot.User.Rules)
	}
	if len(snapshot.User.Leases) != 0 {
		t.Fatal("lease for a compiled-denied selector must be removed")
	}
}

func TestComposeRejectsCompleteConflictingCandidateWithRedactedReport(t *testing.T) {
	envelope := broadTestEnvelope()
	digest, err := envelope.SHA256()
	if err != nil {
		t.Fatal(err)
	}
	source := OperatorSource{
		Schema: OperatorSourceSchema, PolicySchema: 1,
		BundleGeneration: 2, ParentBundleGeneration: 1,
		StaticSHA256: digest, IssuedAt: testTime, NotBefore: testTime, ExpiresAt: testExpiry,
		Root: DomainSource{PolicyGeneration: 1, Rules: []Rule{
			endpointRule("root.gateway-wildcard", "root.gateway-wildcard-selector", "*.example.test", EffectAllow, TLSRequired, PathPhysical, ProtocolTCP),
			endpointRule("root.gateway-api", "root.gateway-api-selector", "api.example.test", EffectAllow, TLSRequired, PathManagedTUN, ProtocolTCP),
		}, Leases: []AuthorizationLease{
			{ID: "root.gateway-wildcard-lease", Domain: DomainRoot, Capability: CapabilityOperatorResume, SelectorIDs: []string{"root.gateway-wildcard-selector"}, IssuedAt: testTime, ExpiresAt: testExpiry},
			{ID: "root.gateway-api-lease", Domain: DomainRoot, Capability: CapabilityOperatorResume, SelectorIDs: []string{"root.gateway-api-selector"}, IssuedAt: testTime, ExpiresAt: testExpiry},
		}},
		User: DomainSource{PolicyGeneration: 1},
	}

	snapshot, err := ComposeEffectiveSnapshot(source, envelope)
	if err == nil || !reflect.DeepEqual(snapshot, EffectiveSnapshot{}) {
		t.Fatal("conflicting candidate must be rejected as a whole")
	}
	var conflict *ConflictError
	if !errors.As(err, &conflict) || len(conflict.Report.Codes) == 0 || len(conflict.Report.Codes) > MaxConflictCodes {
		t.Fatalf("expected bounded conflict report: %v", err)
	}
	encoded, marshalErr := json.Marshal(conflict)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	combined := string(encoded) + err.Error()
	for _, protected := range []string{"api.example.test", "managed_tun", "physical", "root.gateway", "443"} {
		if strings.Contains(combined, protected) {
			t.Fatalf("conflict report leaked protected selector data %q: %s", protected, combined)
		}
	}
}

func broadTestEnvelope() SafetyEnvelope {
	envelope := DefaultSafetyEnvelope()
	kinds := []SelectorKind{SelectorEndpoint, SelectorRoute, SelectorAction, SelectorCredential}
	envelope.Root.AllowedSelectorKinds = append([]SelectorKind(nil), kinds...)
	envelope.User.AllowedSelectorKinds = append([]SelectorKind(nil), kinds...)
	envelope.Root.AllowedTargets = []string{"shared"}
	envelope.User.AllowedTargets = []string{"shared"}
	return envelope
}

func endpointRule(ruleID, selectorID, host string, effect Effect, tls TLSMode, path NetworkPath, protocol Protocol) Rule {
	return Rule{
		ID: ruleID, Effect: effect,
		Selector: Selector{
			ID: selectorID, Kind: SelectorEndpoint,
			Endpoint: &EndpointSelector{
				Host: host, Ports: []PortRange{{First: 443, Last: 443}},
				Protocol: protocol, TLS: tls, Path: path,
			},
		},
	}
}
