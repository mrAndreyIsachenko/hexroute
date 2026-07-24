package cloudincident

import (
	"errors"
	"strings"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/event"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/silentnode"
)

const (
	maxCorrelationKeyBytes = 192
	maxEvidencePerSignal   = 16
)

type ConditionState string

const (
	ConditionDetected ConditionState = "detected"
	ConditionCleared  ConditionState = "cleared"
)

type Status string

const (
	StatusOpen         Status = "open"
	StatusAcknowledged Status = "acknowledged"
	StatusResolved     Status = "resolved"
)

type EvidenceRole string

const (
	EvidenceTrigger    EvidenceRole = "trigger"
	EvidenceSupporting EvidenceRole = "supporting"
	EvidenceRecovery   EvidenceRole = "recovery"
	EvidenceExclusion  EvidenceRole = "exclusion"
)

type ResolutionReason string

const (
	ResolutionConditionCleared ResolutionReason = "condition_cleared"
	ResolutionExpectedSleep    ResolutionReason = "expected_sleep"
	ResolutionNodeInactive     ResolutionReason = "node_inactive"
)

type Evidence struct {
	EventID metadata.UUID
	Role    EvidenceRole
}

type Signal struct {
	CorrelationKey   string
	NodeID           metadata.UUID
	Category         event.IncidentCategory
	Component        control.Component
	Severity         event.IncidentSeverity
	RequiresAction   bool
	State            ConditionState
	ResolutionReason ResolutionReason
	ObservedAt       time.Time
	Evidence         []Evidence
}

type Result struct {
	IncidentID metadata.UUID
	Status     Status
	Generation uint64
	Found      bool
	Changed    bool
}

var (
	ErrInvalidSignal    = errors.New("invalid incident signal")
	ErrIncidentConflict = errors.New("incident correlation conflict")
	ErrEvidenceConflict = errors.New("incident evidence conflict")
	ErrIncidentNotFound = errors.New("active incident not found")
)

func SignalFromSilentDecision(decision silentnode.Decision) (Signal, error) {
	if _, err := metadata.ParseUUID(string(decision.NodeID)); err != nil ||
		decision.EvaluatedAt.IsZero() {
		return Signal{}, ErrInvalidSignal
	}
	signal := Signal{
		CorrelationKey: "silent-node:" + string(decision.NodeID),
		NodeID:         decision.NodeID,
		Category:       event.IncidentAvailability,
		Component:      control.ComponentRuntime,
		Severity:       event.SeverityWarning,
		RequiresAction: true,
		ObservedAt:     decision.EvaluatedAt.UTC(),
	}
	switch decision.State {
	case silentnode.StateSilent:
		signal.State = ConditionDetected
		if decision.ReferenceEventID != "" {
			signal.Evidence = []Evidence{{
				EventID: decision.ReferenceEventID,
				Role:    EvidenceTrigger,
			}}
		}
	case silentnode.StateHealthy:
		signal.State = ConditionCleared
		signal.ResolutionReason = ResolutionConditionCleared
		if decision.ReferenceEventID != "" {
			signal.Evidence = []Evidence{{
				EventID: decision.ReferenceEventID,
				Role:    EvidenceRecovery,
			}}
		}
	case silentnode.StateSleeping:
		signal.State = ConditionCleared
		signal.ResolutionReason = ResolutionExpectedSleep
		if decision.SleepEventID != "" {
			signal.Evidence = []Evidence{{
				EventID: decision.SleepEventID,
				Role:    EvidenceExclusion,
			}}
		}
	case silentnode.StateIgnored:
		signal.State = ConditionCleared
		signal.ResolutionReason = ResolutionNodeInactive
	default:
		return Signal{}, ErrInvalidSignal
	}
	if err := validateSignal(signal); err != nil {
		return Signal{}, err
	}
	return signal, nil
}

func validateSignal(signal Signal) error {
	if !validCorrelationKey(signal.CorrelationKey) ||
		signal.ObservedAt.IsZero() ||
		!validCategory(signal.Category) ||
		!validComponent(signal.Component) ||
		!validSeverity(signal.Severity) ||
		len(signal.Evidence) > maxEvidencePerSignal {
		return ErrInvalidSignal
	}
	if signal.NodeID != "" {
		if _, err := metadata.ParseUUID(string(signal.NodeID)); err != nil {
			return ErrInvalidSignal
		}
	}
	switch signal.State {
	case ConditionDetected:
		if signal.ResolutionReason != "" {
			return ErrInvalidSignal
		}
	case ConditionCleared:
		if !validResolutionReason(signal.ResolutionReason) {
			return ErrInvalidSignal
		}
	default:
		return ErrInvalidSignal
	}
	seen := make(map[metadata.UUID]struct{}, len(signal.Evidence))
	for _, evidence := range signal.Evidence {
		if _, err := metadata.ParseUUID(string(evidence.EventID)); err != nil ||
			!validEvidenceForState(signal.State, evidence.Role) {
			return ErrInvalidSignal
		}
		if _, ok := seen[evidence.EventID]; ok {
			return ErrInvalidSignal
		}
		seen[evidence.EventID] = struct{}{}
	}
	return nil
}

func validCorrelationKey(value string) bool {
	if value == "" || len(value) > maxCorrelationKeyBytes {
		return false
	}
	for index, character := range value {
		if (character >= 'a' && character <= 'z') ||
			(character >= '0' && character <= '9') {
			continue
		}
		if index > 0 && strings.ContainsRune(".:_-", character) {
			continue
		}
		return false
	}
	return true
}

func validCategory(value event.IncidentCategory) bool {
	switch value {
	case event.IncidentAvailability,
		event.IncidentRecoveryBudget,
		event.IncidentSpoolOverflow,
		event.IncidentSecurityValidation,
		event.IncidentDeployment:
		return true
	default:
		return false
	}
}

func validComponent(value control.Component) bool {
	switch value {
	case control.ComponentNetwork,
		control.ComponentTunnel,
		control.ComponentRoutes,
		control.ComponentPritunl,
		control.ComponentCodex,
		control.ComponentTelegram,
		control.ComponentRuntime:
		return true
	default:
		return false
	}
}

func validSeverity(value event.IncidentSeverity) bool {
	switch value {
	case event.SeverityInfo, event.SeverityWarning, event.SeverityCritical:
		return true
	default:
		return false
	}
}

func validResolutionReason(value ResolutionReason) bool {
	switch value {
	case ResolutionConditionCleared, ResolutionExpectedSleep, ResolutionNodeInactive:
		return true
	default:
		return false
	}
}

func validEvidenceForState(state ConditionState, role EvidenceRole) bool {
	switch state {
	case ConditionDetected:
		return role == EvidenceTrigger || role == EvidenceSupporting
	case ConditionCleared:
		return role == EvidenceRecovery || role == EvidenceExclusion
	default:
		return false
	}
}
