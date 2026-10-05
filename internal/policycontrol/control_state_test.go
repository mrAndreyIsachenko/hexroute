package policycontrol

import (
	"errors"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

// fixedControlState is a runtime's control state as a test can hold it: one
// generation, or a runtime that cannot read its own.
type fixedControlState struct {
	generation uint64
	err        error
}

func (state fixedControlState) CurrentGeneration() (uint64, error) {
	return state.generation, state.err
}

// The generation an authorization is judged against is this runtime's, not the
// caller's. Before this, the evaluator compared the request with a copy of the
// request, so the only requirement was a number that was not zero.
func TestAnActionIsJudgedAgainstTheRuntimesOwnGeneration(t *testing.T) {
	handler := activeRootHandler(t, grantingTunnelPayload())
	if err := handler.SetControlState(fixedControlState{generation: 7}); err != nil {
		t.Fatal(err)
	}
	digest := policy.SHA256Hex([]byte(tunnelTestPlan))
	if answer := handler.AuthorizeTunnelOwnership("tunnel", 7, digest); !answer.Allowed {
		t.Fatalf("a current generation was refused: %+v", answer)
	}
	// The same request, from a caller whose control state is behind this
	// runtime's, is a generation mismatch however well-formed it is.
	if answer := handler.AuthorizeTunnelOwnership("tunnel", 6, digest); answer.Allowed ||
		answer.Reason != policy.ActionGenerationMismatch {
		t.Fatalf("a stale generation gave %+v", answer)
	}
	// And a caller that names a generation this runtime has not reached.
	if answer := handler.AuthorizeTunnelOwnership("tunnel", 8, digest); answer.Allowed ||
		answer.Reason != policy.ActionGenerationMismatch {
		t.Fatalf("an unreached generation gave %+v", answer)
	}
}

// A runtime that cannot read its own control state refuses, and says which.
func TestAnUnreadableControlStateRefuses(t *testing.T) {
	digest := policy.SHA256Hex([]byte(tunnelTestPlan))

	unreadable := activeRootHandler(t, grantingTunnelPayload())
	if err := unreadable.SetControlState(
		fixedControlState{err: errors.New("the control state could not be read")}); err != nil {
		t.Fatal(err)
	}
	if answer := unreadable.AuthorizeTunnelOwnership("tunnel", 7, digest); answer.Allowed ||
		answer.Reason != policy.ActionControlStateUnreadable {
		t.Fatalf("an unreadable control state gave %+v", answer)
	}

	// A handler that was never given one is the same answer: it has nothing to
	// compare against, and authorizing there is the failure the comparison
	// exists to prevent.
	unwired := activeRootHandler(t, grantingTunnelPayload())
	if answer := unwired.AuthorizeTunnelOwnership("tunnel", 7, digest); answer.Allowed ||
		answer.Reason != policy.ActionControlStateUnreadable {
		t.Fatalf("a handler with no control state gave %+v", answer)
	}
	if err := unwired.SetControlState(nil); err == nil {
		t.Fatal("a nil control state was accepted")
	}
}

// Every action path is judged the same way. They are listed rather than
// abbreviated: the defect this closes was on all three at once, and a test that
// covered one would have gone on passing while the others authorized freely.
func TestEveryActionPathComparesTheRuntimesGeneration(t *testing.T) {
	digest := policy.SHA256Hex([]byte(tunnelTestPlan))
	for _, item := range []struct {
		name string
		ask  func(*Handler, uint64) policy.ActionAuthorizationDecision
	}{
		{"tunnel", func(handler *Handler, generation uint64) policy.ActionAuthorizationDecision {
			return handler.AuthorizeTunnelOwnership("tunnel", generation, digest)
		}},
		{"pritunl recovery", func(handler *Handler, generation uint64) policy.ActionAuthorizationDecision {
			return handler.AuthorizePritunlRecovery(policy.DomainRoot, "pritunl", generation, digest)
		}},
		{"operator resume", func(handler *Handler, generation uint64) policy.ActionAuthorizationDecision {
			return handler.EvaluateOperatorResume(policy.DomainRoot, "tunnel", generation, digest)
		}},
	} {
		t.Run(item.name, func(t *testing.T) {
			handler := activeRootHandler(t, grantingTunnelPayload())
			if err := handler.SetControlState(fixedControlState{generation: 7}); err != nil {
				t.Fatal(err)
			}
			if answer := item.ask(handler, 6); answer.Allowed ||
				answer.Reason != policy.ActionGenerationMismatch {
				t.Fatalf("a stale generation gave %+v", answer)
			}
			unreadable := activeRootHandler(t, grantingTunnelPayload())
			if err := unreadable.SetControlState(
				fixedControlState{err: errors.New("unreadable")}); err != nil {
				t.Fatal(err)
			}
			if answer := item.ask(unreadable, 7); answer.Allowed ||
				answer.Reason != policy.ActionControlStateUnreadable {
				t.Fatalf("an unreadable control state gave %+v", answer)
			}
		})
	}
}
