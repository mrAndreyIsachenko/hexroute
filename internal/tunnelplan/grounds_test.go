package tunnelplan

import (
	"strings"
	"testing"
	"time"
)

// Every cause a decision can name has something behind it in the same decision.
//
// This is the regression for a record that could not be read. On 2026-09-12 this
// rule recorded six rebuilds for a returned link in half an hour and the link
// had not returned; working that out took the connectivity archive laid beside
// the decisions and matched on time, because that archive — not the decision —
// held the count of endpoints that answered.
//
// The comparison these decisions exist for is against a runtime that writes no
// such store.
func TestEveryCauseHasAGround(t *testing.T) {
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
			func(g Grounds) bool { return g.Slept == 20*time.Minute }},
		{CauseCarrierChanged,
			func(_ *State, o *Observed) {
				o.Carrier = NewSignature([]CarriedDestination{
					{Destination: "198.51.100.9", Interface: "en0"},
				})
			},
			func(g Grounds) bool { return g.CarrierEntries == 1 && g.Carrier != "" }},
		{CauseLinkReturned,
			func(s *State, o *Observed) { s.LinkPresent = false; o.LinkPresent = true },
			func(g Grounds) bool { return g.LinkPresent && g.LinkFailures == 0 }},
		{CausePayloadFailed,
			func(s *State, o *Observed) { s.PayloadFailures = 1; o.PayloadOK = false },
			func(g Grounds) bool { return !g.PayloadOK && g.PayloadFailures >= 2 }},
		{CauseRoutesDrifted,
			func(_ *State, o *Observed) { o.RoutesDrifted = true },
			func(g Grounds) bool { return g.RoutesDrifted }},
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
				t.Fatalf("%q was named and its ground is not in the decision: %+v",
					item.cause, plan.Grounds)
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
		t.Fatalf("an incomplete cycle reported a carrier: %q / %d",
			plan.Grounds.Carrier, plan.Grounds.CarrierEntries)
	}
}

// The payload count reported is the one the cause stood on.
//
// The counter is spent on the decision it caused, so reading it after would say
// nothing stood behind a cause that had two failures behind it.
func TestThePayloadGroundIsTheCountBehindTheCause(t *testing.T) {
	previous, observed := steady()
	previous.PayloadFailures = 1
	observed.PayloadOK = false
	plan, next, err := Decide(policy(), previous, observed)
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if !causes(plan)[CausePayloadFailed] {
		t.Fatalf("the fixture did not reach the payload cause: %v", plan.Causes)
	}
	if plan.Grounds.PayloadFailures != 2 {
		t.Fatalf("the ground says %d failures; the cause stood on 2",
			plan.Grounds.PayloadFailures)
	}
	if next.PayloadFailures != 0 {
		t.Fatalf("the counter was not spent: %d", next.PayloadFailures)
	}
}

// A digest identifies a carrier without disclosing it.
func TestTheDigestKeepsTheDestinationsOut(t *testing.T) {
	// The shape the machine produces: real addresses, several of them.
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
	// Different carriers must be distinguishable, or the digest says nothing.
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
