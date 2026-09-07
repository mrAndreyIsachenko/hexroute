package policy

import "testing"

func capabilityRule(ruleID, selectorID string, effect Effect, capability Capability, target string) Rule {
	return Rule{ID: ruleID, Effect: effect, Selector: Selector{
		ID: selectorID, Kind: SelectorAction,
		Action: &ActionSelector{Capability: capability, Target: target},
	}}
}

// TestBothDomainsMayHoldOneTwoSidedCapability is the case the Pritunl cutover
// could not compile.
//
// Its runbook, both runtimes and the safety envelope all describe one capability
// granted to both domains on one target — the user runtime holds the credential
// and reconnects, the root runtime restarts the one named service — and the
// conflict detector refused it as an ownership violation.
func TestBothDomainsMayHoldOneTwoSidedCapability(t *testing.T) {
	report := FindConflicts(conflictSnapshot(
		[]Rule{capabilityRule("root.allow-recovery", "root.rescue-pritunl",
			EffectAllow, CapabilityPritunlRecovery, "pritunl")},
		[]Rule{capabilityRule("user.allow-recovery", "user.recover-pritunl",
			EffectAllow, CapabilityPritunlRecovery, "pritunl")},
	))
	if !report.Empty() {
		t.Fatalf("two domains authorized for one two-sided capability conflicted: %v", report.Codes)
	}
}

// TestOneDomainMayBeRevokedWhileTheOtherStands is why the narrower repair was
// not taken. Falling through to the same-domain check would call differing
// effects a conflict — and root allowing while user denies is exactly what a
// partial rollback leaves behind, refused at the moment it is needed.
func TestOneDomainMayBeRevokedWhileTheOtherStands(t *testing.T) {
	report := FindConflicts(conflictSnapshot(
		[]Rule{capabilityRule("root.allow-recovery", "root.rescue-pritunl",
			EffectAllow, CapabilityPritunlRecovery, "pritunl")},
		[]Rule{capabilityRule("user.deny-recovery", "user.recover-pritunl",
			EffectDeny, CapabilityPritunlRecovery, "pritunl")},
	))
	if !report.Empty() {
		t.Fatalf("a partial rollback was refused by the compiler: %v", report.Codes)
	}
}

// TestSingularThingsStillHaveOneOwner keeps the rule where it was written for.
// A credential, a route and an endpoint are each singular on this host, and two
// domains claiming one denies a fact rather than describing an arrangement.
func TestSingularThingsStillHaveOneOwner(t *testing.T) {
	cases := map[string]EffectiveSnapshot{
		"one credential, two domains": conflictSnapshot(
			[]Rule{credentialRule("root.credential", "root.credential-selector",
				EffectAllow, "synthetic-key", DomainRoot)},
			[]Rule{credentialRule("user.credential", "user.credential-selector",
				EffectAllow, "synthetic-key", DomainUser)},
		),
		"one route, two domains": conflictSnapshot(
			[]Rule{routeRule("root.route", "root.route-selector", "198.51.100.0/24",
				EffectAllow, PathPhysical)},
			[]Rule{routeRule("user.route", "user.route-selector", "198.51.100.0/24",
				EffectAllow, PathPhysical)},
		),
	}
	for name, snapshot := range cases {
		t.Run(name, func(t *testing.T) {
			report := FindConflicts(snapshot)
			if len(report.Codes) != 1 || report.Codes[0] != ConflictCrossDomain {
				t.Fatalf("report = %v, want a cross-domain ownership violation", report.Codes)
			}
		})
	}
}

// TestNothingElseMoved asserts the relaxation is confined to what crosses the
// domain boundary. Inside one domain an action selector claimed twice with
// different effects is still ambiguous, and still refused.
func TestNothingElseMoved(t *testing.T) {
	report := FindConflicts(conflictSnapshot([]Rule{
		capabilityRule("root.allow-recovery", "root.rescue-allow",
			EffectAllow, CapabilityPritunlRecovery, "pritunl"),
		capabilityRule("root.deny-recovery", "root.rescue-deny",
			EffectDeny, CapabilityPritunlRecovery, "pritunl"),
	}, nil))
	if len(report.Codes) != 1 || report.Codes[0] != ConflictActionSemantics {
		t.Fatalf("report = %v, want an action semantics conflict inside one domain", report.Codes)
	}
}

// TestEveryCapabilityTheEnvelopePermitsInBothDomainsCompiles derives its cases
// from the envelope rather than naming them.
//
// The envelope declares what each domain may be authorized for, and a compiler
// that refuses what the envelope permits leaves an authority that can be
// written down, reviewed and signed but never issued. That is precisely what
// happened to Pritunl recovery: defined, permitted, asked for by both runtimes,
// documented in a runbook, and never once compiled.
//
// Deriving the cases is the point. A test naming pritunl_recovery would pass
// while the next two-sided capability failed exactly the same way.
func TestEveryCapabilityTheEnvelopePermitsInBothDomainsCompiles(t *testing.T) {
	envelope := DefaultSafetyEnvelope()
	shared := make([]Capability, 0)
	for _, capability := range envelope.Root.AllowedCapabilities {
		for _, other := range envelope.User.AllowedCapabilities {
			if capability == other {
				shared = append(shared, capability)
			}
		}
	}
	if len(shared) == 0 {
		t.Fatal("the envelope permits no capability in both domains; this test measures nothing")
	}

	for _, capability := range shared {
		for _, target := range envelope.User.AllowedTargets {
			if !containsString(envelope.Root.AllowedTargets, target) {
				continue
			}
			t.Run(string(capability)+" on "+target, func(t *testing.T) {
				report := FindConflicts(conflictSnapshot(
					[]Rule{capabilityRule("root.allow", "root.selector", EffectAllow, capability, target)},
					[]Rule{capabilityRule("user.allow", "user.selector", EffectAllow, capability, target)},
				))
				if !report.Empty() {
					t.Fatalf("the envelope permits %s on %s in both domains, "+
						"but the compiler refuses it: %v", capability, target, report.Codes)
				}
			})
		}
	}
}

// TestTheEnvelopeIsStillTheAuthority asserts the converse. Relaxing the
// cross-domain rule must not let a candidate reach past what the envelope
// permits — the envelope decides, the conflict detector does not second-guess
// it, and neither replaces the other.
func TestTheEnvelopeIsStillTheAuthority(t *testing.T) {
	envelope := DefaultSafetyEnvelope()
	source := OperatorSource{
		Schema: OperatorSourceSchema, PolicySchema: 1,
		BundleGeneration: 2, ParentBundleGeneration: 1,
		IssuedAt: testTime, NotBefore: testTime, ExpiresAt: testExpiry,
		Root: DomainSource{PolicyGeneration: 1},
		User: DomainSource{PolicyGeneration: 1, Rules: []Rule{
			// codex is in root's targets and not in the user's.
			capabilityRule("user.allow", "user.selector", EffectAllow, CapabilityPritunlRecovery, "codex"),
		}},
	}
	digest, err := envelope.SHA256()
	if err != nil {
		t.Fatal(err)
	}
	source.StaticSHA256 = digest
	if err := ValidateAgainstEnvelope(source, envelope); err == nil {
		t.Fatal("a target outside the domain's allowlist was accepted")
	}
}
