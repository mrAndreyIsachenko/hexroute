package resumeplan

import (
	"math"

	"github.com/mrAndreyIsachenko/hexroute/internal/actionplan"
	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

type Plan struct {
	action      actionplan.Plan
	before      control.Snapshot
	applied     control.Snapshot
	compensated control.Snapshot
}

func Build(target control.Component, before control.Snapshot, at control.Tick) (Plan, error) {
	if target == "" || !validSnapshot(before) || before.State != control.StateSafeMode ||
		before.Generation > math.MaxUint64-2 || at < before.LastTick {
		return Plan{}, actionplan.ErrInvalidPlan
	}
	applied := before
	applied.State = control.StateDegraded
	applied.Generation++
	applied.Attempts = 0
	applied.RecoveringSince = 0
	applied.NextActionAt = at
	applied.SafeUntil = 0
	applied.LastTick = at
	compensated := before
	compensated.Generation = applied.Generation + 1
	compensated.LastTick = at

	beforeDigest, _, err := policy.CanonicalSHA256(before)
	if err != nil {
		return Plan{}, actionplan.ErrInvalidPlan
	}
	appliedDigest, _, err := policy.CanonicalSHA256(applied)
	if err != nil {
		return Plan{}, actionplan.ErrInvalidPlan
	}
	compensatedDigest, _, err := policy.CanonicalSHA256(compensated)
	if err != nil {
		return Plan{}, actionplan.ErrInvalidPlan
	}
	inputDigest, _, err := policy.CanonicalSHA256(struct {
		Event              control.Event `json:"event"`
		ExpectedGeneration uint64        `json:"expected_generation"`
		At                 control.Tick  `json:"at"`
	}{
		Event:              control.EventOperatorResume,
		ExpectedGeneration: before.Generation,
		At:                 at,
	})
	if err != nil {
		return Plan{}, actionplan.ErrInvalidPlan
	}
	action, err := actionplan.New(string(target), []actionplan.StepSpec{{
		ID:            "operator-resume",
		Kind:          actionplan.StepOperatorResume,
		InputSHA256:   inputDigest,
		BeforeSHA256:  beforeDigest,
		AppliedSHA256: appliedDigest,
		Inverse: actionplan.InverseSpec{
			Kind:           actionplan.InverseRestoreControlSnapshot,
			InputSHA256:    compensatedDigest,
			RestoredSHA256: compensatedDigest,
		},
	}})
	if err != nil {
		return Plan{}, err
	}
	return Plan{
		action: action, before: before, applied: applied, compensated: compensated,
	}, nil
}

func (plan Plan) ActionPlan() actionplan.Plan {
	return plan.action
}

func (plan Plan) Digest() string {
	return plan.action.Digest()
}

func (plan Plan) Before() control.Snapshot {
	return plan.before
}

func (plan Plan) Applied() control.Snapshot {
	return plan.applied
}

func (plan Plan) Compensated() control.Snapshot {
	return plan.compensated
}

func validSnapshot(snapshot control.Snapshot) bool {
	return snapshot.SchemaVersion == control.SnapshotSchemaVersion &&
		snapshot.State.Valid() && snapshot.LastTick >= 0 &&
		snapshot.RecoveringSince >= 0 && snapshot.NextActionAt >= 0 &&
		snapshot.SafeUntil >= 0
}
