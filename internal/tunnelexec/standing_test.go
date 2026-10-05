package tunnelexec

import "testing"

func holding() Standing {
	return Standing{Owns: true, RateAllows: true, GrantActive: true, PayloadThreshold: 3}
}

// Each condition ends this runtime's ownership on its own.
func TestEachConditionGivesTheTunnelBack(t *testing.T) {
	if handback := holding().Handback(); handback != "" {
		t.Fatalf("a healthy runtime gave the tunnel back for %q", handback)
	}
	for _, item := range []struct {
		name  string
		alter func(*Standing)
		want  Handback
	}{
		{"the grant lapsed", func(standing *Standing) { standing.GrantActive = false }, HandbackGrantLapsed},
		{"the rate bound", func(standing *Standing) { standing.RateAllows = false }, HandbackRate},
		{"a tunnel carrying nothing", func(standing *Standing) { standing.PayloadFailures = 3 }, HandbackDeadTunnel},
	} {
		t.Run(item.name, func(t *testing.T) {
			standing := holding()
			item.alter(&standing)
			if handback := standing.Handback(); handback != item.want {
				t.Fatalf("handback = %q, want %q", handback, item.want)
			}
		})
	}
}

// A runtime that holds no tunnel gives none back, whatever else is true of it.
func TestARuntimeThatOwnsNothingGivesNothingBack(t *testing.T) {
	standing := holding()
	standing.Owns = false
	standing.GrantActive = false
	standing.RateAllows = false
	standing.PayloadFailures = 9
	if handback := standing.Handback(); handback != "" {
		t.Fatalf("a runtime holding nothing gave back %q", handback)
	}
}

// The payload ends ownership only once it has failed enough times, and only
// where a threshold was configured at all.
func TestThePayloadEndsOwnershipOnlyAtItsThreshold(t *testing.T) {
	standing := holding()
	standing.PayloadFailures = 2
	if handback := standing.Handback(); handback != "" {
		t.Fatalf("two failures of three gave back %q", handback)
	}
	standing.PayloadFailures = 4
	if handback := standing.Handback(); handback != HandbackDeadTunnel {
		t.Fatalf("four failures of three gave %q", handback)
	}
	// No threshold is not a threshold of zero: a runtime configured without one
	// does not hand the tunnel back on the first cycle it ever runs.
	standing.PayloadThreshold = 0
	if handback := standing.Handback(); handback != "" {
		t.Fatalf("an unconfigured threshold gave back %q", handback)
	}
}

// The reason named is the most certain one that holds. A lapsed grant is true
// whatever the network is doing, and a record naming the network instead would
// say the path failed when what failed was the right to touch it.
func TestTheReasonNamedIsTheMostCertainOne(t *testing.T) {
	for _, item := range []struct {
		name     string
		standing Standing
		want     Handback
	}{
		{"lapsed and rate-bound", Standing{Owns: true, PayloadThreshold: 3}, HandbackGrantLapsed},
		{"rate-bound and carrying nothing",
			Standing{Owns: true, GrantActive: true, PayloadThreshold: 3, PayloadFailures: 9}, HandbackRate},
	} {
		t.Run(item.name, func(t *testing.T) {
			if handback := item.standing.Handback(); handback != item.want {
				t.Fatalf("handback = %q, want %q", handback, item.want)
			}
		})
	}
}
