package actionplan

import (
	"errors"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

func TestPlanIsCanonicalOrderedAndImmutable(t *testing.T) {
	steps := []StepSpec{testStep("resume-a"), testStep("resume-b")}
	plan, err := New("synthetic-target", steps)
	if err != nil {
		t.Fatal(err)
	}
	digest := plan.Digest()
	if !validDigest(digest) || plan.Target() != "synthetic-target" || plan.Len() != 2 {
		t.Fatalf("plan target=%q len=%d digest=%q", plan.Target(), plan.Len(), digest)
	}

	steps[0].ID = "mutated-input"
	returned := plan.Steps()
	returned[0].ID = "mutated-copy"
	stored, ok := plan.Step(0)
	if !ok || stored.ID != "resume-a" || plan.Digest() != digest {
		t.Fatalf("plan changed through caller slice: %+v", stored)
	}

	rebuilt, err := New("synthetic-target", plan.Steps())
	if err != nil || rebuilt.Digest() != digest {
		t.Fatalf("rebuilt digest=%q error=%v", rebuilt.Digest(), err)
	}
	reversed, err := New("synthetic-target", []StepSpec{testStep("resume-b"), testStep("resume-a")})
	if err != nil {
		t.Fatal(err)
	}
	if reversed.Digest() == digest {
		t.Fatal("step order did not affect the immutable plan digest")
	}
}

func TestPlanRejectsInvalidOrAmbiguousSteps(t *testing.T) {
	valid := testStep("resume-a")
	tests := []struct {
		name   string
		target string
		steps  []StepSpec
	}{
		{name: "invalid target", target: "/tmp/live", steps: []StepSpec{valid}},
		{name: "empty", target: "synthetic-target"},
		{name: "duplicate", target: "synthetic-target", steps: []StepSpec{valid, valid}},
		{name: "unsupported operation", target: "synthetic-target", steps: []StepSpec{func() StepSpec {
			step := valid
			step.Kind = StepKind("restart")
			return step
		}()}},
		{name: "no-op", target: "synthetic-target", steps: []StepSpec{func() StepSpec {
			step := valid
			step.AppliedSHA256 = step.BeforeSHA256
			return step
		}()}},
		{name: "invalid inverse", target: "synthetic-target", steps: []StepSpec{func() StepSpec {
			step := valid
			step.Inverse.Kind = InverseKind("stop_process")
			return step
		}()}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := New(test.target, test.steps); !errors.Is(err, ErrInvalidPlan) {
				t.Fatalf("New() error = %v", err)
			}
		})
	}
}

func testStep(id string) StepSpec {
	beforeSHA256 := policy.SHA256Hex([]byte("before-" + id))
	return StepSpec{
		ID:            id,
		Kind:          StepOperatorResume,
		InputSHA256:   policy.SHA256Hex([]byte("input-" + id)),
		BeforeSHA256:  beforeSHA256,
		AppliedSHA256: policy.SHA256Hex([]byte("applied-" + id)),
		Inverse: InverseSpec{
			Kind:           InverseRestoreControlSnapshot,
			InputSHA256:    policy.SHA256Hex([]byte("inverse-" + id)),
			RestoredSHA256: beforeSHA256,
		},
	}
}
