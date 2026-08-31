package connectivityhost

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivity"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityreduce"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityview"
)

func viewWith(
	component connectivity.Component,
	class connectivityreduce.ProposalClass,
) connectivityview.LocalStatus {
	status := connectivityview.LocalStatus{BootID: connectivity.FixtureBootID}
	for _, each := range connectivity.Components() {
		entry := connectivityview.LocalComponent{Component: each}
		if each == component {
			entry.ProposalClass = class
		} else {
			entry.ProposalClass = connectivityreduce.ProposalObserve
		}
		status.Components = append(status.Components, entry)
	}
	return status
}

func comparisonFor(
	t *testing.T,
	comparison Comparison,
	component connectivity.Component,
) ComponentComparison {
	t.Helper()
	for _, entry := range comparison.Components {
		if entry.Component == component {
			return entry
		}
	}
	t.Fatalf("%s is missing from the comparison", component)
	return ComponentComparison{}
}

// Every configured component appears. One missing would be indistinguishable
// from one both planners agreed to leave alone.
func TestComparisonCoversEveryComponent(t *testing.T) {
	comparison := Compare(
		viewWith(connectivity.ComponentScopedRoutes, connectivityreduce.ProposalObserve),
		nil, connectivity.FixtureBootID, 3, true)
	if len(comparison.Components) != len(connectivity.Components()) {
		t.Fatalf("%d components, want %d",
			len(comparison.Components), len(connectivity.Components()))
	}
}

// The direction that matters most before mutation authority exists: the
// component planner would act and the read model would not.
func TestPlannerActingAloneIsRecorded(t *testing.T) {
	comparison := Compare(
		viewWith(connectivity.ComponentScopedRoutes, connectivityreduce.ProposalObserve),
		[]PlannerIntent{{
			Component: connectivity.ComponentScopedRoutes, Establish: true,
		}},
		connectivity.FixtureBootID, 3, true)
	entry := comparisonFor(t, comparison, connectivity.ComponentScopedRoutes)
	if entry.Agreement != AgreementPlannerOnly {
		t.Fatalf("agreement %q, want planner_only", entry.Agreement)
	}
	if comparison.Divergent != 0 {
		t.Fatalf("one-sided intent counted as divergence")
	}
}

// Opposite directions are the divergence a soak is watching for.
func TestOppositeDirectionsAreDivergent(t *testing.T) {
	comparison := Compare(
		viewWith(connectivity.ComponentScopedRoutes, connectivityreduce.ProposalWithdraw),
		[]PlannerIntent{{
			Component: connectivity.ComponentScopedRoutes, Establish: true,
		}},
		connectivity.FixtureBootID, 3, true)
	entry := comparisonFor(t, comparison, connectivity.ComponentScopedRoutes)
	if entry.Agreement != AgreementDivergent {
		t.Fatalf("agreement %q, want divergent", entry.Agreement)
	}
	if comparison.Divergent != 1 {
		t.Fatalf("divergent count %d, want 1", comparison.Divergent)
	}
}

// Establish and reconcile both mean "make this exist", so they agree with a
// planner that would establish.
func TestReconcileAgreesWithEstablish(t *testing.T) {
	comparison := Compare(
		viewWith(connectivity.ComponentScopedRoutes, connectivityreduce.ProposalReconcile),
		[]PlannerIntent{{
			Component: connectivity.ComponentScopedRoutes, Establish: true,
		}},
		connectivity.FixtureBootID, 3, true)
	if comparisonFor(t, comparison, connectivity.ComponentScopedRoutes).Agreement !=
		AgreementBoth {
		t.Fatal("reconcile and establish were not counted as the same direction")
	}
}

// Observe is what the model says when it cannot say what should be. Treating
// it as agreement with a silent planner would hide model uncertainty behind
// planner confidence.
func TestObserveIsNotTreatedAsAgreementToAct(t *testing.T) {
	comparison := Compare(
		viewWith(connectivity.ComponentRelays, connectivityreduce.ProposalObserve),
		nil, connectivity.FixtureBootID, 3, true)
	if comparisonFor(t, comparison, connectivity.ComponentRelays).Agreement !=
		AgreementNeither {
		t.Fatal("observe against silence was not recorded as neither acting")
	}
}

// A soak lasts 72 hours. A file that appends an identical line every cycle
// measures elapsed time, not disagreement.
func TestRecorderWritesOnlyWhenTheCorrelationChanges(t *testing.T) {
	recorder, err := OpenRecorder(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	quiet := Compare(
		viewWith(connectivity.ComponentScopedRoutes, connectivityreduce.ProposalObserve),
		nil, connectivity.FixtureBootID, 1, true)

	wrote, err := recorder.Record(quiet)
	if err != nil || !wrote {
		t.Fatalf("first record = %v, %v", wrote, err)
	}
	// The same correlation at a later snapshot generation is not news.
	later := quiet
	later.SnapshotGeneration = 9
	wrote, err = recorder.Record(later)
	if err != nil || wrote {
		t.Fatalf("an unchanged correlation was written again: %v, %v", wrote, err)
	}
	changed := Compare(
		viewWith(connectivity.ComponentScopedRoutes, connectivityreduce.ProposalWithdraw),
		[]PlannerIntent{{
			Component: connectivity.ComponentScopedRoutes, Establish: true,
		}},
		connectivity.FixtureBootID, 10, true)
	wrote, err = recorder.Record(changed)
	if err != nil || !wrote {
		t.Fatalf("a divergence was not written: %v, %v", wrote, err)
	}
	if recorder.Written() != 2 {
		t.Fatalf("wrote %d comparisons, want 2", recorder.Written())
	}
}

// The record has to survive the process, or a 72-hour soak answers nothing
// afterwards.
func TestRecordedComparisonsSurviveTheProcess(t *testing.T) {
	root := t.TempDir()
	recorder, err := OpenRecorder(root)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := recorder.Record(Compare(
		viewWith(connectivity.ComponentRelays, connectivityreduce.ProposalEstablish),
		nil, connectivity.FixtureBootID, 1, true)); err != nil {
		t.Fatalf("record: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, comparisonFile))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("nothing was written to disk")
	}

	// Surviving the process means the next process finds it, not only that
	// the bytes are there. A recorder that opened blind would count from zero
	// and write this same comparison again because it could not remember
	// writing it — which over a soak's restarts is a duplicate line and an
	// under-reported total in the file the soak is studied from.
	resumed, err := OpenRecorder(root)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if resumed.Written() != 1 {
		t.Fatalf("a reopened recorder counted %d over one recorded comparison",
			resumed.Written())
	}
	wrote, err := resumed.Record(Compare(
		viewWith(connectivity.ComponentRelays, connectivityreduce.ProposalEstablish),
		nil, connectivity.FixtureBootID, 1, true))
	if err != nil {
		t.Fatalf("record after reopen: %v", err)
	}
	if wrote {
		t.Fatal("a reopened recorder wrote the comparison it had already written")
	}
}

// A disabled recorder costs the record, never the daemon.
func TestNilRecorderIsSilentAndHarmless(t *testing.T) {
	var recorder *Recorder
	wrote, err := recorder.Record(Comparison{})
	if err != nil || wrote {
		t.Fatalf("nil recorder = %v, %v", wrote, err)
	}
	if recorder.Full() || recorder.Written() != 0 {
		t.Fatal("a nil recorder reported state")
	}
}
