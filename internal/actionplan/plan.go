package actionplan

import (
	"errors"
	"regexp"

	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

const (
	PlanSchema = "hexroute.action-plan.v1"
	MaxSteps   = 16
)

type StepKind string

const StepOperatorResume StepKind = "operator_resume"

func (kind StepKind) Valid() bool {
	return kind == StepOperatorResume
}

type InverseKind string

const InverseRestoreControlSnapshot InverseKind = "restore_control_snapshot"

func (kind InverseKind) Valid() bool {
	return kind == InverseRestoreControlSnapshot
}

type InverseSpec struct {
	Kind        InverseKind `json:"kind"`
	InputSHA256 string      `json:"input_sha256"`
}

type StepSpec struct {
	ID            string      `json:"id"`
	Kind          StepKind    `json:"kind"`
	InputSHA256   string      `json:"input_sha256"`
	BeforeSHA256  string      `json:"before_sha256"`
	AppliedSHA256 string      `json:"applied_sha256"`
	Inverse       InverseSpec `json:"inverse"`
}

type Plan struct {
	target string
	steps  []StepSpec
	digest string
}

type canonicalStep struct {
	Position uint32 `json:"position"`
	StepSpec
}

type canonicalPlan struct {
	Schema string          `json:"schema"`
	Target string          `json:"target"`
	Steps  []canonicalStep `json:"steps"`
}

var (
	ErrInvalidPlan = errors.New("invalid action plan")
	targetPattern  = regexp.MustCompile(`^[a-z][a-z0-9_.:-]{0,127}$`)
	stepIDPattern  = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
	digestPattern  = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

func New(target string, steps []StepSpec) (Plan, error) {
	if !targetPattern.MatchString(target) || len(steps) == 0 || len(steps) > MaxSteps {
		return Plan{}, ErrInvalidPlan
	}
	ownedSteps := append([]StepSpec(nil), steps...)
	canonical := canonicalPlan{
		Schema: PlanSchema,
		Target: target,
		Steps:  make([]canonicalStep, len(ownedSteps)),
	}
	seen := make(map[string]struct{}, len(ownedSteps))
	for position, step := range ownedSteps {
		if validateStep(step) != nil {
			return Plan{}, ErrInvalidPlan
		}
		if _, exists := seen[step.ID]; exists {
			return Plan{}, ErrInvalidPlan
		}
		seen[step.ID] = struct{}{}
		canonical.Steps[position] = canonicalStep{
			Position: uint32(position),
			StepSpec: step,
		}
	}
	encoded, err := policy.MarshalCanonical(canonical)
	if err != nil {
		return Plan{}, ErrInvalidPlan
	}
	return Plan{
		target: target,
		steps:  ownedSteps,
		digest: policy.SHA256Hex(encoded),
	}, nil
}

func (plan Plan) Target() string {
	return plan.target
}

func (plan Plan) Digest() string {
	return plan.digest
}

func (plan Plan) Len() int {
	return len(plan.steps)
}

func (plan Plan) Step(position int) (StepSpec, bool) {
	if position < 0 || position >= len(plan.steps) {
		return StepSpec{}, false
	}
	return plan.steps[position], true
}

func (plan Plan) Steps() []StepSpec {
	return append([]StepSpec(nil), plan.steps...)
}

func (plan Plan) valid() bool {
	if !targetPattern.MatchString(plan.target) || !validDigest(plan.digest) ||
		len(plan.steps) == 0 || len(plan.steps) > MaxSteps {
		return false
	}
	rebuilt, err := New(plan.target, plan.steps)
	return err == nil && rebuilt.digest == plan.digest
}

func validateStep(step StepSpec) error {
	if !stepIDPattern.MatchString(step.ID) || !step.Kind.Valid() ||
		!validDigest(step.InputSHA256) || !validDigest(step.BeforeSHA256) ||
		!validDigest(step.AppliedSHA256) || step.BeforeSHA256 == step.AppliedSHA256 ||
		!step.Inverse.Kind.Valid() || !validDigest(step.Inverse.InputSHA256) {
		return ErrInvalidPlan
	}
	return nil
}

func validDigest(value string) bool {
	return digestPattern.MatchString(value)
}
