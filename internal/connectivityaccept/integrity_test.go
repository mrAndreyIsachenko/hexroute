package connectivityaccept

import (
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivity"
	"github.com/mrAndreyIsachenko/hexroute/internal/safety"
)

// nonBaseline builds an ordinary observation for a component's owner.
func nonBaseline(component connectivity.Component, sequence uint64) connectivity.Fact {
	fact := connectivity.FixtureBaseline(component, sequence)
	fact.Baseline = false
	fact.Reason = connectivity.ReasonProbeSucceeded
	return fact
}

// A hole is numbered against a source, not a component. Closing it therefore
// takes a restatement of everything that source speaks about: a baseline for
// physical_network says what physical_network is now, and says nothing about
// what the hole held for default_path, which the same source also owns.
func TestOneComponentBaselineDoesNotCloseAHoleItCannotAccountFor(t *testing.T) {
	acceptor := New()
	source, domain := connectivity.FixtureSource(connectivity.ComponentPhysicalNetwork)
	declared := safety.ConnectivitySourceComponents(source)
	if len(declared) < 2 {
		t.Fatalf("%q speaks about %d components; this test needs a shared stream",
			source, len(declared))
	}

	if _, err := acceptor.Accept(
		connectivity.FixtureBaseline(connectivity.ComponentPhysicalNetwork, 1),
		domain,
	); err != nil {
		t.Fatalf("seed: %v", err)
	}

	skipped, err := acceptor.Accept(nonBaseline(connectivity.ComponentPhysicalNetwork, 9), domain)
	if err != nil {
		t.Fatalf("skip: %v", err)
	}
	if skipped.OpenedGap == nil {
		t.Fatal("skipping a sequence did not open a hole")
	}
	if len(skipped.Source.PendingBaseline) != len(declared) {
		t.Fatalf("pending %v, want every component of %q", skipped.Source.PendingBaseline, source)
	}

	partial, err := acceptor.Accept(
		connectivity.FixtureBaseline(connectivity.ComponentPhysicalNetwork, 10),
		domain,
	)
	if err != nil {
		t.Fatalf("partial baseline: %v", err)
	}
	if len(partial.Source.Gaps) == 0 || !partial.Source.AwaitingBaseline() {
		t.Fatal("a baseline for one component closed a hole covering another")
	}
	if partial.Reason != ReasonBaselinePending {
		t.Fatalf("reason %q, want baseline_pending", partial.Reason)
	}

	settled, err := acceptor.Accept(
		connectivity.FixtureBaseline(connectivity.ComponentDefaultPath, 11),
		domain,
	)
	if err != nil {
		t.Fatalf("settling baseline: %v", err)
	}
	if len(settled.Source.Gaps) != 0 || settled.Source.AwaitingBaseline() {
		t.Fatalf("the hole survived a complete restatement: %+v", settled.Source)
	}
	if settled.Reason != ReasonBaselineAccepted {
		t.Fatalf("reason %q, want baseline_accepted", settled.Reason)
	}
}

// A source that speaks about exactly one component settles with one baseline.
func TestSingleComponentSourceSettlesWithItsOwnBaseline(t *testing.T) {
	acceptor := New()
	_, domain := connectivity.FixtureSource(connectivity.ComponentRelays)
	if _, err := acceptor.Accept(nonBaseline(connectivity.ComponentRelays, 1), domain); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := acceptor.Accept(nonBaseline(connectivity.ComponentRelays, 4), domain); err != nil {
		t.Fatalf("skip: %v", err)
	}
	settled, err := acceptor.Accept(
		connectivity.FixtureBaseline(connectivity.ComponentRelays, 5),
		domain,
	)
	if err != nil {
		t.Fatalf("baseline: %v", err)
	}
	if len(settled.Source.Gaps) != 0 || settled.Source.AwaitingBaseline() {
		t.Fatalf("a complete restatement did not close the hole: %+v", settled.Source)
	}
}

// Every decision reports the stream it left behind, including the refusals.
func TestEveryOutcomeCarriesTheStreamItLeftBehind(t *testing.T) {
	acceptor := New()
	_, domain := connectivity.FixtureSource(connectivity.ComponentRelays)
	fact := nonBaseline(connectivity.ComponentRelays, 1)
	if _, err := acceptor.Accept(fact, domain); err != nil {
		t.Fatalf("seed: %v", err)
	}

	duplicate, err := acceptor.Accept(fact, domain)
	if err != nil {
		t.Fatalf("duplicate: %v", err)
	}
	if duplicate.Outcome != OutcomeDuplicate || duplicate.Source.LastSequence != 1 {
		t.Fatalf("duplicate lost the stream: %+v", duplicate)
	}

	clash := fact
	clash.Lifecycle = connectivity.LifecycleDegraded
	conflict, err := acceptor.Accept(clash, domain)
	if err != nil {
		t.Fatalf("conflict: %v", err)
	}
	if conflict.Outcome != OutcomeConflict {
		t.Fatalf("outcome %q, want conflict", conflict.Outcome)
	}
	if conflict.Source.Conflicts != 1 || !conflict.Source.AwaitingBaseline() {
		t.Fatalf("a reused identity did not make the stream owe a restatement: %+v",
			conflict.Source)
	}
}

// Dropping holes is a fact about the stream, and it has to survive being
// stored: the checkpoint that carries it is what a restart restores from.
func TestGapOverflowIsReportedAndStaysInsideTheBound(t *testing.T) {
	acceptor := New()
	_, domain := connectivity.FixtureSource(connectivity.ComponentRelays)
	if _, err := acceptor.Accept(nonBaseline(connectivity.ComponentRelays, 1), domain); err != nil {
		t.Fatalf("seed: %v", err)
	}
	sequence := uint64(1)
	var last Acceptance
	for index := 0; index < MaxGapRanges+8; index++ {
		sequence += 2
		acceptance, err := acceptor.Accept(nonBaseline(connectivity.ComponentRelays, sequence), domain)
		if err != nil {
			t.Fatalf("skip %d: %v", sequence, err)
		}
		last = acceptance
	}
	if len(last.Source.Gaps) != MaxGapRanges {
		t.Fatalf("retained %d holes, want the bound of %d", len(last.Source.Gaps), MaxGapRanges)
	}
	if !last.Source.GapOverflow {
		t.Fatal("holes were dropped without saying so")
	}
	if _, err := Restore(acceptor.State()); err != nil {
		t.Fatalf("an overflowed stream cannot be restored: %v", err)
	}
}

// A restored state must be one this acceptor could have produced.
func TestRestoreRejectsAStreamThatIsBothInterruptedAndSettled(t *testing.T) {
	source, _ := connectivity.FixtureSource(connectivity.ComponentRelays)
	state := State{
		HostSequence: 4,
		Sources: map[connectivity.SourceID]*SourceState{
			source: {
				BootID:       connectivity.FixtureBootID,
				LastSequence: 4,
				Gaps:         []GapRange{{From: 2, To: 3}},
			},
		},
	}
	if _, err := Restore(state); err == nil {
		t.Fatal("a stream holding holes while owing nothing was restored")
	}
}

func TestRestoreRejectsABaselineDebtTheSourceCannotOwe(t *testing.T) {
	source, _ := connectivity.FixtureSource(connectivity.ComponentRelays)
	state := State{
		HostSequence: 1,
		Sources: map[connectivity.SourceID]*SourceState{
			source: {
				BootID:          connectivity.FixtureBootID,
				LastSequence:    1,
				PendingBaseline: []connectivity.Component{connectivity.ComponentUserAccess},
			},
		},
	}
	if _, err := Restore(state); err == nil {
		t.Fatal("a source was restored owing a baseline for a component it never speaks about")
	}
}
