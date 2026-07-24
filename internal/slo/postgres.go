package slo

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"strconv"
	"sync"

	"github.com/jackc/pgx/v5"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

type Database interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

type PostgresStore struct {
	database Database
	random   io.Reader
	randomMu sync.Mutex
}

func NewPostgresStore(
	database Database,
	randomSource io.Reader,
) (*PostgresStore, error) {
	if database == nil {
		return nil, ErrInvalidSLO
	}
	if randomSource == nil {
		randomSource = rand.Reader
	}
	return &PostgresStore{
		database: database,
		random:   randomSource,
	}, nil
}

func (store *PostgresStore) Upsert(
	ctx context.Context,
	aggregate Aggregate,
) (aggregateID metadata.UUID, err error) {
	if store == nil ||
		store.database == nil ||
		ctx == nil ||
		validateAggregate(aggregate) != nil {
		return "", ErrInvalidSLO
	}
	transaction, err := store.database.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", err
	}
	defer rollback(ctx, transaction, &err)
	if _, err = transaction.Exec(
		ctx,
		"SELECT pg_advisory_xact_lock(hashtextextended($1, 0))",
		aggregateLockIdentity(aggregate),
	); err != nil {
		return "", err
	}

	var existingID string
	err = transaction.QueryRow(ctx, `
		SELECT slo_aggregate_id::text
		FROM slo_aggregates
		WHERE granularity = $1
		  AND target_key = $2
		  AND service = $3
		  AND objective = $4
		  AND window_start = $5
		FOR UPDATE
	`,
		string(aggregate.Granularity),
		aggregate.TargetKey,
		string(aggregate.Service),
		aggregate.Objective,
		aggregate.WindowStart.UTC(),
	).Scan(&existingID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	if existingID == "" {
		aggregateID, err = store.nextID()
		if err != nil {
			return "", err
		}
		_, err = transaction.Exec(ctx, `
			INSERT INTO slo_aggregates (
				slo_aggregate_id,
				granularity,
				target_key,
				node_id,
				service,
				objective,
				window_start,
				window_end,
				eligible_milliseconds,
				good_milliseconds,
				bad_milliseconds,
				excluded_milliseconds,
				qualifying_count,
				total_count,
				computed_at
			) VALUES (
				$1, $2, $3, $4, $5, $6, $7, $8,
				$9, $10, $11, $12, $13, $14, $15
			)
		`,
			string(aggregateID),
			string(aggregate.Granularity),
			aggregate.TargetKey,
			nullableUUID(aggregate.NodeID),
			string(aggregate.Service),
			aggregate.Objective,
			aggregate.WindowStart.UTC(),
			aggregate.WindowEnd.UTC(),
			aggregate.EligibleMilliseconds,
			aggregate.GoodMilliseconds,
			aggregate.BadMilliseconds,
			aggregate.ExcludedMilliseconds,
			int64(aggregate.QualifyingCount),
			int64(aggregate.TotalCount),
			aggregate.ComputedAt.UTC(),
		)
	} else {
		aggregateID = metadata.UUID(existingID)
		_, err = transaction.Exec(ctx, `
			UPDATE slo_aggregates
			SET node_id = $2,
			    window_end = $3,
			    eligible_milliseconds = $4,
			    good_milliseconds = $5,
			    bad_milliseconds = $6,
			    excluded_milliseconds = $7,
			    qualifying_count = $8,
			    total_count = $9,
			    computed_at = $10
			WHERE slo_aggregate_id = $1
		`,
			existingID,
			nullableUUID(aggregate.NodeID),
			aggregate.WindowEnd.UTC(),
			aggregate.EligibleMilliseconds,
			aggregate.GoodMilliseconds,
			aggregate.BadMilliseconds,
			aggregate.ExcludedMilliseconds,
			int64(aggregate.QualifyingCount),
			int64(aggregate.TotalCount),
			aggregate.ComputedAt.UTC(),
		)
	}
	if err != nil {
		return "", err
	}
	if _, err = transaction.Exec(ctx, `
		DELETE FROM slo_incident_links
		WHERE slo_aggregate_id = $1
	`, string(aggregateID)); err != nil {
		return "", err
	}
	for _, link := range aggregate.Links {
		if _, err = transaction.Exec(ctx, `
			INSERT INTO slo_incident_links (
				slo_aggregate_id,
				incident_id,
				linkage_role
			) VALUES ($1, $2, $3)
		`,
			string(aggregateID),
			string(link.IncidentID),
			string(link.Role),
		); err != nil {
			return "", err
		}
	}
	if err = transaction.Commit(ctx); err != nil {
		return "", err
	}
	return aggregateID, nil
}

func (store *PostgresStore) nextID() (metadata.UUID, error) {
	store.randomMu.Lock()
	defer store.randomMu.Unlock()
	return metadata.NewUUID(store.random)
}

func nullableUUID(value metadata.UUID) any {
	if value == "" {
		return nil
	}
	return string(value)
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

func aggregateLockIdentity(aggregate Aggregate) string {
	return aggregate.TargetKey + ":" +
		string(aggregate.Service) + ":" +
		aggregate.Objective + ":" +
		strconv.FormatInt(aggregate.WindowStart.UTC().UnixNano(), 10)
}
