package alertdelivery

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

const maxOutboxBatch = 50

type outboxItem struct {
	snapshot Snapshot
	attempts uint32
}

func (store *PostgresStore) DrainOutbox(
	ctx context.Context,
	workerID metadata.UUID,
	at time.Time,
	limit int,
) (int, error) {
	if store == nil ||
		store.database == nil ||
		ctx == nil ||
		metadataUUID(workerID) == "" ||
		at.IsZero() ||
		limit <= 0 ||
		limit > maxOutboxBatch {
		return 0, ErrInvalidDelivery
	}
	items, err := store.claimOutbox(ctx, workerID, at.UTC(), limit)
	if err != nil {
		return 0, err
	}
	processed := 0
	for _, item := range items {
		if err := store.completeOutbox(
			ctx,
			workerID,
			item,
			at.UTC(),
		); err != nil {
			return processed, err
		}
		processed++
	}
	return processed, nil
}

func (store *PostgresStore) claimOutbox(
	ctx context.Context,
	workerID metadata.UUID,
	at time.Time,
	limit int,
) (items []outboxItem, err error) {
	transaction, err := store.database.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer rollback(ctx, transaction, &err)
	rows, err := transaction.Query(ctx, `
		SELECT
			incident_id::text,
			incident_generation,
			node_id::text,
			snapshot_status,
			snapshot_severity,
			snapshot_category,
			snapshot_component,
			snapshot_requires_action,
			snapshot_transitioned_at,
			attempt_count
		FROM incident_alert_outbox
		WHERE processed_at IS NULL
		  AND (claim_until IS NULL OR claim_until <= $1)
		ORDER BY snapshot_transitioned_at, incident_id, incident_generation
		LIMIT $2
		FOR UPDATE SKIP LOCKED
	`, at, limit)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		item, scanErr := scanOutbox(rows)
		if scanErr != nil {
			rows.Close()
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	claimUntil := at.Add(store.policy.LeaseDuration)
	for index := range items {
		attempts := items[index].attempts
		if attempts < 1000 {
			attempts++
		}
		tag, updateErr := transaction.Exec(ctx, `
			UPDATE incident_alert_outbox
			SET claim_owner = $3,
			    claim_until = $4,
			    attempt_count = $5,
			    last_result_code = 'claimed'
			WHERE incident_id = $1
			  AND incident_generation = $2
			  AND processed_at IS NULL
		`,
			string(items[index].snapshot.IncidentID),
			items[index].snapshot.Generation,
			string(workerID),
			claimUntil,
			attempts,
		)
		if updateErr != nil {
			return nil, updateErr
		}
		if tag.RowsAffected() != 1 {
			return nil, ErrDeliveryClaimLost
		}
		items[index].attempts = attempts
	}
	if err = transaction.Commit(ctx); err != nil {
		return nil, err
	}
	return items, nil
}

func (store *PostgresStore) completeOutbox(
	ctx context.Context,
	workerID metadata.UUID,
	item outboxItem,
	at time.Time,
) (err error) {
	plan, err := store.policy.Plan(item.snapshot)
	if err != nil {
		return err
	}
	deliveryIDs := make([]metadata.UUID, len(plan))
	for index := range plan {
		deliveryIDs[index], err = store.nextID()
		if err != nil {
			return err
		}
	}
	transaction, err := store.database.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer rollback(ctx, transaction, &err)
	var claimOwner *string
	err = transaction.QueryRow(ctx, `
		SELECT claim_owner::text
		FROM incident_alert_outbox
		WHERE incident_id = $1
		  AND incident_generation = $2
		  AND processed_at IS NULL
		  AND claim_until > $3
		FOR UPDATE
	`,
		string(item.snapshot.IncidentID),
		item.snapshot.Generation,
		at,
	).Scan(&claimOwner)
	if errors.Is(err, pgx.ErrNoRows) ||
		claimOwner == nil ||
		*claimOwner != string(workerID) {
		return ErrDeliveryClaimLost
	}
	if err != nil {
		return err
	}
	for index, planned := range plan {
		_, err = transaction.Exec(ctx, `
			INSERT INTO alert_deliveries (
				alert_delivery_id,
				incident_id,
				incident_generation,
				channel,
				delivery_status,
				actionable,
				next_attempt_at,
				last_result_code,
				snapshot_status,
				snapshot_severity,
				snapshot_category,
				snapshot_component,
				snapshot_transitioned_at,
				updated_at
			) VALUES (
				$1, $2, $3, $4, $5, $6, $7, $8,
				$9, $10, $11, $12, $13, CURRENT_TIMESTAMP
			)
			ON CONFLICT (incident_id, incident_generation, channel) DO NOTHING
		`,
			string(deliveryIDs[index]),
			string(item.snapshot.IncidentID),
			item.snapshot.Generation,
			string(planned.Channel),
			string(planned.Status),
			planned.Actionable,
			nullableTime(planned.NextAttemptAt),
			nullableString(planned.LastResultCode),
			string(item.snapshot.Status),
			string(item.snapshot.Severity),
			string(item.snapshot.Category),
			string(item.snapshot.Component),
			item.snapshot.TransitionedAt,
		)
		if err != nil {
			return err
		}
	}
	tag, err := transaction.Exec(ctx, `
		UPDATE incident_alert_outbox
		SET processed_at = $4,
		    claim_owner = NULL,
		    claim_until = NULL,
		    last_result_code = 'queued'
		WHERE incident_id = $1
		  AND incident_generation = $2
		  AND claim_owner = $3
		  AND processed_at IS NULL
	`,
		string(item.snapshot.IncidentID),
		item.snapshot.Generation,
		string(workerID),
		at,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrDeliveryClaimLost
	}
	return transaction.Commit(ctx)
}

func scanOutbox(scanner deliveryScanner) (outboxItem, error) {
	var (
		incidentID string
		nodeID     *string
		generation int64
		attempts   int64
		item       outboxItem
	)
	err := scanner.Scan(
		&incidentID,
		&generation,
		&nodeID,
		&item.snapshot.Status,
		&item.snapshot.Severity,
		&item.snapshot.Category,
		&item.snapshot.Component,
		&item.snapshot.RequiresAction,
		&item.snapshot.TransitionedAt,
		&attempts,
	)
	if err != nil {
		return outboxItem{}, err
	}
	item.snapshot.IncidentID, err = metadata.ParseUUID(incidentID)
	if err != nil || generation <= 0 || attempts < 0 || attempts > 1000 {
		return outboxItem{}, ErrInvalidDelivery
	}
	item.snapshot.Generation = uint64(generation)
	item.attempts = uint32(attempts)
	if nodeID != nil {
		item.snapshot.NodeID, err = metadata.ParseUUID(*nodeID)
		if err != nil {
			return outboxItem{}, ErrInvalidDelivery
		}
	}
	item.snapshot.TransitionedAt = item.snapshot.TransitionedAt.UTC()
	if err := validateSnapshot(item.snapshot); err != nil {
		return outboxItem{}, err
	}
	return item, nil
}
