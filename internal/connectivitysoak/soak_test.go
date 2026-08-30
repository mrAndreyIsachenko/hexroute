package connectivitysoak

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivitycheckpoint"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivitytrace"
)

func scratch(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "soak")
}

// The whole point of the catalogue is that these run. Before this package the
// thirteen faults were thirteen paragraphs.
func TestEveryCanonicalTraceInjectsAndMatches(t *testing.T) {
	traces, err := connectivitytrace.Canonical()
	if err != nil {
		t.Fatalf("catalogue: %v", err)
	}
	if len(traces) != len(connectivitytrace.Faults()) {
		t.Fatalf("catalogue holds %d traces for %d faults",
			len(traces), len(connectivitytrace.Faults()))
	}
	for _, trace := range traces {
		t.Run(string(trace.Fault), func(t *testing.T) {
			outcome, err := Run(trace, scratch(t))
			if err != nil {
				t.Fatalf("inject: %v", err)
			}
			if !outcome.Matched {
				t.Fatalf("%s did not produce what it said it would: %s",
					trace.Fault, outcome.Mismatch)
			}
			if outcome.GuessedHealthy {
				t.Fatalf("%s left the model looking untroubled", trace.Fault)
			}
			if outcome.TraceSHA256 == "" || outcome.Observation.CheckpointID == "" {
				t.Fatalf("%s produced nothing to bind a result to", trace.Fault)
			}
		})
	}
}

// A runner that reported every run as a match would be worth nothing, and
// would look exactly like this one from the outside.
func TestAWrongExpectationIsReportedRatherThanAccepted(t *testing.T) {
	trace, err := connectivitytrace.For(connectivitytrace.FaultGap)
	if err != nil {
		t.Fatalf("catalogue: %v", err)
	}
	none := uint16(0)
	trace.Expectation.Assert.OpenGaps = &none

	outcome, err := Run(trace, scratch(t))
	if err != nil {
		t.Fatalf("inject: %v", err)
	}
	if outcome.Matched {
		t.Fatal("a gap trace claiming no gap was reported as a match")
	}
	if outcome.Mismatch == "" {
		t.Fatal("a failed claim was not named")
	}
}

// A damaging fault the model shrugs off is the one result that voids a run,
// and it must not be reachable by writing an assertion the shrug satisfies.
func TestAnUntroubledModelIsCaughtEvenWhenEveryClaimHolds(t *testing.T) {
	trace, err := connectivitytrace.For(connectivitytrace.FaultDuplicate)
	if err != nil {
		t.Fatalf("catalogue: %v", err)
	}
	// A duplicate disturbs nothing, which is correct for a duplicate. Calling
	// it damaging is a lie about the fault, and the runner must notice that
	// the model came back clean rather than take the passing claims as proof.
	trace.Expectation.Damaging = true

	outcome, err := Run(trace, scratch(t))
	if err != nil {
		t.Fatalf("inject: %v", err)
	}
	if !outcome.Matched {
		t.Fatalf("the duplicate trace stopped matching its own claims: %s", outcome.Mismatch)
	}
	if !outcome.GuessedHealthy {
		t.Fatal("a fault that had to leave a mark left none, and it went unreported")
	}
}

// A soak that opened an existing store would be describing that store as much
// as the fault, and on this machine the nearest existing store is the running
// daemon's.
func TestASoakRefusesToInheritALineage(t *testing.T) {
	root := scratch(t)
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "occupied.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	trace, err := connectivitytrace.For(connectivitytrace.FaultDuplicate)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Run(trace, root); err == nil {
		t.Fatal("a soak ran against a root that already held something")
	}
}

func TestARelativeRootIsRefused(t *testing.T) {
	trace, err := connectivitytrace.For(connectivitytrace.FaultDuplicate)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Run(trace, "soak"); err == nil {
		t.Fatal("a relative scratch root was accepted")
	}
}

// The store faults are injected by editing files. If the store's layout moved
// and the search stopped finding them, every store trace would still pass:
// nothing was damaged, so nothing was refused, and a lineage that resumes
// cleanly looks like a lineage that survived. This is that test.
func TestAnUndamagedLineageOfTheSameShapeResumesLatest(t *testing.T) {
	trace, err := connectivitytrace.For(connectivitytrace.FaultCheckpointCorruption)
	if err != nil {
		t.Fatal(err)
	}
	root := scratch(t)
	session, err := newSession(trace, root)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := session.play(); err != nil {
		t.Fatalf("play: %v", err)
	}
	resume, err := session.reopen(trace.BootID)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if resume.Status != connectivitycheckpoint.ResumeLatest {
		t.Fatalf("an undamaged lineage resumed %s; the store traces prove nothing",
			resume.Status)
	}

	damaged, err := Run(trace, scratch(t))
	if err != nil {
		t.Fatalf("inject: %v", err)
	}
	if damaged.Observation.Resume == connectivitycheckpoint.ResumeLatest {
		t.Fatal("the damage was not applied; the injector found nothing to damage")
	}
}

// Every store fault must reach a different verdict from an intact lineage, or
// the catalogue holds four names for one test.
func TestEachStoreFaultIsRefusedOnItsOwnTerms(t *testing.T) {
	seen := make(map[string]connectivitytrace.Fault)
	for _, fault := range connectivitytrace.Faults() {
		trace, err := connectivitytrace.For(fault)
		if err != nil {
			t.Fatal(err)
		}
		if trace.Layer != connectivitytrace.LayerStore {
			continue
		}
		outcome, err := Run(trace, scratch(t))
		if err != nil {
			t.Fatalf("%s: %v", fault, err)
		}
		verdict := string(outcome.Observation.Resume) + "/" +
			string(outcome.Observation.ResumeReason) + "/" +
			string(rune('0'+outcome.Observation.ResumeDepth))
		if other, clash := seen[verdict]; clash {
			t.Fatalf("%s and %s are refused identically (%s); one of them is untested",
				fault, other, verdict)
		}
		seen[verdict] = fault
	}
	if len(seen) < 4 {
		t.Fatalf("only %d store faults ran", len(seen))
	}
}
