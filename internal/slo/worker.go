package slo

import (
	"context"
	"errors"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

// The dashboard has rendered an empty SLO section since it was written,
// because nothing ever filled it. This is the pass that does.
//
// One aggregate per node per window. The unique key is
// (granularity, target_key, service, objective, window_start) and does not
// include the node, so the node is the target key: two hosts reporting the
// same window would otherwise upsert over each other and the surviving number
// would describe neither, without anything saying so.

// Worker calculates closed windows and stores their aggregates.
type Worker struct {
	store *PostgresStore
	// Lookback bounds how far back a run will reach for windows that were
	// never computed. Detail events are evicted after thirty days, so reaching
	// past that finds nothing and refuses; the bound keeps a restarted worker
	// from asking anyway.
	lookback time.Duration
}

// MaxLookback is as far back as a run may reach.
const MaxLookback = 7 * 24 * time.Hour

// NewWorker returns a calculation pass over closed hourly windows.
func NewWorker(store *PostgresStore, lookback time.Duration) (*Worker, error) {
	if store == nil || lookback <= 0 || lookback > MaxLookback {
		return nil, ErrInvalidSLO
	}
	return &Worker{store: store, lookback: lookback}, nil
}

// Result reports what one pass did.
type Result struct {
	Windows int
	// Unmeasurable counts windows refused because their leading state or a
	// failure's incident could not be established. They are not failures of
	// the pass: they are windows this host cannot honestly measure, and they
	// stay uncomputed until the evidence that would explain them exists.
	Unmeasurable int
}

// RunOnce calculates every closed hourly window inside the lookback that has
// no aggregate yet.
func (worker *Worker) RunOnce(ctx context.Context, at time.Time) (Result, error) {
	if worker == nil || worker.store == nil || ctx == nil || at.IsZero() {
		return Result{}, ErrInvalidSLO
	}
	nodes, err := worker.nodes(ctx)
	if err != nil {
		return Result{}, err
	}
	// Only closed windows. A window still in progress would be computed from
	// partial evidence and then upserted again later with a different answer,
	// which is a number that changes for a reason nobody can see.
	newest := at.UTC().Truncate(time.Hour)
	oldest := newest.Add(-worker.lookback).Truncate(time.Hour)

	result := Result{}
	for _, node := range nodes {
		for start := oldest; start.Before(newest); start = start.Add(time.Hour) {
			computed, measurable, err := worker.window(ctx, node, start, at.UTC())
			if err != nil {
				return result, err
			}
			switch {
			case computed:
				result.Windows++
			case !measurable:
				result.Unmeasurable++
			}
		}
	}
	return result, nil
}

// window calculates one node's window unless it already has an aggregate.
func (worker *Worker) window(
	ctx context.Context,
	node metadata.UUID,
	start time.Time,
	computedAt time.Time,
) (bool, bool, error) {
	end := start.Add(time.Hour)
	request := Request{
		Granularity: GranularityHour,
		TargetKey:   string(node),
		NodeID:      node,
		WindowStart: start,
		WindowEnd:   end,
		ComputedAt:  computedAt,
	}
	stored, err := worker.exists(ctx, request)
	if err != nil || stored {
		return false, true, err
	}
	states, err := worker.store.TwilightStates(ctx, node, start, end)
	switch {
	case errors.Is(err, ErrLeadingStateUnknown),
		errors.Is(err, ErrUnattributedFailure),
		errors.Is(err, ErrTooManyStateChanges):
		return false, false, nil
	case err != nil:
		return false, true, err
	}
	aggregate, err := CalculateTwilight(request, states)
	if err != nil {
		// The calculator refusing a series this reader built is a disagreement
		// between them, not a property of the window. It is reported rather
		// than counted as unmeasurable.
		return false, true, err
	}
	if _, err := worker.store.Upsert(ctx, aggregate); err != nil {
		return false, true, err
	}
	return true, true, nil
}

// exists reports whether the window already has an aggregate.
func (worker *Worker) exists(ctx context.Context, request Request) (bool, error) {
	reader, ok := worker.store.database.(Reader)
	if !ok {
		return false, ErrInvalidSLO
	}
	rows, err := reader.Query(ctx, `
		SELECT 1
		FROM slo_aggregates
		WHERE granularity = $1
		  AND target_key = $2
		  AND service = $3
		  AND objective = $4
		  AND window_start = $5
		LIMIT 1
	`, string(request.Granularity), request.TargetKey,
		string(ServiceTwilight), ObjectiveTwilightAvailability, request.WindowStart)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	return rows.Next(), rows.Err()
}

// nodes returns the active nodes to calculate for.
func (worker *Worker) nodes(ctx context.Context) ([]metadata.UUID, error) {
	reader, ok := worker.store.database.(Reader)
	if !ok {
		return nil, ErrInvalidSLO
	}
	rows, err := reader.Query(ctx, `
		SELECT node_id::text
		FROM nodes
		WHERE lifecycle_status = 'active'
		ORDER BY node_id
		LIMIT 64
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	nodes := make([]metadata.UUID, 0, 4)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		parsed, parseErr := metadata.ParseUUID(id)
		if parseErr != nil {
			return nil, ErrInvalidSLO
		}
		nodes = append(nodes, parsed)
	}
	return nodes, rows.Err()
}
