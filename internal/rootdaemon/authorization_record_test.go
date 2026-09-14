package rootdaemon

import (
	"strings"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/event"

	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/tunnelplan"
)

type answeringAuthority struct {
	decision   policy.ActionAuthorizationDecision
	asked      int
	target     string
	generation uint64
	digest     string
}

// It keeps every argument. The first version of this fake discarded the
// generation and the digest, returned a policy-shaped refusal, and so passed
// while the runtime sent zero and an empty string — a request the real
// evaluator rejects before consulting policy.
func (authority *answeringAuthority) AuthorizeTunnelOwnership(
	target string,
	generation uint64,
	digest string,
) policy.ActionAuthorizationDecision {
	authority.asked++
	authority.target = target
	authority.generation = generation
	authority.digest = digest
	return authority.decision
}

// A decision that would act says whether it would have been allowed.
//
// Deciding and being permitted are different questions, and a record answering
// only the first cannot show the second was ever asked. Under the generation
// active while this was written the answer is a refusal — and recording the
// refusal is what proves the question reaches the policy handler.
func TestARecordSaysWhetherTheDecisionWouldBeAllowed(t *testing.T) {
	authority := &answeringAuthority{decision: policy.ActionAuthorizationDecision{
		Reason: policy.ActionSelectorMismatch,
	}}
	record, asked := recordFor(t, tunnelplan.ActionRebuildTunnel, authority)
	if asked != 1 {
		t.Fatalf("a decision to act asked policy %d times, want once", asked)
	}
	if authority.target != tunnelAuthorityTarget {
		t.Fatalf("policy was asked about %q, want %q",
			authority.target, tunnelAuthorityTarget)
	}
	if record.Authorization == nil {
		t.Fatal("a decision to act was recorded without saying whether it was allowed")
	}
	if record.Authorization.Allowed {
		t.Fatal("a refusal was recorded as an authorization")
	}
	if record.Authorization.Reason != string(policy.ActionSelectorMismatch) {
		t.Fatalf("reason = %q, want the handler's own",
			record.Authorization.Reason)
	}
}

// A cycle that decided to do nothing asks nothing: there is nothing to permit.
func TestADecisionToDoNothingAsksNoPermission(t *testing.T) {
	authority := &answeringAuthority{decision: policy.ActionAuthorizationDecision{
		Allowed: true, Reason: policy.ActionAuthorized,
	}}
	record, asked := recordFor(t, tunnelplan.ActionNone, authority)
	if asked != 0 {
		t.Fatalf("a decision to do nothing asked policy %d times", asked)
	}
	if record.Authorization != nil {
		t.Fatal("a decision to do nothing recorded an authorization")
	}
}

// A runtime with no policy control records no answer rather than a refusal.
//
// A typed nil inside an interface is not nil, and asking through one would get
// "invalid request" back — which reads in the record as policy refusing rather
// than as policy being absent.
func TestNoPolicyControlRecordsNoAnswer(t *testing.T) {
	record := tunnelDecisionRecord(planFor(tunnelplan.ActionRebuildTunnel), 1, nil, 7)
	if record.Authorization != nil {
		t.Fatal("a record carried an authorization nobody answered")
	}
	if tunnelAuthorityOf(nil) != nil {
		t.Fatal("a missing handler became an authorizer that answers")
	}
}

func planFor(action tunnelplan.Action) tunnelplan.Plan {
	causes := []tunnelplan.Cause{tunnelplan.CauseProcessGone}
	if action == tunnelplan.ActionNone {
		causes = nil
	}
	return tunnelplan.Plan{
		Action: action, Causes: causes,
		Grounds: tunnelplan.Grounds{Complete: true, ProcessRunning: false},
	}
}

// recordFor builds the record the daemon would store, by the same call.
func recordFor(
	t *testing.T,
	action tunnelplan.Action,
	authority *answeringAuthority,
) (event.TunnelDecision, int) {
	t.Helper()
	return tunnelDecisionRecord(planFor(action), 1, authority, 7), authority.asked
}

// The question is one the evaluator will actually consider.
//
// A request without a control-state generation or a plan digest is rejected as
// malformed before any policy is read. Sent that way, every answer is
// `invalid_request` whatever the active generation grants, which is what the
// archive held on 2026-09-14.
func TestTheQuestionCarriesAGenerationAndADigest(t *testing.T) {
	authority := &answeringAuthority{decision: policy.ActionAuthorizationDecision{
		Reason: policy.ActionSelectorMismatch,
	}}
	tunnelDecisionRecord(planFor(tunnelplan.ActionRebuildTunnel), 1, authority, 7)
	if authority.generation != 7 {
		t.Fatalf("asked under control generation %d, want 7", authority.generation)
	}
	if len(authority.digest) != 64 || strings.Trim(authority.digest, "0123456789abcdef") != "" {
		t.Fatalf("plan digest %q is not a SHA-256 the evaluator accepts", authority.digest)
	}
}

// With no control state yet, nothing is asked and no refusal is recorded.
//
// The first cycle records its decision before the operator snapshot has a
// generation. Asking then would write `invalid_request` into the record, which
// reads as policy refusing when nothing could be asked.
func TestNoControlStateAsksNothing(t *testing.T) {
	authority := &answeringAuthority{decision: policy.ActionAuthorizationDecision{
		Reason: policy.ActionSelectorMismatch,
	}}
	record := tunnelDecisionRecord(planFor(tunnelplan.ActionRebuildTunnel), 1, authority, 0)
	if authority.asked != 0 {
		t.Fatalf("asked policy %d times with no control state", authority.asked)
	}
	if record.Authorization != nil {
		t.Fatal("recorded an answer to a question that was not asked")
	}
}

// The digest is this decision's, not the act's in general.
func TestTheDigestBindsTheDecision(t *testing.T) {
	rebuild := tunnelPlanDigest(planFor(tunnelplan.ActionRebuildTunnel), 7)
	later := tunnelPlanDigest(planFor(tunnelplan.ActionRebuildTunnel), 8)
	other := tunnelPlanDigest(tunnelplan.Plan{
		Action: tunnelplan.ActionRebuildTunnel,
		Causes: []tunnelplan.Cause{tunnelplan.CauseRoutesDrifted},
	}, 7)
	if rebuild == later {
		t.Fatal("the digest does not change with the control generation")
	}
	if rebuild == other {
		t.Fatal("the digest does not change with the causes")
	}
}
