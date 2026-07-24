package silentnode

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mrAndreyIsachenko/hexroute/internal/event"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

type Database interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

type PostgresStore struct {
	database Database
}

type sleepWireRecord struct {
	Schema   event.Schema    `json:"schema"`
	Version  uint16          `json:"version"`
	Priority event.Priority  `json:"priority"`
	Payload  json.RawMessage `json:"payload"`
}

var (
	ErrSleepEventNotFound = errors.New("sleep event not found")
	ErrInvalidSleepEvent  = errors.New("invalid sleep event")
)

func NewPostgresStore(database Database) (*PostgresStore, error) {
	if database == nil {
		return nil, ErrInvalidNode
	}
	return &PostgresStore{database: database}, nil
}

func (store *PostgresStore) Decisions(
	ctx context.Context,
	policy Policy,
	at time.Time,
) ([]Decision, error) {
	if err := policy.Validate(); err != nil || at.IsZero() {
		return nil, ErrInvalidPolicy
	}
	rows, err := store.database.Query(ctx, `
		SELECT
			n.node_id::text,
			n.node_kind,
			n.lifecycle_status,
			n.expected_heartbeat_seconds,
			n.created_at,
			n.last_seen_at,
			EXISTS (
				SELECT 1
				FROM sleep_intervals s
				WHERE s.node_id = n.node_id
				  AND s.started_at <= $1
				  AND (s.ended_at IS NULL OR s.ended_at > $1)
			)
		FROM nodes n
		ORDER BY n.node_id
	`, at.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	decisions := make([]Decision, 0)
	for rows.Next() {
		var (
			nodeIDString    string
			nodeKind        string
			lifecycleStatus string
			expectedSeconds int64
			createdAt       time.Time
			lastSeenAt      *time.Time
			sleeping        bool
		)
		if err := rows.Scan(
			&nodeIDString,
			&nodeKind,
			&lifecycleStatus,
			&expectedSeconds,
			&createdAt,
			&lastSeenAt,
			&sleeping,
		); err != nil {
			return nil, err
		}
		nodeID, err := metadata.ParseUUID(nodeIDString)
		if err != nil {
			return nil, ErrInvalidNode
		}
		node := Node{
			NodeID:                    nodeID,
			NodeKind:                  nodeKind,
			LifecycleStatus:           lifecycleStatus,
			ExpectedHeartbeatInterval: time.Duration(expectedSeconds) * time.Second,
			CreatedAt:                 createdAt.UTC(),
			SleepingAtEvaluation:      sleeping,
		}
		if lastSeenAt != nil {
			node.LastSeenAt = lastSeenAt.UTC()
		}
		decision, err := Evaluate(node, policy, at)
		if err != nil {
			return nil, err
		}
		decisions = append(decisions, decision)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return decisions, nil
}

func (store *PostgresStore) ProjectSleepEvent(
	ctx context.Context,
	eventID metadata.UUID,
) (err error) {
	if _, parseErr := metadata.ParseUUID(string(eventID)); parseErr != nil {
		return ErrInvalidSleepEvent
	}
	transaction, err := store.database.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() {
		rollbackErr := transaction.Rollback(ctx)
		if err == nil && rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			err = rollbackErr
		}
	}()

	var (
		nodeIDString    string
		sessionIDString string
		nodeKind        string
		occurredAt      time.Time
		version         uint16
		priority        event.Priority
		payload         []byte
	)
	err = transaction.QueryRow(ctx, `
		SELECT
			e.node_id::text,
			e.boot_session_id::text,
			n.node_kind,
			e.occurred_at,
			e.schema_version,
			e.priority,
			e.payload
		FROM events e
		JOIN nodes n ON n.node_id = e.node_id
		WHERE e.event_id = $1 AND e.schema_name = $2
	`, string(eventID), string(event.SchemaSleep)).Scan(
		&nodeIDString,
		&sessionIDString,
		&nodeKind,
		&occurredAt,
		&version,
		&priority,
		&payload,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrSleepEventNotFound
	}
	if err != nil {
		return err
	}
	if nodeKind != "mac" {
		return ErrInvalidSleepEvent
	}
	nodeID, nodeErr := metadata.ParseUUID(nodeIDString)
	sessionID, sessionErr := metadata.ParseUUID(sessionIDString)
	if nodeErr != nil || sessionErr != nil {
		return ErrInvalidSleepEvent
	}
	sleep, err := decodeSleep(version, priority, payload)
	if err != nil {
		return err
	}
	switch sleep.Phase {
	case event.SleepStarted:
		err = projectSleepStart(
			ctx,
			transaction,
			eventID,
			nodeID,
			sessionID,
			occurredAt.UTC(),
			sleep.Reason,
		)
	case event.SleepEnded:
		err = projectSleepEnd(
			ctx,
			transaction,
			eventID,
			nodeID,
			sessionID,
			occurredAt.UTC(),
			sleep.Reason,
		)
	default:
		err = ErrInvalidSleepEvent
	}
	if err != nil {
		return err
	}
	return transaction.Commit(ctx)
}

func decodeSleep(
	version uint16,
	priority event.Priority,
	payload json.RawMessage,
) (event.Sleep, error) {
	encoded, err := json.Marshal(sleepWireRecord{
		Schema:   event.SchemaSleep,
		Version:  version,
		Priority: priority,
		Payload:  payload,
	})
	if err != nil {
		return event.Sleep{}, ErrInvalidSleepEvent
	}
	record, err := event.Decode(encoded)
	if err != nil {
		return event.Sleep{}, ErrInvalidSleepEvent
	}
	sleep, ok := record.Payload.(*event.Sleep)
	if !ok || sleep == nil {
		return event.Sleep{}, ErrInvalidSleepEvent
	}
	return *sleep, nil
}

func projectSleepStart(
	ctx context.Context,
	transaction pgx.Tx,
	eventID metadata.UUID,
	nodeID metadata.UUID,
	sessionID metadata.UUID,
	occurredAt time.Time,
	reason event.SleepReason,
) error {
	_, err := transaction.Exec(ctx, `
		INSERT INTO sleep_intervals (
			sleep_interval_id,
			node_id,
			boot_session_id,
			started_at,
			start_event_id,
			reason_code
		)
		SELECT $1, $2, $3, $4, $1, $5
		WHERE NOT EXISTS (
			SELECT 1
			FROM sleep_intervals
			WHERE node_id = $2 AND ended_at IS NULL
		)
		ON CONFLICT DO NOTHING
	`,
		string(eventID),
		string(nodeID),
		string(sessionID),
		occurredAt,
		string(reason),
	)
	return err
}

func projectSleepEnd(
	ctx context.Context,
	transaction pgx.Tx,
	eventID metadata.UUID,
	nodeID metadata.UUID,
	sessionID metadata.UUID,
	occurredAt time.Time,
	reason event.SleepReason,
) error {
	var (
		intervalID string
		startedAt  time.Time
	)
	err := transaction.QueryRow(ctx, `
		SELECT sleep_interval_id::text, started_at
		FROM sleep_intervals
		WHERE node_id = $1 AND ended_at IS NULL
		ORDER BY started_at DESC
		LIMIT 1
		FOR UPDATE
	`, string(nodeID)).Scan(&intervalID, &startedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		_, err = transaction.Exec(ctx, `
			INSERT INTO sleep_intervals (
				sleep_interval_id,
				node_id,
				boot_session_id,
				started_at,
				ended_at,
				end_event_id,
				reason_code
			) VALUES ($1, $2, $3, $4, $4, $1, $5)
			ON CONFLICT DO NOTHING
		`,
			string(eventID),
			string(nodeID),
			string(sessionID),
			occurredAt,
			string(reason),
		)
		return err
	}
	if err != nil {
		return err
	}
	if occurredAt.Before(startedAt) {
		return ErrInvalidSleepEvent
	}
	_, err = transaction.Exec(ctx, `
		UPDATE sleep_intervals
		SET ended_at = $2,
		    end_event_id = $3
		WHERE sleep_interval_id = $1
	`, intervalID, occurredAt, string(eventID))
	return err
}
