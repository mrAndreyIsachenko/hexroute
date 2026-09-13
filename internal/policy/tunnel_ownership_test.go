package policy

import (
	"testing"
	"time"
)

// Owning the tunnel is grantable to root and to no other domain.
//
// The user domain holds a keychain and a one-time code. A generation that could
// grant it the tunnel would let an operator write down, and sign, that the
// runtime holding the credentials may restart the network — and the only thing
// standing between that and the machine is this refusal.
//
// It is proved through the compiler rather than against the envelope, because
// the last capability added here was defined, permitted, asked for and written
// into a runbook without ever being compiled: every part was proven and the path
// between them was not.
func TestTheTunnelCapabilityIsRefusedToTheUserDomain(t *testing.T) {
	source := tunnelGrantSource(t)
	source.User.Rules = append(source.User.Rules, capabilityRule(
		"user.allow-tunnel", "user.own-tunnel",
		EffectAllow, CapabilityTunnelOwnership, "tunnel"))
	source.User.Leases = append(source.User.Leases, AuthorizationLease{
		ID: "user.lease-tunnel", Domain: DomainUser,
		Capability:  CapabilityTunnelOwnership,
		SelectorIDs: []string{"user.own-tunnel"},
		IssuedAt:    testTime, ExpiresAt: testExpiry,
	})

	if _, err := ComposeEffectiveSnapshot(source, DefaultSafetyEnvelope()); err == nil {
		t.Fatal("a generation granting the tunnel to the user domain compiled")
	}
}

// And it compiles for root, or the grant could never be made at all.
func TestTheTunnelCapabilityCompilesForRoot(t *testing.T) {
	snapshot, err := ComposeEffectiveSnapshot(
		tunnelGrantSource(t), DefaultSafetyEnvelope())
	if err != nil {
		t.Fatalf("a generation granting the tunnel to root was refused: %v", err)
	}

	// What the compiler emitted must authorize the thing it was written for,
	// through the evaluator the runtime asks rather than a payload by hand.
	decision := EvaluateActionAuthorization(
		activeStateFor(snapshot.Root),
		ActionAuthorizationRequest{
			Domain: DomainRoot, Capability: CapabilityTunnelOwnership,
			BundleGeneration:       2,
			DomainPolicyGeneration: snapshot.Root.PolicyGeneration,
			ControlStateGeneration: 7, Target: "tunnel", PlanSHA256: testDigest,
		},
		insideTheWindow(t),
	)
	if !decision.Allowed {
		t.Fatalf("the compiled grant does not authorize the tunnel: %q",
			decision.Reason)
	}
}

// A generation that does not carry it refuses, and says which question failed.
func TestWithoutTheGrantTheAnswerIsARefusal(t *testing.T) {
	snapshot, err := ComposeEffectiveSnapshot(grantSource(t), DefaultSafetyEnvelope())
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	decision := EvaluateActionAuthorization(
		activeStateFor(snapshot.Root),
		ActionAuthorizationRequest{
			Domain: DomainRoot, Capability: CapabilityTunnelOwnership,
			BundleGeneration:       2,
			DomainPolicyGeneration: snapshot.Root.PolicyGeneration,
			ControlStateGeneration: 7, Target: "tunnel", PlanSHA256: testDigest,
		},
		insideTheWindow(t),
	)
	if decision.Allowed {
		t.Fatal("a generation that never granted the tunnel authorized it")
	}
	if decision.Reason == ActionAuthorized {
		t.Fatal("a refusal reported itself as an authorization")
	}
}

// tunnelGrantSource is the generation the cutover will need, written as an
// operator would: one capability, root alone, one selector and one lease.
func tunnelGrantSource(t *testing.T) OperatorSource {
	t.Helper()
	source := grantSource(t)
	source.Root.Rules = append(source.Root.Rules, capabilityRule(
		"root.allow-tunnel", "root.own-tunnel",
		EffectAllow, CapabilityTunnelOwnership, "tunnel"))
	source.Root.Leases = append(source.Root.Leases, AuthorizationLease{
		ID: "root.lease-tunnel", Domain: DomainRoot,
		Capability:  CapabilityTunnelOwnership,
		SelectorIDs: []string{"root.own-tunnel"},
		IssuedAt:    testTime, ExpiresAt: testExpiry,
	})
	return source
}

// insideTheWindow is a moment the fixture generations are valid at.
func insideTheWindow(t *testing.T) time.Time {
	t.Helper()
	at, err := time.Parse(time.RFC3339, "2026-08-02T09:30:00Z")
	if err != nil {
		t.Fatal(err)
	}
	return at
}
