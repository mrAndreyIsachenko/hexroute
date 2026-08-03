package operator

import (
	"github.com/mrAndreyIsachenko/hexroute/internal/actionplan"
	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

func buildOperatorResumePlan(
	target control.Component,
	before control.Snapshot,
	at control.Tick,
) (actionplan.Plan, error) {
	if target == "" || !validSnapshot(before) || before.State != control.StateSafeMode ||
		at < before.LastTick {
		return actionplan.Plan{}, actionplan.ErrInvalidPlan
	}
	after := before
	after.State = control.StateDegraded
	after.Generation++
	after.Attempts = 0
	after.RecoveringSince = 0
	after.NextActionAt = at
	after.SafeUntil = 0
	after.LastTick = at

	beforeDigest, _, err := policy.CanonicalSHA256(before)
	if err != nil {
		return actionplan.Plan{}, actionplan.ErrInvalidPlan
	}
	afterDigest, _, err := policy.CanonicalSHA256(after)
	if err != nil {
		return actionplan.Plan{}, actionplan.ErrInvalidPlan
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
		return actionplan.Plan{}, actionplan.ErrInvalidPlan
	}
	return actionplan.New(string(target), []actionplan.StepSpec{{
		ID:            "operator-resume",
		Kind:          actionplan.StepOperatorResume,
		InputSHA256:   inputDigest,
		BeforeSHA256:  beforeDigest,
		AppliedSHA256: afterDigest,
		Inverse: actionplan.InverseSpec{
			Kind:        actionplan.InverseRestoreControlSnapshot,
			InputSHA256: beforeDigest,
		},
	}})
}
