package notification

import (
	"errors"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/event"
)

type ExternalState string

const (
	ExternalNotRequired ExternalState = "not_required"
	ExternalPending     ExternalState = "pending"
	ExternalDelivered   ExternalState = "delivered"
)

type Template string

const (
	TemplateAccessContinuity Template = "access_continuity"
	TemplatePritunlSafeMode  Template = "pritunl_safe_mode"
	TemplateTelegramCluster  Template = "telegram_cluster"
	TemplateSecurityFailure  Template = "security_failure"
	TemplateRuntimeFailure   Template = "runtime_failure"
	TemplateExternalPending  Template = "external_pending"
)

type DecisionReason string

const (
	ReasonActionRequired      DecisionReason = "action_required"
	ReasonExternalPending     DecisionReason = "external_pending"
	ReasonNightRecoveryDigest DecisionReason = "night_recovery_digest"
	ReasonNonActionable       DecisionReason = "non_actionable"
)

type Input struct {
	Incident event.Incident
	External ExternalState
}

type Decision struct {
	LocalImmediate  bool
	MorningDigest   bool
	ExternalPending bool
	Template        Template
	Reason          DecisionReason
}

type Policy struct {
	NightStartHour uint8
	NightEndHour   uint8
}

var ErrInvalidNotificationInput = errors.New("invalid notification input")

func (policy Policy) Decide(input Input, at time.Time) (Decision, error) {
	if !validPolicy(policy) ||
		at.IsZero() ||
		!validExternalState(input.External) {
		return Decision{}, ErrInvalidNotificationInput
	}
	if _, err := event.Encode(event.SchemaIncident, input.Incident); err != nil {
		return Decision{}, ErrInvalidNotificationInput
	}

	decision := Decision{
		ExternalPending: input.External == ExternalPending,
		Reason:          ReasonNonActionable,
	}
	if input.Incident.Status == event.IncidentResolved {
		if policy.isNight(at) {
			decision.MorningDigest = true
			decision.Reason = ReasonNightRecoveryDigest
		}
		return decision, nil
	}
	if !actionable(input.Incident) {
		return decision, nil
	}

	decision.LocalImmediate = true
	decision.Template = templateFor(input)
	decision.Reason = ReasonActionRequired
	if input.External == ExternalPending &&
		input.Incident.Severity == event.SeverityCritical {
		decision.Reason = ReasonExternalPending
	}
	return decision, nil
}

func validPolicy(policy Policy) bool {
	return policy.NightStartHour < 24 &&
		policy.NightEndHour < 24 &&
		policy.NightStartHour != policy.NightEndHour
}

func validExternalState(state ExternalState) bool {
	switch state {
	case ExternalNotRequired, ExternalPending, ExternalDelivered:
		return true
	default:
		return false
	}
}

func (policy Policy) isNight(at time.Time) bool {
	hour := uint8(at.Hour())
	if policy.NightStartHour < policy.NightEndHour {
		return hour >= policy.NightStartHour && hour < policy.NightEndHour
	}
	return hour >= policy.NightStartHour || hour < policy.NightEndHour
}

func actionable(incident event.Incident) bool {
	if incident.Severity == event.SeverityCritical {
		return true
	}
	switch incident.Category {
	case event.IncidentRecoveryBudget,
		event.IncidentSecurityValidation:
		return true
	default:
		return false
	}
}

func templateFor(input Input) Template {
	if input.External == ExternalPending &&
		input.Incident.Severity == event.SeverityCritical {
		return TemplateExternalPending
	}
	switch {
	case input.Incident.Category == event.IncidentRecoveryBudget &&
		input.Incident.Component == control.ComponentPritunl:
		return TemplatePritunlSafeMode
	case input.Incident.Category == event.IncidentSecurityValidation:
		return TemplateSecurityFailure
	case input.Incident.Component == control.ComponentTelegram:
		return TemplateTelegramCluster
	case input.Incident.Component == control.ComponentNetwork,
		input.Incident.Component == control.ComponentTunnel,
		input.Incident.Component == control.ComponentRoutes,
		input.Incident.Component == control.ComponentCodex:
		return TemplateAccessContinuity
	default:
		return TemplateRuntimeFailure
	}
}
