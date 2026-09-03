package incidentbundle

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

type PostgresStore struct {
	database Database
}

func NewPostgresStore(database Database) (*PostgresStore, error) {
	if database == nil {
		return nil, ErrInvalidBundle
	}
	return &PostgresStore{database: database}, nil
}

func (store *PostgresStore) ClaimExpired(
	ctx context.Context,
	workerID metadata.UUID,
	at time.Time,
	limit int,
) (deletions []Deletion, err error) {
	if store == nil ||
		store.database == nil ||
		ctx == nil ||
		!validUUID(workerID) ||
		at.IsZero() ||
		limit <= 0 ||
		limit > maxDeleteBatch {
		return nil, ErrInvalidBundle
	}
	at = at.UTC()
	transaction, err := store.database.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer rollback(ctx, transaction, &err)
	rows, err := transaction.Query(ctx, `
		WITH candidates AS (
			SELECT incident_bundle_id
			FROM incident_bundles
			WHERE deleted_at IS NULL
			  AND expires_at <= $1
			  AND next_delete_attempt_at <= $1
			  AND (delete_claim_until IS NULL OR delete_claim_until <= $1)
			ORDER BY next_delete_attempt_at, incident_bundle_id
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		UPDATE incident_bundles b
		SET delete_claim_owner = $3,
		    delete_claim_until = $4,
		    delete_attempt_count = LEAST(delete_attempt_count + 1, 1000)
		FROM candidates c
		WHERE b.incident_bundle_id = c.incident_bundle_id
		RETURNING
			b.incident_bundle_id::text,
			b.object_key,
			b.delete_attempt_count,
			b.delete_claim_until
	`, at, limit, string(workerID), at.Add(deleteLease))
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var (
			bundleID     string
			objectKey    string
			attempt      uint32
			claimedUntil time.Time
		)
		if err := rows.Scan(
			&bundleID,
			&objectKey,
			&attempt,
			&claimedUntil,
		); err != nil {
			rows.Close()
			return nil, err
		}
		deletions = append(deletions, Deletion{
			BundleID:     metadata.UUID(bundleID),
			ObjectKey:    objectKey,
			Attempt:      attempt,
			ClaimedUntil: claimedUntil.UTC(),
		})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	if err = transaction.Commit(ctx); err != nil {
		return nil, err
	}
	return deletions, nil
}

func (store *PostgresStore) CompleteDeletion(
	ctx context.Context,
	workerID metadata.UUID,
	deletion Deletion,
	completion DeletionCompletion,
	at time.Time,
) (err error) {
	if store == nil ||
		store.database == nil ||
		ctx == nil ||
		!validUUID(workerID) ||
		!validUUID(deletion.BundleID) ||
		deletion.Attempt == 0 ||
		(completion != DeletionSucceeded &&
			completion != DeletionUnavailable) ||
		at.IsZero() {
		return ErrInvalidBundle
	}
	at = at.UTC()
	transaction, err := store.database.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer rollback(ctx, transaction, &err)
	var (
		claimOwner *string
		attempt    uint32
	)
	err = transaction.QueryRow(ctx, `
		SELECT delete_claim_owner::text, delete_attempt_count
		FROM incident_bundles
		WHERE incident_bundle_id = $1
		  AND deleted_at IS NULL
		FOR UPDATE
	`, string(deletion.BundleID)).Scan(&claimOwner, &attempt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrBundleNotFound
	}
	if err != nil {
		return err
	}
	if claimOwner == nil ||
		*claimOwner != string(workerID) ||
		attempt != deletion.Attempt {
		return ErrBundleClaimLost
	}

	switch completion {
	case DeletionSucceeded:
		_, err = transaction.Exec(ctx, `
			UPDATE incident_bundles
			SET deleted_at = $2,
			    delete_claim_owner = NULL,
			    delete_claim_until = NULL,
			    next_delete_attempt_at = $2,
			    last_delete_result_code = 'object_deleted'
			WHERE incident_bundle_id = $1
		`, string(deletion.BundleID), at)
	case DeletionUnavailable:
		_, err = transaction.Exec(ctx, `
			UPDATE incident_bundles
			SET delete_claim_owner = NULL,
			    delete_claim_until = NULL,
			    next_delete_attempt_at = $2,
			    last_delete_result_code = 'object_delete_unavailable'
			WHERE incident_bundle_id = $1
		`,
			string(deletion.BundleID),
			at.Add(retryDelay(attempt)),
		)
	}
	if err != nil {
		return err
	}
	return transaction.Commit(ctx)
}

func retryDelay(attempt uint32) time.Duration {
	delay := minRetryDelay
	for current := uint32(1); current < attempt && delay < maxRetryDelay; current++ {
		delay *= 2
		if delay >= maxRetryDelay {
			return maxRetryDelay
		}
	}
	return delay
}

func validUUID(value metadata.UUID) bool {
	_, err := metadata.ParseUUID(string(value))
	return err == nil
}

func rollback(
	ctx context.Context,
	transaction pgx.Tx,
	resultErr *error,
) {
	rollbackErr := transaction.Rollback(ctx)
	if *resultErr == nil &&
		rollbackErr != nil &&
		!errors.Is(rollbackErr, pgx.ErrTxClosed) {
		*resultErr = rollbackErr
	}
}

// maxPendingBatch bounds one pass over incidents that have never been
// bundled. The bound exists because the first pass on an installed deployment
// meets every closed incident it has ever recorded at once.
const maxPendingBatch = 64

// PendingClosedIncidents names closed incidents that have never been bundled
// and have evidence to bundle.
//
// Two exclusions decide this query, and both come from what Create does.
//
// An incident with no linked event returns ErrNoIncidentEvidence, and nothing
// about the incident will change to make that untrue later; selecting one
// would fail the same way on every pass, forever, and bury the failures that
// mean something.
//
// An incident whose bundle was deleted is excluded by the absence of any row,
// not by deleted_at. Deletion happens only at the recorded expiry, and Create
// revives a deleted row rather than skipping it — so selecting on "no live
// bundle" would have this pass resurrect what expiry just removed, and the two
// would undo each other every interval for as long as the deployment runs.
// Retention is the reason the object went away; recreating it here would make
// retention unreachable. A deliberate later request may still repopulate the
// row, which is what the expiry scenario reserves.
func (store *PostgresStore) PendingClosedIncidents(
	ctx context.Context,
	limit int,
) (incidents []metadata.UUID, err error) {
	if store == nil ||
		store.database == nil ||
		ctx == nil ||
		limit <= 0 ||
		limit > maxPendingBatch {
		return nil, ErrInvalidBundle
	}
	transaction, err := store.database.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer rollback(ctx, transaction, &err)
	rows, err := transaction.Query(ctx, `
		SELECT i.incident_id::text
		FROM incidents i
		WHERE i.incident_status = 'resolved'
		  AND NOT EXISTS (
			SELECT 1
			FROM incident_bundles b
			WHERE b.incident_id = i.incident_id
		  )
		  AND EXISTS (
			SELECT 1
			FROM incident_events e
			WHERE e.incident_id = i.incident_id
		  )
		ORDER BY i.last_observed_at, i.incident_id
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var incidentID string
		if err := rows.Scan(&incidentID); err != nil {
			rows.Close()
			return nil, err
		}
		incidents = append(incidents, metadata.UUID(incidentID))
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	if err = transaction.Commit(ctx); err != nil {
		return nil, err
	}
	return incidents, nil
}
