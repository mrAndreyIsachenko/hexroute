package slo

import (
	"errors"
	"strings"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

type Granularity string

const (
	GranularityHour Granularity = "hour"
	GranularityDay  Granularity = "day"
)

type Service string

const (
	ServiceTwilight Service = "twilight_transport"
	ServiceCodex    Service = "codex_fallback"
	ServicePritunl  Service = "pritunl"
	ServiceTelegram Service = "telegram"
)

const (
	ObjectiveTwilightAvailability = "availability_99_9"
	ObjectiveCodexActivation      = "activation_within_30s"
	ObjectivePritunlThreeMinutes  = "recovery_within_3m"
	ObjectivePritunlTenMinutes    = "recovery_within_10m"
	ObjectiveTelegramAvailability = "cluster_availability_99_9"
	ObjectiveTelegramFailover     = "client_failover_within_60s"

	CodexDeadline            = 30 * time.Second
	TelegramFailoverDeadline = 60 * time.Second
	PritunlThreeMinutes      = 3 * time.Minute
	PritunlTenMinutes        = 10 * time.Minute
	maxTargetKeyBytes        = 192
	maxCalculationPoints     = 4096
)

type LinkRole string

const (
	LinkFailure   LinkRole = "failure"
	LinkExclusion LinkRole = "exclusion"
	LinkRecovery  LinkRole = "recovery"
)

type Request struct {
	Granularity Granularity
	TargetKey   string
	NodeID      metadata.UUID
	WindowStart time.Time
	WindowEnd   time.Time
	ComputedAt  time.Time
}

type IncidentLink struct {
	IncidentID metadata.UUID
	Role       LinkRole
}

type Aggregate struct {
	AggregateID          metadata.UUID
	Granularity          Granularity
	TargetKey            string
	NodeID               metadata.UUID
	Service              Service
	Objective            string
	WindowStart          time.Time
	WindowEnd            time.Time
	EligibleMilliseconds int64
	GoodMilliseconds     int64
	BadMilliseconds      int64
	ExcludedMilliseconds int64
	QualifyingCount      uint64
	TotalCount           uint64
	ComputedAt           time.Time
	Links                []IncidentLink
}

type TwilightState struct {
	At               time.Time
	Awake            bool
	CarrierAvailable bool
	TransportReady   bool
	IncidentID       metadata.UUID
}

type TelegramState struct {
	At                 time.Time
	ReachableProviders uint16
	HealthyProxies     uint16
	IncidentID         metadata.UUID
}

type RecoveryAttempt struct {
	StartedAt          time.Time
	RecoveredAt        *time.Time
	EligibilityEndedAt *time.Time
	IncidentID         metadata.UUID
}

var ErrInvalidSLO = errors.New("invalid SLO calculation")

func validateRequest(request Request, nodeRequired bool) error {
	request.WindowStart = request.WindowStart.UTC()
	request.WindowEnd = request.WindowEnd.UTC()
	request.ComputedAt = request.ComputedAt.UTC()
	if !validTargetKey(request.TargetKey) ||
		request.WindowStart.IsZero() ||
		request.WindowEnd.IsZero() ||
		!request.WindowEnd.After(request.WindowStart) ||
		request.ComputedAt.IsZero() ||
		request.ComputedAt.Before(request.WindowEnd) {
		return ErrInvalidSLO
	}
	switch request.Granularity {
	case GranularityHour:
		if request.WindowEnd.Sub(request.WindowStart) != time.Hour ||
			request.WindowStart.Minute() != 0 ||
			request.WindowStart.Second() != 0 ||
			request.WindowStart.Nanosecond() != 0 {
			return ErrInvalidSLO
		}
	case GranularityDay:
		if request.WindowEnd.Sub(request.WindowStart) != 24*time.Hour ||
			request.WindowStart.Hour() != 0 ||
			request.WindowStart.Minute() != 0 ||
			request.WindowStart.Second() != 0 ||
			request.WindowStart.Nanosecond() != 0 {
			return ErrInvalidSLO
		}
	default:
		return ErrInvalidSLO
	}
	if nodeRequired && !validUUID(request.NodeID) {
		return ErrInvalidSLO
	}
	if !nodeRequired && request.NodeID != "" && !validUUID(request.NodeID) {
		return ErrInvalidSLO
	}
	return nil
}

func validateAggregate(aggregate Aggregate) error {
	request := Request{
		Granularity: aggregate.Granularity,
		TargetKey:   aggregate.TargetKey,
		NodeID:      aggregate.NodeID,
		WindowStart: aggregate.WindowStart,
		WindowEnd:   aggregate.WindowEnd,
		ComputedAt:  aggregate.ComputedAt,
	}
	nodeRequired := aggregate.Service != ServiceTelegram
	if validateRequest(request, nodeRequired) != nil ||
		(aggregate.AggregateID != "" &&
			!validUUID(aggregate.AggregateID)) ||
		!validService(aggregate.Service) ||
		!validObjective(aggregate.Service, aggregate.Objective) ||
		aggregate.EligibleMilliseconds < 0 ||
		aggregate.GoodMilliseconds < 0 ||
		aggregate.BadMilliseconds < 0 ||
		aggregate.ExcludedMilliseconds < 0 ||
		aggregate.GoodMilliseconds+aggregate.BadMilliseconds >
			aggregate.EligibleMilliseconds ||
		aggregate.QualifyingCount > aggregate.TotalCount ||
		aggregate.TotalCount > maxCalculationPoints ||
		len(aggregate.Links) > maxCalculationPoints {
		return ErrInvalidSLO
	}
	seen := make(map[IncidentLink]struct{}, len(aggregate.Links))
	for _, link := range aggregate.Links {
		if !validUUID(link.IncidentID) || !validLinkRole(link.Role) {
			return ErrInvalidSLO
		}
		if _, duplicate := seen[link]; duplicate {
			return ErrInvalidSLO
		}
		seen[link] = struct{}{}
	}
	return nil
}

func validTargetKey(value string) bool {
	if value == "" || len(value) > maxTargetKeyBytes {
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

func validService(service Service) bool {
	return service == ServiceTwilight ||
		service == ServiceCodex ||
		service == ServicePritunl ||
		service == ServiceTelegram
}

func validObjective(service Service, objective string) bool {
	switch service {
	case ServiceTwilight:
		return objective == ObjectiveTwilightAvailability
	case ServiceCodex:
		return objective == ObjectiveCodexActivation
	case ServicePritunl:
		return objective == ObjectivePritunlThreeMinutes ||
			objective == ObjectivePritunlTenMinutes
	case ServiceTelegram:
		return objective == ObjectiveTelegramAvailability ||
			objective == ObjectiveTelegramFailover
	default:
		return false
	}
}

func validLinkRole(role LinkRole) bool {
	return role == LinkFailure ||
		role == LinkExclusion ||
		role == LinkRecovery
}

func validUUID(value metadata.UUID) bool {
	_, err := metadata.ParseUUID(string(value))
	return err == nil
}
