package slo

import (
	"errors"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

const probeIncident = metadata.UUID("22222222-2222-4222-8222-222222222222")

// covering is an incident that explains any unhealthy time in these fixtures.
func covering() []incidentSpan {
	return []incidentSpan{{id: probeIncident, openedAt: at(-60)}}
}

func at(minute int) time.Time {
	return time.Date(2026, time.August, 30, 12, minute, 0, 0, time.UTC)
}

// The rule the document states and the calculator enforces: a window whose
// opening state is unknown is refused, not filled in. Measuring a window as
// unavailable and being unable to measure it are different answers, and only
// one of them is true.
func TestSeriesWithoutALeadingStateIsRefused(t *testing.T) {
	// Both components speak only after the window opened.
	_, err := merge(
		[]componentSignal{{at: at(5), ready: true}},
		[]componentSignal{{at: at(5), ready: true}},
		[]componentSignal{{at: at(0), ready: true}},
		nil,
		at(0),
	)
	if err == nil {
		t.Fatal("a window with no leading state produced a series")
	}
	if !errors.Is(err, ErrLeadingStateUnknown) {
		t.Fatalf("error = %v, want ErrLeadingStateUnknown", err)
	}
}

// Eligibility is awake and carrier; health is transport readiness. The
// document defines all three and the series has to carry them separately,
// because excluded time is not failed time.
func TestSeriesCarriesEligibilityAndHealthSeparately(t *testing.T) {
	states, err := merge(
		[]componentSignal{{at: at(0), ready: true}, {at: at(10), ready: false}},
		[]componentSignal{{at: at(0), ready: true}, {at: at(5), ready: false}},
		[]componentSignal{{at: at(0), ready: true}},
		covering(),
		at(0),
	)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if len(states) != 3 {
		t.Fatalf("%d states, want 3", len(states))
	}
	// Opens awake, carrier up, transport ready.
	if !states[0].Awake || !states[0].CarrierAvailable || !states[0].TransportReady {
		t.Fatalf("opening state = %+v", states[0])
	}
	// Transport fails: still eligible, no longer good.
	if !states[1].CarrierAvailable || states[1].TransportReady {
		t.Fatalf("transport failure = %+v", states[1])
	}
	// Carrier fails: no longer eligible at all, which is excluded rather
	// than bad.
	if states[2].CarrierAvailable {
		t.Fatalf("carrier failure = %+v", states[2])
	}
}

// A window that opens while the host is asleep opens ineligible, and the
// sleep interval that contains the start is what says so.
func TestWindowOpeningInsideSleepIsNotEligible(t *testing.T) {
	states, err := merge(
		[]componentSignal{{at: at(0), ready: true}},
		[]componentSignal{{at: at(0), ready: true}},
		// The opening entry carries the sleep that began before the window.
		[]componentSignal{{at: at(0), ready: false}, {at: at(7), ready: true}},
		nil,
		at(0),
	)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if states[0].Awake {
		t.Fatal("a window opening inside sleep was recorded as awake")
	}
	if !states[len(states)-1].Awake {
		t.Fatal("waking during the window was not recorded")
	}
}

// Evidence before the window describes the leading state; it must not become
// a point outside the window that the calculator would refuse for being out
// of order.
func TestEvidenceBeforeTheWindowCollapsesToTheOpeningState(t *testing.T) {
	states, err := merge(
		[]componentSignal{{at: at(-30), ready: true}},
		[]componentSignal{{at: at(-20), ready: false}},
		[]componentSignal{{at: at(0), ready: true}},
		covering(),
		at(0),
	)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if len(states) != 1 {
		t.Fatalf("%d states, want one opening state", len(states))
	}
	if !states[0].At.Equal(at(0)) {
		t.Fatalf("opening state at %s, want the window start", states[0].At)
	}
	if !states[0].CarrierAvailable || states[0].TransportReady {
		t.Fatalf("leading state was not carried forward: %+v", states[0])
	}
}

// The series the calculator receives must be one it accepts, or the reader has
// produced work nothing can use.
func TestProducedSeriesIsAcceptedByTheCalculator(t *testing.T) {
	states, err := merge(
		[]componentSignal{{at: at(0), ready: true}},
		[]componentSignal{{at: at(0), ready: true}, {at: at(20), ready: false}},
		[]componentSignal{{at: at(0), ready: true}},
		covering(),
		at(0),
	)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	aggregate, err := CalculateTwilight(Request{
		Granularity: GranularityHour,
		TargetKey:   "11111111-1111-4111-8111-111111111111",
		NodeID:      metadata.UUID("11111111-1111-4111-8111-111111111111"),
		WindowStart: at(0),
		WindowEnd:   at(60),
		ComputedAt:  at(61),
	}, states)
	if err != nil {
		t.Fatalf("the calculator refused the series this reader builds: %v", err)
	}
	if aggregate.EligibleMilliseconds == 0 {
		t.Fatal("an eligible window measured no eligible time")
	}
	if aggregate.GoodMilliseconds == 0 || aggregate.BadMilliseconds == 0 {
		t.Fatalf("a window with a transport failure measured good=%d bad=%d",
			aggregate.GoodMilliseconds, aggregate.BadMilliseconds)
	}
}

// Truncating a series produces a number, and the number would be wrong
// without saying so.
func TestTooManyChangesIsRefusedRatherThanTruncated(t *testing.T) {
	carrier := make([]componentSignal, 0, maxCalculationPoints+2)
	for index := 0; index <= maxCalculationPoints+1; index++ {
		carrier = append(carrier, componentSignal{
			at: at(0).Add(time.Duration(index) * time.Second), ready: index%2 == 0,
		})
	}
	_, err := merge(
		carrier,
		[]componentSignal{{at: at(0), ready: true}},
		[]componentSignal{{at: at(0), ready: true}},
		nil,
		at(0),
	)
	if !errors.Is(err, ErrTooManyStateChanges) {
		t.Fatalf("error = %v, want ErrTooManyStateChanges", err)
	}
}

// Eligible time that is not good has to name the incident that explains it.
// Downtime nobody recorded is a hole in the incident record, and publishing an
// availability number over it would paper the hole with a measurement.
func TestUnattributedFailedTimeIsRefused(t *testing.T) {
	_, err := merge(
		[]componentSignal{{at: at(0), ready: true}},
		[]componentSignal{{at: at(0), ready: true}, {at: at(20), ready: false}},
		[]componentSignal{{at: at(0), ready: true}},
		nil,
		at(0),
	)
	if !errors.Is(err, ErrUnattributedFailure) {
		t.Fatalf("error = %v, want ErrUnattributedFailure", err)
	}
}

// Unhealthy time while ineligible needs no incident: excluded time is not
// failed time, and demanding an incident for sleep would invent outages.
func TestUnhealthyWhileAsleepNeedsNoIncident(t *testing.T) {
	states, err := merge(
		[]componentSignal{{at: at(0), ready: true}},
		[]componentSignal{{at: at(0), ready: false}},
		[]componentSignal{{at: at(0), ready: false}},
		nil,
		at(0),
	)
	if err != nil {
		t.Fatalf("sleep with an unready transport demanded an incident: %v", err)
	}
	if states[0].IncidentID != "" {
		t.Fatal("excluded time was attributed to an incident")
	}
}
