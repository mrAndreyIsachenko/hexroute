package tunnelexec

import "testing"

func open() Gates {
	return Gates{Owns: true, Asked: true, Authorized: true, RateAllows: true}
}

// Every gate stops the act on its own, and says which one did.
func TestEachGateStopsTheActAndNamesItself(t *testing.T) {
	if blocked := open().Blocked(); blocked != "" {
		t.Fatalf("every gate open gave %q", blocked)
	}
	if !open().Allowed() {
		t.Fatal("every gate open was not allowed")
	}
	for _, item := range []struct {
		name  string
		alter func(*Gates)
		want  Block
	}{
		{"no claim", func(gates *Gates) { gates.Owns = false }, BlockedNotOwned},
		{"suspended", func(gates *Gates) { gates.Suspended = true }, BlockedSuspended},
		{"nothing to ask with", func(gates *Gates) { gates.Asked = false }, BlockedUnasked},
		{"no grant", func(gates *Gates) { gates.Authorized = false }, BlockedUnauthorized},
		{"rate bound", func(gates *Gates) { gates.RateAllows = false }, BlockedRate},
	} {
		t.Run(item.name, func(t *testing.T) {
			gates := open()
			item.alter(&gates)
			if blocked := gates.Blocked(); blocked != item.want {
				t.Fatalf("blocked = %q, want %q", blocked, item.want)
			}
			if gates.Allowed() {
				t.Fatal("a closed gate allowed the act")
			}
		})
	}
}

// A record names the gate a reader should look at first. A machine that was
// merely asleep must not be reported as unauthorized, and a runtime that owns
// nothing must not be reported for a rate it never reached.
func TestTheGateNamedIsTheLeastSpecificOneThatHolds(t *testing.T) {
	for _, item := range []struct {
		name  string
		gates Gates
		want  Block
	}{
		{"asleep without the tunnel", Gates{Suspended: true}, BlockedNotOwned},
		{"nothing at all", Gates{}, BlockedNotOwned},
		{"asleep and unauthorized", Gates{Owns: true, Suspended: true}, BlockedSuspended},
		{"unasked and rate-bound", Gates{Owns: true}, BlockedUnasked},
		{"unauthorized and rate-bound", Gates{Owns: true, Asked: true}, BlockedUnauthorized},
		// An answer nobody was given is not a refusal. A runtime that never put
		// the question is in no position to say what the grant would have said,
		// and a record that named the grant sends the reader to the policy for a
		// runtime that had no state to ask with.
		{"an answer without a question", Gates{Owns: true, Authorized: true, RateAllows: true},
			BlockedUnasked},
	} {
		t.Run(item.name, func(t *testing.T) {
			if blocked := item.gates.Blocked(); blocked != item.want {
				t.Fatalf("blocked = %q, want %q", blocked, item.want)
			}
		})
	}
}
