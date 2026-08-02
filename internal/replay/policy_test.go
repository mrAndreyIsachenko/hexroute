package replay

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

func TestPolicyReplayEvaluatesSyntheticAndObservationCases(t *testing.T) {
	snapshot := replaySnapshot()
	cases := []PolicyCase{
		policyCase("synthetic-user-allow", CaseSyntheticInvariant, policy.DomainUser, "pritunl", DecisionAllow),
		policyCase("observation-root-deny", CaseRedactedObservation, policy.DomainRoot, "routes", DecisionDeny),
	}
	report, err := EvaluatePolicy(snapshot, cases, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Passed || report.SyntheticCases != 1 || report.ObservationCases != 1 || len(report.Violations) != 0 {
		t.Fatalf("unexpected report: %#v", report)
	}
	firstDigest, _ := PolicyReportSHA256(report)
	cases[0], cases[1] = cases[1], cases[0]
	second, err := EvaluatePolicy(snapshot, cases, nil)
	if err != nil {
		t.Fatal(err)
	}
	secondDigest, _ := PolicyReportSHA256(second)
	if firstDigest != secondDigest {
		t.Fatal("case order changed deterministic replay report")
	}
}

func TestPolicyReplayFailureIsBoundedAndRedacted(t *testing.T) {
	report, err := EvaluatePolicy(replaySnapshot(), []PolicyCase{
		policyCase("private-observation", CaseRedactedObservation, policy.DomainUser, "pritunl", DecisionDeny),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if report.Passed || len(report.Violations) != 1 || report.Violations[0].Code != ViolationUnexpectedAllow {
		t.Fatalf("unexpected failure report: %#v", report)
	}
	if RequirePolicyReplay(report) == nil {
		t.Fatal("failed replay must block signing")
	}
	encoded, _ := json.Marshal(report)
	for _, protected := range []string{"pritunl", "private-observation"} {
		if strings.Contains(string(encoded), protected) {
			t.Fatalf("replay report leaked case data %q", protected)
		}
	}
}

func TestPolicyReplayExtendsExistingRootTraceEvaluator(t *testing.T) {
	trace := Trace{Name: "unsafe-restart", Events: []Event{{
		Schema: TraceSchema, Trace: "unsafe-restart", Sequence: 1,
		Component: ComponentRoot, Kind: KindDecision, State: StateRecovering,
		Reason: "process_exit_recovery_allowed", Action: ActionRestartSingBox,
	}}}
	report, err := EvaluatePolicy(replaySnapshot(), []PolicyCase{
		policyCase("synthetic-user-allow", CaseSyntheticInvariant, policy.DomainUser, "pritunl", DecisionAllow),
	}, []Trace{trace})
	if err != nil {
		t.Fatal(err)
	}
	if report.Passed || len(report.Violations) != 1 || report.Violations[0].Code != ViolationRootDivergence {
		t.Fatalf("unexpected root trace report: %#v", report)
	}
}

func TestDecodePolicyCasesRejectsUnknownFieldsAndDuplicateIDs(t *testing.T) {
	valid := `{"schema":"hexroute.policy-replay-case.v1","kind":"synthetic_invariant","id":"case-one","domain":"user","capability":"operator_resume","target":"pritunl","expected":"allow"}`
	decoded, err := DecodePolicyCases(strings.NewReader(valid))
	if err != nil || len(decoded) != 1 {
		t.Fatalf("decode valid case: %v", err)
	}
	if _, err := DecodePolicyCases(strings.NewReader(valid + "\n" + valid)); err == nil {
		t.Fatal("duplicate case ID must be rejected")
	}
	if _, err := DecodePolicyCases(strings.NewReader(strings.TrimSuffix(valid, "}") + `,"path":"private"}`)); err == nil {
		t.Fatal("unknown case field must be rejected")
	}
}

func replaySnapshot() policy.EffectiveSnapshot {
	return policy.EffectiveSnapshot{
		Schema: policy.EffectiveSnapshotSchema, PolicySchema: 1,
		BundleGeneration: 2, ParentBundleGeneration: 1,
		StaticSHA256: strings.Repeat("a", 64),
		IssuedAt:     "2026-08-02T09:00:00Z", NotBefore: "2026-08-02T09:00:00Z", ExpiresAt: "2026-08-02T10:00:00Z",
		Root: policy.DomainPayload{
			Schema: policy.DomainPayloadSchema, Domain: policy.DomainRoot, PolicyGeneration: 1,
			Rules: []policy.Rule{{
				ID: "root.deny-routes", Effect: policy.EffectDeny,
				Selector: policy.Selector{ID: "root.routes", Kind: policy.SelectorAction,
					Action: &policy.ActionSelector{Capability: policy.CapabilityOperatorResume, Target: "routes"}},
			}},
		},
		User: policy.DomainPayload{
			Schema: policy.DomainPayloadSchema, Domain: policy.DomainUser, PolicyGeneration: 2,
			Rules: []policy.Rule{{
				ID: "user.allow-pritunl", Effect: policy.EffectAllow,
				Selector: policy.Selector{ID: "user.pritunl", Kind: policy.SelectorAction,
					Action: &policy.ActionSelector{Capability: policy.CapabilityOperatorResume, Target: "pritunl"}},
			}},
			Leases: []policy.AuthorizationLease{{
				ID: "user.synthetic-lease", Domain: policy.DomainUser, Capability: policy.CapabilityOperatorResume,
				SelectorIDs: []string{"user.pritunl"}, IssuedAt: "2026-08-02T09:00:00Z", ExpiresAt: "2026-08-02T10:00:00Z",
			}},
		},
	}
}

func policyCase(id string, kind PolicyCaseKind, domain policy.Domain, target string, expected PolicyDecision) PolicyCase {
	return PolicyCase{
		Schema: PolicyCaseSchema, Kind: kind, ID: id, Domain: domain,
		Capability: policy.CapabilityOperatorResume, Target: target, Expected: expected,
	}
}
