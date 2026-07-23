package control

import "testing"

func TestStateValidity(t *testing.T) {
	for _, state := range []State{
		StateSuspended,
		StateHealthy,
		StateDegraded,
		StateRecovering,
		StateSafeMode,
	} {
		if !state.Valid() {
			t.Fatalf("state %q should be valid", state)
		}
	}
	if State("BROKEN").Valid() {
		t.Fatal("unknown state should be invalid")
	}
}

func TestDependenciesOuterReady(t *testing.T) {
	ready := Dependencies{
		PhysicalNetwork: ReadinessReady,
		Tunnel:          ReadinessReady,
		ScopedRoutes:    ReadinessReady,
	}
	if !ready.OuterReady() {
		t.Fatal("all-ready dependencies should be outer ready")
	}

	ready.Tunnel = ReadinessBlocked
	if ready.OuterReady() {
		t.Fatal("blocked tunnel must make outer readiness false")
	}
}
