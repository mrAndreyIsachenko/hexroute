package connectivityreduce

import (
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivity"
	"github.com/mrAndreyIsachenko/hexroute/internal/control"
)

// The three classifications below describe a host the diff cannot speak
// confidently about. They were reachable and never asserted, which is the
// wrong half of the diff to leave unpinned: a qualification chain that records
// them is recording values nothing has ever checked.

// A component whose owner has gone quiet is stale, not missing. The
// distinction matters: missing invites restoring something, stale says the
// host does not currently know.
func TestSilentOwnerIsClassifiedStaleNotMissing(t *testing.T) {
	h := newHarness(t)
	h.reduceManaged(h.offer(baselineFacts()...), managedPolicy())

	output, err := Reduce(Input{
		Prior: h.snapshot, Policy: activePolicy(), PolicyComponents: managedPolicy(),
		BootID: connectivity.FixtureBootID, EvaluationTick: evaluationTick + 400,
	})
	if err != nil {
		t.Fatalf("reduce: %v", err)
	}
	entry := classFor(output.Diff, connectivity.ComponentRelays)
	if entry.Classification != ClassStale {
		t.Fatalf("classification %q, want %q", entry.Classification, ClassStale)
	}
	if entry.Reason != DiffReasonStaleObservation {
		t.Fatalf("reason %q, want %q", entry.Reason, DiffReasonStaleObservation)
	}
}

// A component nobody has ever spoken about is unknown. DNS is the standing
// case: it has a declared owner and no collector, so it must never be reported
// as converged or missing on the strength of silence.
func TestNeverObservedComponentIsClassifiedUnknown(t *testing.T) {
	h := newHarness(t)
	// Every component except DNS restates itself.
	facts := make([]connectivity.Fact, 0)
	for _, fact := range baselineFacts() {
		if fact.Component != connectivity.ComponentDNS {
			facts = append(facts, fact)
		}
	}
	output := h.reduceManaged(h.offer(facts...), managedPolicy())

	entry := classFor(output.Diff, connectivity.ComponentDNS)
	if entry.Classification != ClassUnknown {
		t.Fatalf("classification %q, want %q", entry.Classification, ClassUnknown)
	}
	if entry.Reason != DiffReasonNotObserved {
		t.Fatalf("reason %q, want %q", entry.Reason, DiffReasonNotObserved)
	}
	// Silence is not evidence of absence, and must not propose a change.
	for _, proposal := range output.Proposals {
		if proposal.Target == connectivity.ComponentDNS &&
			proposal.Class != ProposalObserve {
			t.Fatalf("an unobserved component proposes %q", proposal.Class)
		}
	}
}

// A component whose stream refused to resolve a disagreement is a conflict,
// and a conflict is never resolved by picking one side in the diff.
func TestUnresolvedConflictIsClassifiedConflict(t *testing.T) {
	h := newHarness(t)
	h.reduceManaged(h.offer(baselineFacts()...), managedPolicy())

	fresh := connectivity.FixtureBaseline(connectivity.ComponentRelays, 20)
	fresh.Baseline = false
	fresh.Reason = connectivity.ReasonProbeSucceeded
	fresh.FreshnessDeadline = control.Tick(evaluationTick + 10_000)
	h.reduceManaged(h.offer(fresh), managedPolicy())

	clash := fresh
	clash.Lifecycle = connectivity.LifecycleFailed
	clash.Reason = connectivity.ReasonProbeFailed
	output := h.reduceManaged(h.offer(clash), managedPolicy())

	entry := classFor(output.Diff, connectivity.ComponentRelays)
	if entry.Classification != ClassConflict {
		t.Fatalf("classification %q, want %q", entry.Classification, ClassConflict)
	}
	if entry.Reason != DiffReasonOwnerConflict {
		t.Fatalf("reason %q, want %q", entry.Reason, DiffReasonOwnerConflict)
	}
}

// The guard is only worth something while every classification the reducer can
// produce is asserted somewhere. A new one must not join silently.
func TestEveryClassificationIsAsserted(t *testing.T) {
	all := []Classification{
		ClassConverged, ClassMissing, ClassUnexpected, ClassDivergent,
		ClassStale, ClassUnknown, ClassConflict, ClassGrandfathered,
	}
	for _, class := range all {
		if class == "" {
			t.Fatal("a classification lost its value")
		}
	}
	if len(all) != 8 {
		t.Fatalf("%d classifications listed; the diff contract changed", len(all))
	}
}
