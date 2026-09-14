package tunnelplan

import (
	"strings"
	"testing"
	"time"
)

// Every cause a decision can name has something behind it in the same decision.
//
// The regression for a record that could not be read on its own: on 2026-09-12
// working out why this rule named six returned links took the connectivity
// archive laid beside the decisions, and the runtime they are compared against
// writes no such store.
func TestEveryActingCauseHasAGround(t *testing.T) {
	previous, observed := steady()
	for _, item := range []struct {
		cause  Cause
		change func(*State, *Observed)
		ground func(Grounds) bool
	}{
		{CauseProcessGone,
			func(_ *State, o *Observed) { o.ProcessRunning = false },
			func(g Grounds) bool { return !g.ProcessRunning }},
		{CauseWakeGap,
			func(_ *State, o *Observed) { o.Slept = 20 * time.Minute },
			func(g Grounds) bool {
				return g.Slept == 20*time.Minute && g.TickGap == 21*time.Minute
			}},
		{CauseCarrierChanged,
			func(_ *State, o *Observed) {
				o.Carrier = NewSignature([]CarriedDestination{{Destination: "198.51.100.9", Interface: "en0"}})
			},
			func(g Grounds) bool { return g.CarrierEntries == 1 && g.Carrier != "" }},
	} {
		t.Run(string(item.cause), func(t *testing.T) {
			state, seen := previous, observed
			item.change(&state, &seen)
			plan, _, err := Decide(policy(), state, seen)
			if err != nil {
				t.Fatalf("Decide: %v", err)
			}
			if !causes(plan)[item.cause] {
				t.Fatalf("the fixture did not reach %q: %v", item.cause, plan.Causes)
			}
			if !item.ground(plan.Grounds) {
				t.Fatalf("%q was named and its ground is not in the decision: %+v", item.cause, plan.Grounds)
			}
		})
	}
}

// A cycle that saw nothing says so, and does not report what it did not see.
func TestAnIncompleteCycleReportsNoCarrier(t *testing.T) {
	previous, observed := steady()
	observed.Complete = false
	plan, _, err := Decide(policy(), previous, observed)
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if plan.Grounds.Complete {
		t.Fatal("an incomplete cycle called itself complete")
	}
	if plan.Grounds.Carrier != "" || plan.Grounds.CarrierEntries != 0 {
		t.Fatalf("an incomplete cycle reported a carrier: %q / %d", plan.Grounds.Carrier, plan.Grounds.CarrierEntries)
	}
}

// A digest identifies a carrier without disclosing it.
func TestTheDigestKeepsTheDestinationsOut(t *testing.T) {
	signature := NewSignature([]CarriedDestination{
		{Destination: "198.51.100.20", Interface: "utun4"},
		{Destination: "203.0.113.20", Interface: "en0"},
		{Destination: "192.0.2.7", Interface: "utun4"},
	})
	digest := signature.Digest()
	if len(digest) != 12 {
		t.Fatalf("the digest is %d characters, want 12", len(digest))
	}
	for _, destination := range []string{"198.51.100.20", "203.0.113.20", "192.0.2.7", "utun4", "en0"} {
		if strings.Contains(digest, destination) {
			t.Fatalf("the digest carries %q", destination)
		}
	}
	if signature.Entries() != 3 {
		t.Fatalf("the signature covers %d entries, want 3", signature.Entries())
	}
	other := NewSignature([]CarriedDestination{
		{Destination: "198.51.100.20", Interface: "en0"},
		{Destination: "203.0.113.20", Interface: "en0"},
		{Destination: "192.0.2.7", Interface: "utun4"},
	})
	if other.Digest() == digest {
		t.Fatal("two different carriers share one digest")
	}
	if Signature("").Digest() != "" {
		t.Fatal("an unobserved carrier was given a digest")
	}
}
