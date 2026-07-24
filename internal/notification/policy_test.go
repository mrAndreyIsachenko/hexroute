package notification

import (
	"errors"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/event"
)

func TestPolicyNotifiesOnlyActionableIncidents(t *testing.T) {
	policy := Policy{NightStartHour: 23, NightEndHour: 8}
	night := time.Date(2026, 7, 25, 2, 0, 0, 0, time.FixedZone("MSK", 3*60*60))

	tests := []struct {
		name     string
		input    Input
		local    bool
		digest   bool
		pending  bool
		template Template
		reason   DecisionReason
	}{
		{
			name: "critical access incident with external delivery pending",
			input: Input{
				Incident: incident(
					event.IncidentOpened,
					event.SeverityCritical,
					event.IncidentAvailability,
					control.ComponentTunnel,
				),
				External: ExternalPending,
			},
			local:    true,
			pending:  true,
			template: TemplateExternalPending,
			reason:   ReasonExternalPending,
		},
		{
			name: "Pritunl recovery budget warning",
			input: Input{
				Incident: incident(
					event.IncidentUpdated,
					event.SeverityWarning,
					event.IncidentRecoveryBudget,
					control.ComponentPritunl,
				),
				External: ExternalDelivered,
			},
			local:    true,
			template: TemplatePritunlSafeMode,
			reason:   ReasonActionRequired,
		},
		{
			name: "one Telegram provider unavailable",
			input: Input{
				Incident: incident(
					event.IncidentOpened,
					event.SeverityWarning,
					event.IncidentAvailability,
					control.ComponentTelegram,
				),
				External: ExternalNotRequired,
			},
			reason: ReasonNonActionable,
		},
		{
			name: "automatic night recovery",
			input: Input{
				Incident: incident(
					event.IncidentResolved,
					event.SeverityInfo,
					event.IncidentAvailability,
					control.ComponentPritunl,
				),
				External: ExternalNotRequired,
			},
			digest: true,
			reason: ReasonNightRecoveryDigest,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision, err := policy.Decide(test.input, night)
			if err != nil {
				t.Fatalf("Decide() error: %v", err)
			}
			if decision.LocalImmediate != test.local ||
				decision.MorningDigest != test.digest ||
				decision.ExternalPending != test.pending ||
				decision.Template != test.template ||
				decision.Reason != test.reason {
				t.Fatalf("Decide() = %+v", decision)
			}
		})
	}
}

func TestPolicySuppressesDaytimeResolutionWithoutDigest(t *testing.T) {
	policy := Policy{NightStartHour: 23, NightEndHour: 8}
	day := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	decision, err := policy.Decide(Input{
		Incident: incident(
			event.IncidentResolved,
			event.SeverityInfo,
			event.IncidentAvailability,
			control.ComponentTunnel,
		),
		External: ExternalNotRequired,
	}, day)
	if err != nil {
		t.Fatalf("Decide() error: %v", err)
	}
	if decision.LocalImmediate ||
		decision.MorningDigest ||
		decision.Reason != ReasonNonActionable {
		t.Fatalf("Decide() = %+v", decision)
	}
}

func TestPolicyRejectsInvalidIncidentExternalStateAndWindow(t *testing.T) {
	valid := Input{
		Incident: incident(
			event.IncidentOpened,
			event.SeverityCritical,
			event.IncidentAvailability,
			control.ComponentTunnel,
		),
		External: ExternalPending,
	}
	now := time.Now()
	tests := []struct {
		policy Policy
		input  Input
		at     time.Time
	}{
		{
			policy: Policy{NightStartHour: 8, NightEndHour: 8},
			input:  valid,
			at:     now,
		},
		{
			policy: Policy{NightStartHour: 23, NightEndHour: 8},
			input: Input{
				Incident: valid.Incident,
				External: ExternalState("raw-endpoint"),
			},
			at: now,
		},
		{
			policy: Policy{NightStartHour: 23, NightEndHour: 8},
			input: Input{
				Incident: event.Incident{
					IncidentID: "invalid",
				},
				External: ExternalPending,
			},
			at: now,
		},
	}
	for _, test := range tests {
		if _, err := test.policy.Decide(test.input, test.at); !errors.Is(
			err,
			ErrInvalidNotificationInput,
		) {
			t.Fatalf("Decide() error = %v, want %v", err, ErrInvalidNotificationInput)
		}
	}
}

func incident(
	status event.IncidentStatus,
	severity event.IncidentSeverity,
	category event.IncidentCategory,
	component control.Component,
) event.Incident {
	return event.Incident{
		IncidentID: "incident-synthetic",
		Status:     status,
		Severity:   severity,
		Category:   category,
		Component:  component,
		Generation: 4,
	}
}
