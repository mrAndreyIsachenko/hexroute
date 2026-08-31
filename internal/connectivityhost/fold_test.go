package connectivityhost

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivity"
	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

// A reduction reads more than the accepted facts. A conflict is kept in the
// aggregate state and owes a restatement; a duplicate and a late arrival are
// decisions the stream's integrity depends on. The journal held only what was
// accepted, so any cycle that folded one of the others could not be
// reproduced from it — and the lineage said exactly that, reporting the
// difference as a published conclusion contradicting its own evidence.
//
// On the live host this appeared as seventeen diverged links scattered among
// twenty-five, which is the shape of conflicts and late arrivals landing in
// some cycles and not others.
func TestAFoldedEventIsInTheJournalThatMustReproduceIt(t *testing.T) {
	run := newSoak(t)
	for cycle := 0; cycle < 3; cycle++ {
		run.advance(time.Minute)
		run.flip(uint64(cycle + 1))
		run.cycle()
	}
	before := verifyStore(t, run)
	if before.Reproduced == 0 {
		t.Fatal("no link was verified first, so this measures nothing")
	}

	// The same identity again with different content: a conflict. It is
	// folded, recorded in the snapshot, and never written to the journal.
	run.advance(time.Minute)
	clash := connectivity.FixtureBaseline(connectivity.ComponentUserAccess, 3)
	clash.MonotonicTick = control.Tick(run.tick)
	clash.FreshnessDeadline = control.Tick(run.tick + 3600)
	clash.Lifecycle = connectivity.LifecycleDegraded
	clash.Reason = connectivity.ReasonProbeFailed
	_, raw, err := policy.CanonicalSHA256(clash)
	if err != nil {
		t.Fatal(err)
	}
	report, err := run.reader.PublishUser([]json.RawMessage{raw})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if report.Conflicts == 0 && report.Stale == 0 {
		t.Fatal("nothing non-accepted was folded, so this measures nothing")
	}
	run.cycle()

	after := verifyStore(t, run)
	if after.Diverged > before.Diverged {
		t.Fatalf("folding an event the journal does not hold made %d link(s) "+
			"unreproducible", after.Diverged-before.Diverged)
	}
	if after.Reproduced <= before.Reproduced {
		t.Fatalf("the cycle that folded the conflict produced no link to "+
			"verify (%d then %d)", before.Reproduced, after.Reproduced)
	}
}
