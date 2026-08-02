package policy

import "testing"

func TestEndpointConflictHasNoSpecificityPrecedence(t *testing.T) {
	tests := []struct {
		name     string
		concrete Rule
		conflict bool
	}{
		{
			name: "wildcard concrete path conflict",
			concrete: endpointRule("root.api", "root.api-selector", "api.example.test", EffectAllow,
				TLSRequired, PathManagedTUN, ProtocolTCP),
			conflict: true,
		},
		{
			name: "wildcard concrete protocol conflict",
			concrete: endpointRule("root.api", "root.api-selector", "api.example.test", EffectAllow,
				TLSRequired, PathPhysical, ProtocolUDP),
			conflict: true,
		},
		{
			name: "wildcard concrete identical semantics",
			concrete: endpointRule("root.api", "root.api-selector", "api.example.test", EffectAllow,
				TLSRequired, PathPhysical, ProtocolTCP),
			conflict: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := conflictSnapshot(
				[]Rule{endpointRule("root.wildcard", "root.wildcard-selector", "*.example.test", EffectAllow, TLSRequired, PathPhysical, ProtocolTCP), test.concrete},
				nil,
			)
			report := FindConflicts(snapshot)
			if report.Empty() == test.conflict {
				t.Fatalf("conflict=%v report=%#v", test.conflict, report)
			}
			if test.conflict && report.Codes[0] != ConflictEndpointSemantics {
				t.Fatalf("unexpected code: %#v", report)
			}
		})
	}
}

func TestTypedSelectorConflicts(t *testing.T) {
	tests := []struct {
		name  string
		left  Rule
		right Rule
		code  ConflictCode
	}{
		{
			name:  "route cidr overlap",
			left:  routeRule("root.route-wide", "root.route-wide-selector", "192.0.2.0/24", EffectAllow, PathPhysical),
			right: routeRule("root.route-narrow", "root.route-narrow-selector", "192.0.2.0/25", EffectAllow, PathManagedTUN),
			code:  ConflictRouteSemantics,
		},
		{
			name:  "action target overlap",
			left:  actionRule("root.action-allow", "root.action-allow-selector", EffectAllow, "shared"),
			right: actionRule("root.action-deny", "root.action-deny-selector", EffectDeny, "shared"),
			code:  ConflictActionSemantics,
		},
		{
			name:  "credential reference overlap",
			left:  credentialRule("root.credential-allow", "root.credential-allow-selector", EffectAllow, "synthetic-key", DomainRoot),
			right: credentialRule("root.credential-deny", "root.credential-deny-selector", EffectDeny, "synthetic-key", DomainRoot),
			code:  ConflictCredentialOwner,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			report := FindConflicts(conflictSnapshot([]Rule{test.left, test.right}, nil))
			if len(report.Codes) != 1 || report.Codes[0] != test.code {
				t.Fatalf("unexpected report: %#v", report)
			}
		})
	}
}

func TestCrossDomainSelectorOwnershipConflict(t *testing.T) {
	root := credentialRule("root.credential", "root.credential-selector", EffectAllow, "synthetic-key", DomainRoot)
	user := credentialRule("user.credential", "user.credential-selector", EffectAllow, "synthetic-key", DomainUser)
	report := FindConflicts(conflictSnapshot([]Rule{root}, []Rule{user}))
	if len(report.Codes) != 1 || report.Codes[0] != ConflictCrossDomain {
		t.Fatalf("unexpected report: %#v", report)
	}
}

func conflictSnapshot(rootRules, userRules []Rule) EffectiveSnapshot {
	return EffectiveSnapshot{
		Schema: EffectiveSnapshotSchema, PolicySchema: 1,
		BundleGeneration: 2, ParentBundleGeneration: 1,
		StaticSHA256: testDigest, IssuedAt: testTime, NotBefore: testTime, ExpiresAt: testExpiry,
		Root: DomainPayload{Schema: DomainPayloadSchema, Domain: DomainRoot, BundleGeneration: 2, PolicyGeneration: 1, Rules: rootRules},
		User: DomainPayload{Schema: DomainPayloadSchema, Domain: DomainUser, BundleGeneration: 2, PolicyGeneration: 1, Rules: userRules},
	}
}

func routeRule(ruleID, selectorID, prefix string, effect Effect, path NetworkPath) Rule {
	return Rule{ID: ruleID, Effect: effect, Selector: Selector{
		ID: selectorID, Kind: SelectorRoute, Route: &RouteSelector{Prefix: prefix, Path: path},
	}}
}

func actionRule(ruleID, selectorID string, effect Effect, target string) Rule {
	return Rule{ID: ruleID, Effect: effect, Selector: Selector{
		ID: selectorID, Kind: SelectorAction,
		Action: &ActionSelector{Capability: CapabilityOperatorResume, Target: target},
	}}
}

func credentialRule(ruleID, selectorID string, effect Effect, reference string, owner Domain) Rule {
	return Rule{ID: ruleID, Effect: effect, Selector: Selector{
		ID: selectorID, Kind: SelectorCredential,
		Credential: &CredentialSelector{Reference: reference, Owner: owner},
	}}
}
