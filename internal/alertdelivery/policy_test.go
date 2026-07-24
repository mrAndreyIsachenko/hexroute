package alertdelivery

import (
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/cloudincident"
	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/event"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

const policyIncidentID = metadata.UUID("11111111-1111-4111-8111-111111111111")

func TestPolicySeparatesActionableNightAndMorningDigest(t *testing.T) {
	location := time.FixedZone("MSK", 3*60*60)
	policy := Policy{
		NightStartHour: 23,
		NightEndHour:   8,
		Location:       location,
		LeaseDuration:  time.Minute,
		RetryMinimum:   time.Minute,
		RetryMaximum:   time.Hour,
	}
	night := time.Date(2026, time.July, 25, 2, 0, 0, 0, location)
	base := Snapshot{
		IncidentID:     policyIncidentID,
		Generation:     2,
		Status:         cloudincident.StatusOpen,
		Severity:       event.SeverityWarning,
		Category:       event.IncidentAvailability,
		Component:      control.ComponentTelegram,
		TransitionedAt: night,
	}

	actionable := base
	actionable.RequiresAction = true
	actionPlan, err := policy.Plan(actionable)
	if err != nil {
		t.Fatalf("Plan(actionable) error = %v", err)
	}
	if len(actionPlan) != 2 ||
		actionPlan[0].Channel != ChannelTelegram ||
		actionPlan[0].Status != StatusPending ||
		!actionPlan[0].Actionable ||
		actionPlan[1].Channel != ChannelLocalMacOS {
		t.Fatalf("actionable plan = %+v", actionPlan)
	}

	recovered := base
	recovered.Status = cloudincident.StatusResolved
	recoveryPlan, err := policy.Plan(recovered)
	if err != nil {
		t.Fatalf("Plan(recovered) error = %v", err)
	}
	wantMorning := time.Date(2026, time.July, 25, 8, 0, 0, 0, location).UTC()
	if len(recoveryPlan) != 2 ||
		recoveryPlan[0].Channel != ChannelTelegram ||
		recoveryPlan[0].Status != StatusSuppressed ||
		recoveryPlan[1].Channel != ChannelMorningDigest ||
		!recoveryPlan[1].NextAttemptAt.Equal(wantMorning) {
		t.Fatalf("recovery plan = %+v, morning = %v", recoveryPlan, wantMorning)
	}

	daytime := base
	daytime.TransitionedAt = time.Date(2026, time.July, 25, 12, 0, 0, 0, location)
	dayPlan, err := policy.Plan(daytime)
	if err != nil {
		t.Fatalf("Plan(daytime) error = %v", err)
	}
	if len(dayPlan) != 1 ||
		dayPlan[0].Channel != ChannelTelegram ||
		dayPlan[0].Status != StatusPending ||
		dayPlan[0].Actionable {
		t.Fatalf("day plan = %+v", dayPlan)
	}
}

func TestPolicyRetryDelayIsBounded(t *testing.T) {
	policy := Policy{
		NightStartHour: 23,
		NightEndHour:   8,
		Location:       time.UTC,
		LeaseDuration:  time.Minute,
		RetryMinimum:   time.Minute,
		RetryMaximum:   5 * time.Minute,
	}
	tests := []struct {
		attempt uint32
		want    time.Duration
	}{
		{attempt: 1, want: time.Minute},
		{attempt: 2, want: 2 * time.Minute},
		{attempt: 4, want: 5 * time.Minute},
		{attempt: 1000, want: 5 * time.Minute},
	}
	for _, test := range tests {
		if got := policy.retryDelay(test.attempt); got != test.want {
			t.Fatalf("retryDelay(%d) = %v, want %v", test.attempt, got, test.want)
		}
	}
}
