package alertdelivery

import (
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/cloudincident"
	"github.com/mrAndreyIsachenko/hexroute/internal/event"
)

type Policy struct {
	NightStartHour uint8
	NightEndHour   uint8
	Location       *time.Location
	LeaseDuration  time.Duration
	RetryMinimum   time.Duration
	RetryMaximum   time.Duration
}

func (policy Policy) Validate() error {
	if policy.NightStartHour >= 24 ||
		policy.NightEndHour >= 24 ||
		policy.NightStartHour == policy.NightEndHour ||
		policy.Location == nil ||
		policy.LeaseDuration < time.Minute ||
		policy.LeaseDuration > 10*time.Minute ||
		policy.RetryMinimum < time.Minute ||
		policy.RetryMaximum < policy.RetryMinimum ||
		policy.RetryMaximum > 24*time.Hour {
		return ErrInvalidPolicy
	}
	return nil
}

func (policy Policy) Plan(snapshot Snapshot) ([]PlanItem, error) {
	if err := policy.Validate(); err != nil {
		return nil, ErrInvalidPolicy
	}
	if err := validateSnapshot(snapshot); err != nil {
		return nil, err
	}
	actionable := snapshotActionable(snapshot)
	if actionable {
		return []PlanItem{
			{
				Channel:       ChannelTelegram,
				Status:        StatusPending,
				Actionable:    true,
				NextAttemptAt: snapshot.TransitionedAt.UTC(),
			},
			{
				Channel:    ChannelLocalMacOS,
				Status:     StatusPending,
				Actionable: true,
			},
		}, nil
	}
	if policy.isNight(snapshot.TransitionedAt) {
		return []PlanItem{
			{
				Channel:        ChannelTelegram,
				Status:         StatusSuppressed,
				LastResultCode: "night_suppressed",
			},
			{
				Channel:       ChannelMorningDigest,
				Status:        StatusPending,
				NextAttemptAt: policy.nextMorning(snapshot.TransitionedAt),
			},
		}, nil
	}
	return []PlanItem{{
		Channel:       ChannelTelegram,
		Status:        StatusPending,
		NextAttemptAt: snapshot.TransitionedAt.UTC(),
	}}, nil
}

func (policy Policy) isNight(at time.Time) bool {
	hour := uint8(at.In(policy.Location).Hour())
	if policy.NightStartHour < policy.NightEndHour {
		return hour >= policy.NightStartHour && hour < policy.NightEndHour
	}
	return hour >= policy.NightStartHour || hour < policy.NightEndHour
}

func (policy Policy) nextMorning(at time.Time) time.Time {
	local := at.In(policy.Location)
	candidate := time.Date(
		local.Year(),
		local.Month(),
		local.Day(),
		int(policy.NightEndHour),
		0,
		0,
		0,
		policy.Location,
	)
	if !candidate.After(local) {
		candidate = candidate.AddDate(0, 0, 1)
	}
	return candidate.UTC()
}

func (policy Policy) retryDelay(attempt uint32) time.Duration {
	delay := policy.RetryMinimum
	for count := uint32(1); count < attempt && delay < policy.RetryMaximum; count++ {
		if delay > policy.RetryMaximum/2 {
			return policy.RetryMaximum
		}
		delay *= 2
	}
	if delay > policy.RetryMaximum {
		return policy.RetryMaximum
	}
	return delay
}

func snapshotActionable(snapshot Snapshot) bool {
	if snapshot.Status == cloudincident.StatusResolved {
		return false
	}
	if snapshot.RequiresAction || snapshot.Severity == event.SeverityCritical {
		return true
	}
	return snapshot.Category == event.IncidentRecoveryBudget ||
		snapshot.Category == event.IncidentSecurityValidation
}
