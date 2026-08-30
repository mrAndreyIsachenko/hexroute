package cloudconnectivity

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mrAndreyIsachenko/hexroute/internal/event"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

// MaxProjectionBatch bounds one projection pass.
const MaxProjectionBatch = 200

type Database interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

type PostgresStore struct {
	database Database
}

func NewPostgresStore(database Database) (*PostgresStore, error) {
	if database == nil {
		return nil, ErrInvalidProjection
	}
	return &PostgresStore{database: database}, nil
}

// pending is one uploaded projection event waiting to be folded in.
type pending struct {
	eventID    metadata.UUID
	nodeID     metadata.UUID
	sessionID  metadata.UUID
	sequence   uint64
	occurredAt time.Time
	version    uint16
	priority   event.Priority
	payload    json.RawMessage
}

type wireRecord struct {
	Schema   event.Schema    `json:"schema"`
	Version  uint16          `json:"version"`
	Priority event.Priority  `json:"priority"`
	Payload  json.RawMessage `json:"payload"`
}

// decode re-validates the stored payload through the same schema the host
// signed. Storage is not trusted to have preserved shape: a payload that no
// longer decodes is refused rather than partially projected.
func decode(item pending) (event.ConnectivityProjection, error) {
	encoded, err := json.Marshal(wireRecord{
		Schema:   event.SchemaConnectivityProjection,
		Version:  item.version,
		Priority: item.priority,
		Payload:  item.payload,
	})
	if err != nil {
		return event.ConnectivityProjection{}, ErrInvalidProjection
	}
	record, err := event.Decode(encoded)
	if err != nil {
		return event.ConnectivityProjection{}, ErrInvalidProjection
	}
	projection, ok := record.Payload.(*event.ConnectivityProjection)
	if !ok || projection == nil {
		return event.ConnectivityProjection{}, ErrInvalidProjection
	}
	return *projection, nil
}

// ProjectPending folds uploaded projections into the read model and returns
// how many it applied.
//
// Events are consumed in each node's own order. A node whose stored position
// is already at or past an event skips it, so the pass is idempotent and a
// replayed batch changes nothing.
func (store *PostgresStore) ProjectPending(
	ctx context.Context,
	limit int,
) (int, error) {
	if store == nil || store.database == nil || ctx == nil {
		return 0, ErrInvalidProjection
	}
	if limit <= 0 || limit > MaxProjectionBatch {
		return 0, ErrInvalidProjection
	}
	items, err := store.pendingEvents(ctx, limit)
	if err != nil {
		return 0, err
	}
	applied := 0
	for _, item := range items {
		projection, decodeErr := decode(item)
		if decodeErr != nil {
			return applied, decodeErr
		}
		changed, foldErr := store.fold(ctx, item, projection)
		if foldErr != nil {
			return applied, foldErr
		}
		if changed {
			applied++
		}
	}
	return applied, nil
}

func (store *PostgresStore) pendingEvents(
	ctx context.Context,
	limit int,
) ([]pending, error) {
	rows, err := store.database.Query(ctx, `
		SELECT
			e.event_id::text,
			e.node_id::text,
			e.boot_session_id::text,
			e.sequence,
			e.occurred_at,
			e.schema_version,
			e.priority,
			e.payload
		FROM events e
		LEFT JOIN connectivity_snapshots s ON s.node_id = e.node_id
		WHERE e.schema_name = $1
		  AND (
			s.node_id IS NULL
			OR (e.occurred_at, e.sequence) > (s.observed_at, s.node_sequence)
		  )
		ORDER BY e.node_id, e.occurred_at, e.sequence
		LIMIT $2
	`, string(event.SchemaConnectivityProjection), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]pending, 0, limit)
	for rows.Next() {
		var (
			eventID    string
			nodeID     string
			sessionID  string
			sequence   int64
			occurredAt time.Time
			version    int
			priority   string
			payload    []byte
		)
		if err := rows.Scan(
			&eventID,
			&nodeID,
			&sessionID,
			&sequence,
			&occurredAt,
			&version,
			&priority,
			&payload,
		); err != nil {
			return nil, err
		}
		parsedEvent, eventErr := metadata.ParseUUID(eventID)
		parsedNode, nodeErr := metadata.ParseUUID(nodeID)
		parsedSession, sessionErr := metadata.ParseUUID(sessionID)
		if eventErr != nil || nodeErr != nil || sessionErr != nil ||
			sequence < 1 || version < 1 {
			return nil, ErrInvalidProjection
		}
		items = append(items, pending{
			eventID:    parsedEvent,
			nodeID:     parsedNode,
			sessionID:  parsedSession,
			sequence:   uint64(sequence),
			occurredAt: occurredAt.UTC(),
			version:    uint16(version),
			priority:   event.Priority(priority),
			payload:    append(json.RawMessage(nil), payload...),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

// fold writes one projection under a guard on the host's own position.
//
// The guard is the event position, not the snapshot generation: a host that
// lost its read-model lineage restarts its generation counter, and refusing
// everything after that would leave the dashboard showing a state the host
// abandoned. A generation that went backwards is recorded as a reset instead.
func (store *PostgresStore) fold(
	ctx context.Context,
	item pending,
	projection event.ConnectivityProjection,
) (applied bool, err error) {
	transaction, err := store.database.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, err
	}
	defer func() {
		rollbackErr := transaction.Rollback(ctx)
		if err == nil && rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			err = rollbackErr
		}
	}()

	var storedGeneration *int64
	err = transaction.QueryRow(ctx, `
		SELECT snapshot_generation
		FROM connectivity_snapshots
		WHERE node_id = $1
		FOR UPDATE
	`, string(item.nodeID)).Scan(&storedGeneration)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, err
	}
	reset := storedGeneration != nil &&
		uint64(*storedGeneration) > projection.SnapshotGeneration

	tag, err := transaction.Exec(ctx, `
		INSERT INTO connectivity_snapshots (
			node_id, event_id, boot_session_id, node_sequence, observed_at,
			snapshot_generation, reducer_version,
			bundle_generation, root_generation, user_generation,
			aggregate_state, authorization_state, authorization_reason,
			open_gaps, gap_overflow, source_conflicts,
			awaiting_baseline, conflict_overflow, lineage_reset, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
			$11, $12, $13, $14, $15, $16, $17, $18, $19, $20
		)
		ON CONFLICT (node_id) DO UPDATE SET
			event_id = EXCLUDED.event_id,
			boot_session_id = EXCLUDED.boot_session_id,
			node_sequence = EXCLUDED.node_sequence,
			observed_at = EXCLUDED.observed_at,
			snapshot_generation = EXCLUDED.snapshot_generation,
			reducer_version = EXCLUDED.reducer_version,
			bundle_generation = EXCLUDED.bundle_generation,
			root_generation = EXCLUDED.root_generation,
			user_generation = EXCLUDED.user_generation,
			aggregate_state = EXCLUDED.aggregate_state,
			authorization_state = EXCLUDED.authorization_state,
			authorization_reason = EXCLUDED.authorization_reason,
			open_gaps = EXCLUDED.open_gaps,
			gap_overflow = EXCLUDED.gap_overflow,
			source_conflicts = EXCLUDED.source_conflicts,
			awaiting_baseline = EXCLUDED.awaiting_baseline,
			conflict_overflow = EXCLUDED.conflict_overflow,
			lineage_reset = EXCLUDED.lineage_reset,
			updated_at = EXCLUDED.updated_at
		WHERE (
			connectivity_snapshots.observed_at,
			connectivity_snapshots.node_sequence
		) < (EXCLUDED.observed_at, EXCLUDED.node_sequence)
	`,
		string(item.nodeID),
		string(item.eventID),
		string(item.sessionID),
		int64(item.sequence),
		item.occurredAt,
		int64(projection.SnapshotGeneration),
		int(projection.ReducerVersion),
		int64(projection.BundleGeneration),
		int64(projection.RootGeneration),
		int64(projection.UserGeneration),
		projection.Aggregate,
		projection.Authorization,
		projection.AuthorizationReason,
		int(projection.OpenGaps),
		projection.GapOverflow,
		int(projection.SourceConflicts),
		int(projection.AwaitingBaseline),
		projection.ConflictOverflow,
		reset,
		item.occurredAt,
	)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		// A newer projection is already stored. Nothing to do, and nothing
		// to report as an error: arriving late is ordinary.
		return false, transaction.Commit(ctx)
	}

	if _, err = transaction.Exec(ctx, `
		DELETE FROM connectivity_snapshot_components WHERE node_id = $1
	`, string(item.nodeID)); err != nil {
		return false, err
	}
	for _, component := range projection.Components {
		if _, err = transaction.Exec(ctx, `
			INSERT INTO connectivity_snapshot_components (
				node_id, component, component_state, freshness, diff_reason
			) VALUES ($1, $2, $3, $4, $5)
		`,
			string(item.nodeID),
			component.Component,
			component.State,
			string(component.Freshness),
			component.Reason,
		); err != nil {
			return false, err
		}
	}

	if _, err = transaction.Exec(ctx, `
		DELETE FROM connectivity_snapshot_proposal_classes WHERE node_id = $1
	`, string(item.nodeID)); err != nil {
		return false, err
	}
	for _, class := range projection.ProposalClasses {
		if _, err = transaction.Exec(ctx, `
			INSERT INTO connectivity_snapshot_proposal_classes (
				node_id, proposal_class, proposal_count
			) VALUES ($1, $2, $3)
		`, string(item.nodeID), class.Class, int(class.Count)); err != nil {
			return false, err
		}
	}
	if err = transaction.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

// Load returns the stored projection for every node that has one.
func (store *PostgresStore) Load(
	ctx context.Context,
	limit int,
) ([]Snapshot, error) {
	if store == nil || store.database == nil || ctx == nil ||
		limit <= 0 || limit > MaxProjectionBatch {
		return nil, ErrInvalidProjection
	}
	rows, err := store.database.Query(ctx, `
		SELECT
			node_id::text,
			COALESCE(event_id::text, ''),
			boot_session_id::text,
			node_sequence,
			observed_at,
			snapshot_generation,
			reducer_version,
			bundle_generation,
			root_generation,
			user_generation,
			aggregate_state,
			authorization_state,
			authorization_reason,
			open_gaps,
			gap_overflow,
			source_conflicts,
			awaiting_baseline,
			conflict_overflow,
			lineage_reset,
			updated_at
		FROM connectivity_snapshots
		ORDER BY node_id
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, ErrProjectionStore
	}
	defer rows.Close()

	snapshots := make([]Snapshot, 0)
	index := make(map[metadata.UUID]int)
	for rows.Next() {
		var (
			snapshot   Snapshot
			nodeID     string
			eventID    string
			sessionID  string
			sequence   int64
			generation int64
			reducer    int
			bundle     int64
			root       int64
			user       int64
			openGaps   int
			conflicts  int
			awaiting   int
		)
		if err := rows.Scan(
			&nodeID,
			&eventID,
			&sessionID,
			&sequence,
			&snapshot.ObservedAt,
			&generation,
			&reducer,
			&bundle,
			&root,
			&user,
			&snapshot.Aggregate,
			&snapshot.Authorization,
			&snapshot.AuthorizationReason,
			&openGaps,
			&snapshot.GapOverflow,
			&conflicts,
			&awaiting,
			&snapshot.ConflictOverflow,
			&snapshot.LineageReset,
			&snapshot.UpdatedAt,
		); err != nil {
			return nil, ErrProjectionStore
		}
		parsedNode, nodeErr := metadata.ParseUUID(nodeID)
		parsedSession, sessionErr := metadata.ParseUUID(sessionID)
		if nodeErr != nil || sessionErr != nil {
			return nil, ErrInvalidProjection
		}
		snapshot.NodeID = parsedNode
		snapshot.SessionID = parsedSession
		if eventID != "" {
			parsedEvent, eventErr := metadata.ParseUUID(eventID)
			if eventErr != nil {
				return nil, ErrInvalidProjection
			}
			snapshot.EventID = parsedEvent
		}
		snapshot.Sequence = uint64(sequence)
		snapshot.ObservedAt = snapshot.ObservedAt.UTC()
		snapshot.UpdatedAt = snapshot.UpdatedAt.UTC()
		snapshot.SnapshotGeneration = uint64(generation)
		snapshot.ReducerVersion = uint16(reducer)
		snapshot.BundleGeneration = uint64(bundle)
		snapshot.RootGeneration = uint64(root)
		snapshot.UserGeneration = uint64(user)
		snapshot.OpenGaps = uint16(openGaps)
		snapshot.SourceConflicts = uint16(conflicts)
		snapshot.AwaitingBaseline = uint16(awaiting)
		index[snapshot.NodeID] = len(snapshots)
		snapshots = append(snapshots, snapshot)
	}
	if err := rows.Err(); err != nil {
		return nil, ErrProjectionStore
	}
	if len(snapshots) == 0 {
		return snapshots, nil
	}
	if err := store.loadComponents(ctx, snapshots, index); err != nil {
		return nil, err
	}
	if err := store.loadProposalClasses(ctx, snapshots, index); err != nil {
		return nil, err
	}
	return snapshots, nil
}

func (store *PostgresStore) loadComponents(
	ctx context.Context,
	snapshots []Snapshot,
	index map[metadata.UUID]int,
) error {
	rows, err := store.database.Query(ctx, `
		SELECT node_id::text, component, component_state, freshness, diff_reason
		FROM connectivity_snapshot_components
		ORDER BY node_id, component
	`)
	if err != nil {
		return ErrProjectionStore
	}
	defer rows.Close()
	for rows.Next() {
		var nodeID string
		var component Component
		if err := rows.Scan(
			&nodeID,
			&component.Component,
			&component.State,
			&component.Freshness,
			&component.DiffReason,
		); err != nil {
			return ErrProjectionStore
		}
		parsed, parseErr := metadata.ParseUUID(nodeID)
		if parseErr != nil {
			return ErrInvalidProjection
		}
		if position, known := index[parsed]; known {
			snapshots[position].Components = append(
				snapshots[position].Components, component)
		}
	}
	if err := rows.Err(); err != nil {
		return ErrProjectionStore
	}
	return nil
}

func (store *PostgresStore) loadProposalClasses(
	ctx context.Context,
	snapshots []Snapshot,
	index map[metadata.UUID]int,
) error {
	rows, err := store.database.Query(ctx, `
		SELECT node_id::text, proposal_class, proposal_count
		FROM connectivity_snapshot_proposal_classes
		ORDER BY node_id, proposal_class
	`)
	if err != nil {
		return ErrProjectionStore
	}
	defer rows.Close()
	for rows.Next() {
		var nodeID string
		var class string
		var count int
		if err := rows.Scan(&nodeID, &class, &count); err != nil {
			return ErrProjectionStore
		}
		parsed, parseErr := metadata.ParseUUID(nodeID)
		if parseErr != nil || count < 1 {
			return ErrInvalidProjection
		}
		if position, known := index[parsed]; known {
			snapshots[position].ProposalClasses = append(
				snapshots[position].ProposalClasses,
				ProposalClass{Class: class, Count: uint16(count)},
			)
		}
	}
	if err := rows.Err(); err != nil {
		return ErrProjectionStore
	}
	for position := range snapshots {
		sort.Slice(snapshots[position].ProposalClasses, func(i, j int) bool {
			return snapshots[position].ProposalClasses[i].Class <
				snapshots[position].ProposalClasses[j].Class
		})
	}
	return nil
}
