package slo

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/event"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

// The calculators take piecewise-constant state; the database holds events.
// This turns one into the other and stores nothing new.
//
// A derived series is not an artifact. The stored artifact is the aggregate,
// upserted on the same key so late evidence can finalize an earlier window —
// and keeping the series as well would give the same truth two homes that can
// disagree, with the second one needing its own correction every time a late
// event arrives.
//
// docs/cloud/slos.md defines what is being read here:
//
//	Twilight is eligible only while the Mac is awake and at least one physical
//	carrier is available. Eligible time is good while the Twilight transport is
//	ready and bad otherwise.
//
// and the rule that shapes every query below:
//
//	The state at the beginning of an availability window is mandatory; the
//	calculator refuses to guess unknown leading time.

// ErrLeadingStateUnknown reports that the state at the window's start cannot be
// established from retained evidence.
//
// It is not a failure of the window. It means the window cannot be measured,
// which is a different thing from measuring it as unavailable — and the
// difference is the whole reason this error exists rather than a zero value.
var ErrLeadingStateUnknown = errors.New("leading state at window start is unknown")

// ErrUnattributedFailure reports that eligible time was unhealthy with no
// incident covering it.
//
// The calculator refuses such a window, and refusing is right: unattributed
// downtime is a gap in the incident record, not a number to publish. The
// window becomes measurable once the incident that explains it exists.
var ErrUnattributedFailure = errors.New("failed time has no incident to attribute it to")

// ErrTooManyStateChanges reports that the window holds more changes than a
// calculation may carry. It refuses rather than truncating: a truncated series
// produces a number, and the number would be wrong without saying so.
var ErrTooManyStateChanges = errors.New("window exceeds the bounded state changes")

// Reader is the read half of the store. It is separate from Database so a
// caller that only calculates cannot write.
type Reader interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

// componentSignal is one component's contribution to eligibility or health.
type componentSignal struct {
	at    time.Time
	ready bool
}

// TwilightStates builds the state series for one node's availability window.
//
// The series always opens at or before the window start, because the
// calculator refuses a series that does not. Where the leading state cannot be
// established the window is refused here rather than filled with an assumption.
func (store *PostgresStore) TwilightStates(
	ctx context.Context,
	nodeID metadata.UUID,
	windowStart time.Time,
	windowEnd time.Time,
) ([]TwilightState, error) {
	if store == nil || store.database == nil || ctx == nil ||
		nodeID == "" || windowStart.IsZero() || !windowEnd.After(windowStart) {
		return nil, ErrInvalidSLO
	}
	reader, ok := store.database.(Reader)
	if !ok {
		return nil, ErrInvalidSLO
	}
	start := windowStart.UTC()
	end := windowEnd.UTC()

	carrier, err := componentSignals(
		ctx, reader, nodeID, control.ComponentNetwork, start, end)
	if err != nil {
		return nil, err
	}
	transport, err := componentSignals(
		ctx, reader, nodeID, control.ComponentTunnel, start, end)
	if err != nil {
		return nil, err
	}
	// Both components must have said something at or before the window opens.
	// Without that there is no leading state, and inventing one would decide
	// the first stretch of the window by assumption.
	if len(carrier) == 0 || len(transport) == 0 ||
		carrier[0].at.After(start) || transport[0].at.After(start) {
		return nil, ErrLeadingStateUnknown
	}
	asleep, err := sleepChanges(ctx, reader, nodeID, start, end)
	if err != nil {
		return nil, err
	}
	incidents, err := incidentSpans(ctx, reader, nodeID, control.ComponentTunnel, start, end)
	if err != nil {
		return nil, err
	}
	return merge(carrier, transport, asleep, incidents, start)
}

// componentSignals returns one component's readiness changes, opening with the
// last one at or before the window start.
func componentSignals(
	ctx context.Context,
	reader Reader,
	nodeID metadata.UUID,
	component control.Component,
	start time.Time,
	end time.Time,
) ([]componentSignal, error) {
	rows, err := reader.Query(ctx, `
		(
			SELECT occurred_at, payload
			FROM events
			WHERE node_id = $1
			  AND schema_name = $2
			  AND payload->>'component' = $3
			  AND occurred_at <= $4
			ORDER BY occurred_at DESC, sequence DESC
			LIMIT 1
		)
		UNION ALL
		(
			SELECT occurred_at, payload
			FROM events
			WHERE node_id = $1
			  AND schema_name = $2
			  AND payload->>'component' = $3
			  AND occurred_at > $4
			  AND occurred_at < $5
			ORDER BY occurred_at, sequence
			LIMIT $6
		)
	`,
		string(nodeID), string(event.SchemaObservation), string(component),
		start, end, maxCalculationPoints+1,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	signals := make([]componentSignal, 0, 16)
	for rows.Next() {
		var (
			at      time.Time
			payload []byte
		)
		if err := rows.Scan(&at, &payload); err != nil {
			return nil, err
		}
		var observation event.Observation
		if err := json.Unmarshal(payload, &observation); err != nil {
			return nil, ErrInvalidSLO
		}
		signals = append(signals, componentSignal{
			at: at.UTC(), ready: observation.Health == control.HealthReady,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(signals, func(i, j int) bool { return signals[i].at.Before(signals[j].at) })
	return signals, nil
}

// sleepChanges returns the instants where the host fell asleep or woke, with
// the state at the window start first.
//
// An interval with no end is still in progress: the host has not woken, and
// the window ends while it is still asleep.
func sleepChanges(
	ctx context.Context,
	reader Reader,
	nodeID metadata.UUID,
	start time.Time,
	end time.Time,
) ([]componentSignal, error) {
	rows, err := reader.Query(ctx, `
		SELECT started_at, ended_at
		FROM sleep_intervals
		WHERE node_id = $1
		  AND started_at < $3
		  AND (ended_at IS NULL OR ended_at > $2)
		ORDER BY started_at
		LIMIT $4
	`, string(nodeID), start, end, maxCalculationPoints)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// The host is awake unless an interval says otherwise, and the opening
	// entry carries that.
	changes := []componentSignal{{at: start, ready: true}}
	for rows.Next() {
		var (
			startedAt time.Time
			endedAt   *time.Time
		)
		if err := rows.Scan(&startedAt, &endedAt); err != nil {
			return nil, err
		}
		if startedAt.UTC().After(start) {
			changes = append(changes, componentSignal{at: startedAt.UTC(), ready: false})
		} else {
			// The window opens inside this interval.
			changes[0].ready = false
		}
		if endedAt != nil && endedAt.UTC().After(start) && endedAt.UTC().Before(end) {
			changes = append(changes, componentSignal{at: endedAt.UTC(), ready: true})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].at.Before(changes[j].at) })
	return changes, nil
}

// incidentSpan is one open incident's extent.
type incidentSpan struct {
	id       metadata.UUID
	openedAt time.Time
	// resolvedAt is zero while the incident is still open.
	resolvedAt time.Time
}

// incidentSpans returns the incidents that could explain unhealthy time for a
// component inside the window.
func incidentSpans(
	ctx context.Context,
	reader Reader,
	nodeID metadata.UUID,
	component control.Component,
	start time.Time,
	end time.Time,
) ([]incidentSpan, error) {
	rows, err := reader.Query(ctx, `
		SELECT incident_id::text, opened_at, resolved_at
		FROM incidents
		WHERE node_id = $1
		  AND component = $2
		  AND opened_at < $4
		  AND (resolved_at IS NULL OR resolved_at > $3)
		ORDER BY opened_at
		LIMIT $5
	`, string(nodeID), string(component), start, end, maxCalculationPoints)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	spans := make([]incidentSpan, 0, 4)
	for rows.Next() {
		var (
			id         string
			openedAt   time.Time
			resolvedAt *time.Time
		)
		if err := rows.Scan(&id, &openedAt, &resolvedAt); err != nil {
			return nil, err
		}
		parsed, parseErr := metadata.ParseUUID(id)
		if parseErr != nil {
			return nil, ErrInvalidSLO
		}
		span := incidentSpan{id: parsed, openedAt: openedAt.UTC()}
		if resolvedAt != nil {
			span.resolvedAt = resolvedAt.UTC()
		}
		spans = append(spans, span)
	}
	return spans, rows.Err()
}

// incidentAt returns the incident covering an instant, if one does.
func incidentAt(spans []incidentSpan, at time.Time) metadata.UUID {
	for _, span := range spans {
		if span.openedAt.After(at) {
			continue
		}
		if span.resolvedAt.IsZero() || span.resolvedAt.After(at) {
			return span.id
		}
	}
	return ""
}

// merge walks the independent change series into one.
func merge(
	carrier []componentSignal,
	transport []componentSignal,
	asleep []componentSignal,
	incidents []incidentSpan,
	start time.Time,
) ([]TwilightState, error) {
	// The opening state of every component is mandatory. Without it the first
	// stretch of the window would be decided by assumption, which is the one
	// thing the document says this may not do.
	if !opensBy(carrier, start) || !opensBy(transport, start) {
		return nil, ErrLeadingStateUnknown
	}
	instants := make([]time.Time, 0, len(carrier)+len(transport)+len(asleep))
	for _, series := range [][]componentSignal{carrier, transport, asleep} {
		for _, signal := range series {
			at := signal.at
			if at.Before(start) {
				// Everything before the window opens describes the leading
				// state, which the series carries as one point at the start.
				at = start
			}
			instants = append(instants, at)
		}
	}
	sort.Slice(instants, func(i, j int) bool { return instants[i].Before(instants[j]) })

	states := make([]TwilightState, 0, len(instants))
	for index, at := range instants {
		if index > 0 && !at.After(instants[index-1]) {
			continue
		}
		state := TwilightState{
			At:               at,
			Awake:            valueAt(asleep, at),
			CarrierAvailable: valueAt(carrier, at),
			TransportReady:   valueAt(transport, at),
		}
		// Eligible time that is not good has to name the incident that
		// explains it. The calculator refuses a window where it cannot, and
		// that refusal is the point: downtime nobody recorded is a hole in the
		// incident record rather than a measurement.
		if state.Awake && state.CarrierAvailable && !state.TransportReady {
			state.IncidentID = incidentAt(incidents, at)
			if state.IncidentID == "" {
				return nil, ErrUnattributedFailure
			}
		}
		states = append(states, state)
		if len(states) > maxCalculationPoints {
			return nil, ErrTooManyStateChanges
		}
	}
	if len(states) == 0 || states[0].At.After(start) {
		return nil, ErrLeadingStateUnknown
	}
	return states, nil
}

// opensBy reports whether a series says anything at or before an instant.
func opensBy(series []componentSignal, at time.Time) bool {
	for _, signal := range series {
		if !signal.at.After(at) {
			return true
		}
	}
	return false
}

// valueAt returns what a series says at an instant: the most recent change at
// or before it.
func valueAt(series []componentSignal, at time.Time) bool {
	value := false
	for _, signal := range series {
		if signal.at.After(at) {
			break
		}
		value = signal.ready
	}
	return value
}
