package slo

import (
	"sort"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

type availabilityPoint struct {
	at         time.Time
	eligible   bool
	healthy    bool
	incidentID metadata.UUID
}

func CalculateTwilight(
	request Request,
	states []TwilightState,
) (Aggregate, error) {
	if validateRequest(request, true) != nil ||
		len(states) == 0 ||
		len(states) > maxCalculationPoints {
		return Aggregate{}, ErrInvalidSLO
	}
	points := make([]availabilityPoint, 0, len(states))
	for _, state := range states {
		points = append(points, availabilityPoint{
			at:         state.At,
			eligible:   state.Awake && state.CarrierAvailable,
			healthy:    state.TransportReady,
			incidentID: state.IncidentID,
		})
	}
	return calculateAvailability(
		request,
		ServiceTwilight,
		ObjectiveTwilightAvailability,
		points,
	)
}

func CalculateTelegram(
	request Request,
	states []TelegramState,
) (Aggregate, error) {
	if validateRequest(request, false) != nil ||
		len(states) == 0 ||
		len(states) > maxCalculationPoints {
		return Aggregate{}, ErrInvalidSLO
	}
	points := make([]availabilityPoint, 0, len(states))
	for _, state := range states {
		if state.HealthyProxies > state.ReachableProviders {
			return Aggregate{}, ErrInvalidSLO
		}
		points = append(points, availabilityPoint{
			at:         state.At,
			eligible:   state.ReachableProviders > 0,
			healthy:    state.HealthyProxies > 0,
			incidentID: state.IncidentID,
		})
	}
	return calculateAvailability(
		request,
		ServiceTelegram,
		ObjectiveTelegramAvailability,
		points,
	)
}

func CalculateCodex(
	request Request,
	attempts []RecoveryAttempt,
) (Aggregate, error) {
	return calculateRecovery(
		request,
		ServiceCodex,
		ObjectiveCodexActivation,
		CodexDeadline,
		true,
		attempts,
	)
}

func CalculatePritunl(
	request Request,
	attempts []RecoveryAttempt,
) ([2]Aggregate, error) {
	threeMinutes, err := calculateRecovery(
		request,
		ServicePritunl,
		ObjectivePritunlThreeMinutes,
		PritunlThreeMinutes,
		true,
		attempts,
	)
	if err != nil {
		return [2]Aggregate{}, err
	}
	tenMinutes, err := calculateRecovery(
		request,
		ServicePritunl,
		ObjectivePritunlTenMinutes,
		PritunlTenMinutes,
		true,
		attempts,
	)
	if err != nil {
		return [2]Aggregate{}, err
	}
	return [2]Aggregate{threeMinutes, tenMinutes}, nil
}

func CalculateTelegramFailover(
	request Request,
	attempts []RecoveryAttempt,
) (Aggregate, error) {
	return calculateRecovery(
		request,
		ServiceTelegram,
		ObjectiveTelegramFailover,
		TelegramFailoverDeadline,
		false,
		attempts,
	)
}

func calculateAvailability(
	request Request,
	service Service,
	objective string,
	points []availabilityPoint,
) (Aggregate, error) {
	for index := range points {
		points[index].at = points[index].at.UTC()
		if points[index].at.IsZero() ||
			(points[index].incidentID != "" &&
				!validUUID(points[index].incidentID)) ||
			(index > 0 && !points[index].at.After(points[index-1].at)) {
			return Aggregate{}, ErrInvalidSLO
		}
	}
	if points[0].at.After(request.WindowStart) {
		return Aggregate{}, ErrInvalidSLO
	}

	good := time.Duration(0)
	bad := time.Duration(0)
	links := make(map[IncidentLink]struct{})
	for index, point := range points {
		start := point.at
		if start.Before(request.WindowStart) {
			start = request.WindowStart
		}
		end := request.WindowEnd
		if index+1 < len(points) && points[index+1].at.Before(end) {
			end = points[index+1].at
		}
		if !end.After(start) || !start.Before(request.WindowEnd) {
			continue
		}
		if point.eligible {
			if point.healthy {
				good += end.Sub(start)
				addLink(links, point.incidentID, LinkRecovery)
			} else {
				if point.incidentID == "" {
					return Aggregate{}, ErrInvalidSLO
				}
				bad += end.Sub(start)
				addLink(links, point.incidentID, LinkFailure)
			}
		} else {
			addLink(links, point.incidentID, LinkExclusion)
		}
	}
	windowMilliseconds := request.WindowEnd.Sub(request.WindowStart).Milliseconds()
	eligibleMilliseconds := (good + bad).Milliseconds()
	if eligibleMilliseconds > windowMilliseconds {
		return Aggregate{}, ErrInvalidSLO
	}
	return Aggregate{
		Granularity:          request.Granularity,
		TargetKey:            request.TargetKey,
		NodeID:               request.NodeID,
		Service:              service,
		Objective:            objective,
		WindowStart:          request.WindowStart.UTC(),
		WindowEnd:            request.WindowEnd.UTC(),
		EligibleMilliseconds: eligibleMilliseconds,
		GoodMilliseconds:     good.Milliseconds(),
		BadMilliseconds:      bad.Milliseconds(),
		ExcludedMilliseconds: windowMilliseconds - eligibleMilliseconds,
		ComputedAt:           request.ComputedAt.UTC(),
		Links:                sortedLinks(links),
	}, nil
}

func calculateRecovery(
	request Request,
	service Service,
	objective string,
	deadline time.Duration,
	nodeRequired bool,
	attempts []RecoveryAttempt,
) (Aggregate, error) {
	if validateRequest(request, nodeRequired) != nil ||
		deadline <= 0 ||
		len(attempts) > maxCalculationPoints {
		return Aggregate{}, ErrInvalidSLO
	}
	sorted := append([]RecoveryAttempt(nil), attempts...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].StartedAt.Before(sorted[j].StartedAt)
	})
	var (
		eligible   time.Duration
		good       time.Duration
		bad        time.Duration
		excluded   time.Duration
		qualifying uint64
		total      uint64
	)
	links := make(map[IncidentLink]struct{})
	for index, attempt := range sorted {
		attempt.StartedAt = attempt.StartedAt.UTC()
		if attempt.StartedAt.IsZero() ||
			attempt.StartedAt.Before(request.WindowStart) ||
			!attempt.StartedAt.Before(request.WindowEnd) ||
			(attempt.IncidentID != "" && !validUUID(attempt.IncidentID)) ||
			(index > 0 && !attempt.StartedAt.After(sorted[index-1].StartedAt)) {
			return Aggregate{}, ErrInvalidSLO
		}
		recoveredAt, eligibilityEndedAt, err := validateAttempt(attempt)
		if err != nil {
			return Aggregate{}, err
		}
		if (recoveredAt != nil && recoveredAt.After(request.ComputedAt)) ||
			(eligibilityEndedAt != nil &&
				eligibilityEndedAt.After(request.ComputedAt)) {
			return Aggregate{}, ErrInvalidSLO
		}
		deadlineAt := attempt.StartedAt.Add(deadline)
		if eligibilityEndedAt != nil &&
			eligibilityEndedAt.Before(deadlineAt) &&
			(recoveredAt == nil || eligibilityEndedAt.Before(*recoveredAt)) {
			excluded += eligibilityEndedAt.Sub(attempt.StartedAt)
			addLink(links, attempt.IncidentID, LinkExclusion)
			continue
		}
		if recoveredAt != nil {
			elapsed := recoveredAt.Sub(attempt.StartedAt)
			total++
			if elapsed <= deadline {
				eligible += elapsed
				good += elapsed
				qualifying++
				addLink(links, attempt.IncidentID, LinkRecovery)
			} else {
				if attempt.IncidentID == "" {
					return Aggregate{}, ErrInvalidSLO
				}
				eligible += deadline
				bad += deadline
				addLink(links, attempt.IncidentID, LinkFailure)
			}
			continue
		}
		if !request.ComputedAt.Before(deadlineAt) {
			if attempt.IncidentID == "" {
				return Aggregate{}, ErrInvalidSLO
			}
			total++
			eligible += deadline
			bad += deadline
			addLink(links, attempt.IncidentID, LinkFailure)
		}
	}
	return Aggregate{
		Granularity:          request.Granularity,
		TargetKey:            request.TargetKey,
		NodeID:               request.NodeID,
		Service:              service,
		Objective:            objective,
		WindowStart:          request.WindowStart.UTC(),
		WindowEnd:            request.WindowEnd.UTC(),
		EligibleMilliseconds: eligible.Milliseconds(),
		GoodMilliseconds:     good.Milliseconds(),
		BadMilliseconds:      bad.Milliseconds(),
		ExcludedMilliseconds: excluded.Milliseconds(),
		QualifyingCount:      qualifying,
		TotalCount:           total,
		ComputedAt:           request.ComputedAt.UTC(),
		Links:                sortedLinks(links),
	}, nil
}

func validateAttempt(
	attempt RecoveryAttempt,
) (*time.Time, *time.Time, error) {
	var recoveredAt *time.Time
	if attempt.RecoveredAt != nil {
		value := attempt.RecoveredAt.UTC()
		if !value.After(attempt.StartedAt) {
			return nil, nil, ErrInvalidSLO
		}
		recoveredAt = &value
	}
	var eligibilityEndedAt *time.Time
	if attempt.EligibilityEndedAt != nil {
		value := attempt.EligibilityEndedAt.UTC()
		if !value.After(attempt.StartedAt) {
			return nil, nil, ErrInvalidSLO
		}
		eligibilityEndedAt = &value
	}
	return recoveredAt, eligibilityEndedAt, nil
}

func addLink(
	links map[IncidentLink]struct{},
	incidentID metadata.UUID,
	role LinkRole,
) {
	if incidentID == "" {
		return
	}
	links[IncidentLink{IncidentID: incidentID, Role: role}] = struct{}{}
}

func sortedLinks(links map[IncidentLink]struct{}) []IncidentLink {
	result := make([]IncidentLink, 0, len(links))
	for link := range links {
		result = append(result, link)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].IncidentID != result[j].IncidentID {
			return result[i].IncidentID < result[j].IncidentID
		}
		return result[i].Role < result[j].Role
	})
	return result
}
