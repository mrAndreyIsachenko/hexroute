package resumeplan

import (
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/actionplan"
	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

func TestBuildOnlyClearsTargetBudgetIntoDegraded(t *testing.T) {
	before := safeModeSnapshot()
	plan, err := Build(control.ComponentPritunl, before, 100)
	if err != nil {
		t.Fatal(err)
	}
	wantApplied := before
	wantApplied.State = control.StateDegraded
	wantApplied.Generation++
	wantApplied.Attempts = 0
	wantApplied.RecoveringSince = 0
	wantApplied.NextActionAt = 100
	wantApplied.LastTick = 100
	wantApplied.SafeUntil = 0
	wantCompensated := before
	wantCompensated.Generation = wantApplied.Generation + 1
	wantCompensated.LastTick = 100
	if plan.Before() != before || !reflect.DeepEqual(plan.Applied(), wantApplied) {
		t.Fatalf("before=%+v applied=%+v", plan.Before(), plan.Applied())
	}
	if !reflect.DeepEqual(plan.Compensated(), wantCompensated) ||
		plan.Compensated().Generation <= plan.Applied().Generation {
		t.Fatalf("compensated=%+v", plan.Compensated())
	}

	action := plan.ActionPlan()
	step, ok := action.Step(0)
	if action.Target() != string(control.ComponentPritunl) || action.Len() != 1 || !ok ||
		step.Kind != actionplan.StepOperatorResume ||
		step.Inverse.Kind != actionplan.InverseRestoreControlSnapshot {
		t.Fatalf("action plan target=%q len=%d step=%+v", action.Target(), action.Len(), step)
	}
	beforeDigest, _, err := policy.CanonicalSHA256(before)
	if err != nil {
		t.Fatal(err)
	}
	appliedDigest, _, err := policy.CanonicalSHA256(wantApplied)
	if err != nil {
		t.Fatal(err)
	}
	compensatedDigest, _, err := policy.CanonicalSHA256(wantCompensated)
	if err != nil {
		t.Fatal(err)
	}
	if step.BeforeSHA256 != beforeDigest || step.AppliedSHA256 != appliedDigest ||
		step.Inverse.InputSHA256 != compensatedDigest ||
		step.Inverse.RestoredSHA256 != compensatedDigest ||
		plan.Digest() != action.Digest() {
		t.Fatalf("step digests = %+v plan=%q", step, plan.Digest())
	}
}

func TestBuildRejectsAnythingOutsideOperatorResumePreconditions(t *testing.T) {
	for _, mutate := range []func(*control.Snapshot, *control.Tick){
		func(snapshot *control.Snapshot, _ *control.Tick) { snapshot.State = control.StateHealthy },
		func(snapshot *control.Snapshot, at *control.Tick) { *at = snapshot.LastTick - 1 },
		func(snapshot *control.Snapshot, _ *control.Tick) { snapshot.Generation = math.MaxUint64 },
	} {
		before := safeModeSnapshot()
		at := control.Tick(100)
		mutate(&before, &at)
		if _, err := Build(control.ComponentPritunl, before, at); !errors.Is(
			err,
			actionplan.ErrInvalidPlan,
		) {
			t.Fatalf("Build() error = %v", err)
		}
	}
}

func TestBuildIsDeterministicAndGenerationBound(t *testing.T) {
	before := safeModeSnapshot()
	first, err := Build(control.ComponentPritunl, before, 100)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Build(control.ComponentPritunl, before, 100)
	if err != nil {
		t.Fatal(err)
	}
	changed := before
	changed.Generation++
	third, err := Build(control.ComponentPritunl, changed, 100)
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest() != second.Digest() || first.Digest() == third.Digest() {
		t.Fatalf("digests first=%q second=%q changed=%q", first.Digest(), second.Digest(), third.Digest())
	}
}

func safeModeSnapshot() control.Snapshot {
	return control.Snapshot{
		SchemaVersion:       control.SnapshotSchemaVersion,
		Generation:          7,
		State:               control.StateSafeMode,
		ConsecutiveFailures: 5,
		Attempts:            3,
		LastTick:            90,
		RecoveringSince:     80,
		NextActionAt:        95,
		SafeUntil:           700,
	}
}
