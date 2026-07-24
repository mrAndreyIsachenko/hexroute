package slo

import (
	"errors"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

const (
	sloNodeID      = metadata.UUID("11111111-1111-4111-8111-111111111111")
	firstIncident  = metadata.UUID("22222222-2222-4222-8222-222222222222")
	secondIncident = metadata.UUID("33333333-3333-4333-8333-333333333333")
	thirdIncident  = metadata.UUID("44444444-4444-4444-8444-444444444444")
	fourthIncident = metadata.UUID("55555555-5555-4555-8555-555555555555")
)

func TestCalculateTwilightExcludesSleepAndMissingCarrier(t *testing.T) {
	request := hourlyRequest()
	aggregate, err := CalculateTwilight(request, []TwilightState{
		{
			At:               request.WindowStart.Add(-time.Minute),
			Awake:            true,
			CarrierAvailable: true,
			TransportReady:   true,
		},
		{
			At:               request.WindowStart.Add(10 * time.Minute),
			Awake:            false,
			CarrierAvailable: true,
			TransportReady:   true,
		},
		{
			At:               request.WindowStart.Add(20 * time.Minute),
			Awake:            true,
			CarrierAvailable: false,
			TransportReady:   false,
		},
		{
			At:               request.WindowStart.Add(30 * time.Minute),
			Awake:            true,
			CarrierAvailable: true,
			TransportReady:   false,
			IncidentID:       firstIncident,
		},
		{
			At:               request.WindowStart.Add(45 * time.Minute),
			Awake:            true,
			CarrierAvailable: true,
			TransportReady:   true,
			IncidentID:       firstIncident,
		},
	})
	if err != nil {
		t.Fatalf("CalculateTwilight() error = %v", err)
	}
	assertDurations(t, aggregate, 40*time.Minute, 25*time.Minute, 15*time.Minute, 20*time.Minute)
	assertLinks(t, aggregate.Links, []IncidentLink{
		{IncidentID: firstIncident, Role: LinkFailure},
		{IncidentID: firstIncident, Role: LinkRecovery},
	})
}

func TestCalculateTelegramUsesClusterEligibility(t *testing.T) {
	request := hourlyRequest()
	request.NodeID = ""
	request.TargetKey = "telegram:cluster"
	aggregate, err := CalculateTelegram(request, []TelegramState{
		{At: request.WindowStart, ReachableProviders: 2, HealthyProxies: 2},
		{
			At:                 request.WindowStart.Add(10 * time.Minute),
			ReachableProviders: 2,
			HealthyProxies:     1,
		},
		{
			At:                 request.WindowStart.Add(20 * time.Minute),
			ReachableProviders: 1,
			HealthyProxies:     0,
			IncidentID:         secondIncident,
		},
		{
			At:                 request.WindowStart.Add(30 * time.Minute),
			ReachableProviders: 0,
			HealthyProxies:     0,
			IncidentID:         thirdIncident,
		},
		{
			At:                 request.WindowStart.Add(40 * time.Minute),
			ReachableProviders: 1,
			HealthyProxies:     1,
			IncidentID:         secondIncident,
		},
	})
	if err != nil {
		t.Fatalf("CalculateTelegram() error = %v", err)
	}
	assertDurations(t, aggregate, 50*time.Minute, 40*time.Minute, 10*time.Minute, 10*time.Minute)
	assertLinks(t, aggregate.Links, []IncidentLink{
		{IncidentID: secondIncident, Role: LinkFailure},
		{IncidentID: secondIncident, Role: LinkRecovery},
		{IncidentID: thirdIncident, Role: LinkExclusion},
	})
}

func TestCalculatePritunlEvaluatesBothDeadlines(t *testing.T) {
	request := hourlyRequest()
	attempts := []RecoveryAttempt{
		{
			StartedAt:   request.WindowStart.Add(5 * time.Minute),
			RecoveredAt: timePointer(request.WindowStart.Add(7 * time.Minute)),
			IncidentID:  firstIncident,
		},
		{
			StartedAt:   request.WindowStart.Add(15 * time.Minute),
			RecoveredAt: timePointer(request.WindowStart.Add(20 * time.Minute)),
			IncidentID:  secondIncident,
		},
		{
			StartedAt:  request.WindowStart.Add(30 * time.Minute),
			IncidentID: thirdIncident,
		},
		{
			StartedAt:          request.WindowStart.Add(45 * time.Minute),
			EligibilityEndedAt: timePointer(request.WindowStart.Add(46 * time.Minute)),
			IncidentID:         fourthIncident,
		},
		{
			StartedAt:  request.WindowStart.Add(59 * time.Minute),
			IncidentID: firstIncident,
		},
	}
	aggregates, err := CalculatePritunl(request, attempts)
	if err != nil {
		t.Fatalf("CalculatePritunl() error = %v", err)
	}
	threeMinutes := aggregates[0]
	if threeMinutes.QualifyingCount != 1 || threeMinutes.TotalCount != 3 {
		t.Fatalf("3m counts = %d/%d", threeMinutes.QualifyingCount, threeMinutes.TotalCount)
	}
	assertDurations(t, threeMinutes, 8*time.Minute, 2*time.Minute, 6*time.Minute, time.Minute)
	assertLinks(t, threeMinutes.Links, []IncidentLink{
		{IncidentID: firstIncident, Role: LinkRecovery},
		{IncidentID: secondIncident, Role: LinkFailure},
		{IncidentID: thirdIncident, Role: LinkFailure},
		{IncidentID: fourthIncident, Role: LinkExclusion},
	})

	tenMinutes := aggregates[1]
	if tenMinutes.QualifyingCount != 2 || tenMinutes.TotalCount != 3 {
		t.Fatalf("10m counts = %d/%d", tenMinutes.QualifyingCount, tenMinutes.TotalCount)
	}
	assertDurations(t, tenMinutes, 17*time.Minute, 7*time.Minute, 10*time.Minute, time.Minute)
	assertLinks(t, tenMinutes.Links, []IncidentLink{
		{IncidentID: firstIncident, Role: LinkRecovery},
		{IncidentID: secondIncident, Role: LinkRecovery},
		{IncidentID: thirdIncident, Role: LinkFailure},
		{IncidentID: fourthIncident, Role: LinkExclusion},
	})
}

func TestCalculateCodexDoesNotFailPendingOpportunity(t *testing.T) {
	request := hourlyRequest()
	aggregate, err := CalculateCodex(request, []RecoveryAttempt{
		{
			StartedAt:   request.WindowStart.Add(5 * time.Minute),
			RecoveredAt: timePointer(request.WindowStart.Add(5*time.Minute + 20*time.Second)),
			IncidentID:  firstIncident,
		},
		{
			StartedAt:   request.WindowStart.Add(15 * time.Minute),
			RecoveredAt: timePointer(request.WindowStart.Add(15*time.Minute + 40*time.Second)),
			IncidentID:  secondIncident,
		},
		{
			StartedAt:  request.WindowEnd.Add(-10 * time.Second),
			IncidentID: thirdIncident,
		},
	})
	if err != nil {
		t.Fatalf("CalculateCodex() error = %v", err)
	}
	if aggregate.QualifyingCount != 1 || aggregate.TotalCount != 2 {
		t.Fatalf("counts = %d/%d", aggregate.QualifyingCount, aggregate.TotalCount)
	}
	assertDurations(t, aggregate, 50*time.Second, 20*time.Second, 30*time.Second, 0)
}

func TestCalculateTelegramFailoverRequiresReachableAlternative(t *testing.T) {
	request := hourlyRequest()
	request.NodeID = ""
	request.TargetKey = "telegram:cluster"
	aggregate, err := CalculateTelegramFailover(request, []RecoveryAttempt{
		{
			StartedAt:   request.WindowStart.Add(5 * time.Minute),
			RecoveredAt: timePointer(request.WindowStart.Add(5*time.Minute + 40*time.Second)),
			IncidentID:  firstIncident,
		},
		{
			StartedAt:   request.WindowStart.Add(15 * time.Minute),
			RecoveredAt: timePointer(request.WindowStart.Add(15*time.Minute + 70*time.Second)),
			IncidentID:  secondIncident,
		},
		{
			StartedAt:          request.WindowStart.Add(25 * time.Minute),
			EligibilityEndedAt: timePointer(request.WindowStart.Add(25*time.Minute + 20*time.Second)),
			IncidentID:         thirdIncident,
		},
	})
	if err != nil {
		t.Fatalf("CalculateTelegramFailover() error = %v", err)
	}
	if aggregate.QualifyingCount != 1 || aggregate.TotalCount != 2 {
		t.Fatalf("counts = %d/%d", aggregate.QualifyingCount, aggregate.TotalCount)
	}
	assertDurations(t, aggregate, 100*time.Second, 40*time.Second, 60*time.Second, 20*time.Second)
	assertLinks(t, aggregate.Links, []IncidentLink{
		{IncidentID: firstIncident, Role: LinkRecovery},
		{IncidentID: secondIncident, Role: LinkFailure},
		{IncidentID: thirdIncident, Role: LinkExclusion},
	})
}

func TestAvailabilityRequiresStateAtWindowStart(t *testing.T) {
	request := hourlyRequest()
	_, err := CalculateTwilight(request, []TwilightState{{
		At:               request.WindowStart.Add(time.Second),
		Awake:            true,
		CarrierAvailable: true,
		TransportReady:   true,
	}})
	if !errors.Is(err, ErrInvalidSLO) {
		t.Fatalf("CalculateTwilight() error = %v, want %v", err, ErrInvalidSLO)
	}
}

func TestFailedIntervalRequiresIncidentLink(t *testing.T) {
	request := hourlyRequest()
	_, err := CalculateTwilight(request, []TwilightState{{
		At:               request.WindowStart,
		Awake:            true,
		CarrierAvailable: true,
		TransportReady:   false,
	}})
	if !errors.Is(err, ErrInvalidSLO) {
		t.Fatalf("CalculateTwilight() error = %v, want %v", err, ErrInvalidSLO)
	}
}

func hourlyRequest() Request {
	start := time.Date(2026, time.July, 25, 12, 0, 0, 0, time.UTC)
	return Request{
		Granularity: GranularityHour,
		TargetKey:   "mac:primary",
		NodeID:      sloNodeID,
		WindowStart: start,
		WindowEnd:   start.Add(time.Hour),
		ComputedAt:  start.Add(time.Hour),
	}
}

func timePointer(value time.Time) *time.Time {
	return &value
}

func assertDurations(
	t *testing.T,
	aggregate Aggregate,
	eligible time.Duration,
	good time.Duration,
	bad time.Duration,
	excluded time.Duration,
) {
	t.Helper()
	if aggregate.EligibleMilliseconds != eligible.Milliseconds() ||
		aggregate.GoodMilliseconds != good.Milliseconds() ||
		aggregate.BadMilliseconds != bad.Milliseconds() ||
		aggregate.ExcludedMilliseconds != excluded.Milliseconds() {
		t.Fatalf(
			"durations = eligible:%d good:%d bad:%d excluded:%d",
			aggregate.EligibleMilliseconds,
			aggregate.GoodMilliseconds,
			aggregate.BadMilliseconds,
			aggregate.ExcludedMilliseconds,
		)
	}
}

func assertLinks(
	t *testing.T,
	got []IncidentLink,
	want []IncidentLink,
) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("links = %+v, want %+v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("links[%d] = %+v, want %+v", index, got[index], want[index])
		}
	}
}
