package policy

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSemanticDiffClassifiesAuthorizationChanges(t *testing.T) {
	current := composedTestSnapshot(t)
	candidate := composedTestSnapshot(t)
	candidate.BundleGeneration = 3
	candidate.ParentBundleGeneration = 2
	candidate.User.PolicyGeneration = 3
	candidate.User.Rules[0].Effect = EffectDeny

	report, err := BuildSemanticDiff(&current, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Entries) != 1 || report.Entries[0].Classification != DiffNewlyDenied ||
		report.AuthorizationExpansion {
		t.Fatalf("unexpected deny diff: %#v", report)
	}

	candidate = composedTestSnapshot(t)
	candidate.BundleGeneration = 3
	candidate.ParentBundleGeneration = 2
	candidate.Root.PolicyGeneration = 2
	candidate.Root.Rules = nil
	report, err = BuildSemanticDiff(&current, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Entries) != 1 || report.Entries[0].Classification != DiffNewlyAllowed ||
		!report.AuthorizationExpansion {
		t.Fatalf("removing deny must be highlighted as expansion: %#v", report)
	}
}

func TestSemanticDiffClassifiesChangedPlanWithoutLeakingSelector(t *testing.T) {
	current := conflictSnapshot([]Rule{
		endpointRule("root.endpoint", "root.endpoint-selector", "gateway.example.test", EffectAllow, TLSRequired, PathPhysical, ProtocolTCP),
	}, nil)
	candidate := conflictSnapshot([]Rule{
		endpointRule("root.endpoint", "root.endpoint-selector", "gateway.example.test", EffectAllow, TLSRequired, PathManagedTUN, ProtocolTCP),
	}, nil)
	candidate.BundleGeneration = 3
	candidate.ParentBundleGeneration = 2
	candidate.Root.PolicyGeneration = 2

	report, err := BuildSemanticDiff(&current, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Entries) != 1 || report.Entries[0].Classification != DiffChangedPlan ||
		!report.Entries[0].Expansion {
		t.Fatalf("unexpected changed plan: %#v", report)
	}
	encoded, err := json.Marshal(report)
	if err != nil || strings.Contains(string(encoded), "gateway.example.test") ||
		strings.Contains(string(encoded), "managed_tun") {
		t.Fatal("redacted report must contain digests, not selector values")
	}
}

func TestSemanticDiffIsDeterministic(t *testing.T) {
	candidate := conflictSnapshot([]Rule{
		actionRule("root.first", "root.first-selector", EffectAllow, "shared"),
		routeRule("root.second", "root.second-selector", "192.0.2.0/24", EffectDeny, PathPhysical),
	}, nil)
	first, err := BuildSemanticDiff(nil, candidate)
	if err != nil {
		t.Fatal(err)
	}
	candidate.Root.Rules[0], candidate.Root.Rules[1] = candidate.Root.Rules[1], candidate.Root.Rules[0]
	second, err := BuildSemanticDiff(nil, candidate)
	if err != nil {
		t.Fatal(err)
	}
	firstDigest, _ := SemanticDiffSHA256(first)
	secondDigest, _ := SemanticDiffSHA256(second)
	if firstDigest != secondDigest {
		t.Fatal("rule order changed semantic diff digest")
	}
}
