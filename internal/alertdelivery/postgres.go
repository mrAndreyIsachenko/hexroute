package alertdelivery

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mrAndreyIsachenko/hexroute/internal/cloudincident"
	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/event"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

type Database interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

type PostgresStore struct {
	database Database
	policy   Policy
	random   io.Reader
	randomMu sync.Mutex
}

func NewPostgresStore(
	database Database,
	policy Policy,
	randomSource io.Reader,
) (*PostgresStore, error) {
	if database == nil || policy.Validate() != nil {
		return nil, ErrInvalidDelivery
	}
	if randomSource == nil {
		randomSource = rand.Reader
	}
	return &PostgresStore{
		database: database,
		policy:   policy,
		random:   randomSource,
	}, nil
}

func (store *PostgresStore) QueueGeneration(
	ctx context.Context,
	incidentID metadata.UUID,
	generation uint64,
) (queued int, err error) {
	if ctx == nil ||
		generation == 0 ||
		metadataUUID(incidentID) == "" {
		return 0, ErrInvalidDelivery
	}
	transaction, err := store.database.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, err
	}
	defer rollback(ctx, transaction, &err)
	if _, err = transaction.Exec(
		ctx,
		"SELECT pg_advisory_xact_lock(hashtextextended($1, 0))",
		string(incidentID)+":"+strconv.FormatUint(generation, 10),
	); err != nil {
		return 0, err
	}
	snapshot, err := loadGeneration(ctx, transaction, incidentID, generation)
	if err != nil {
		return 0, err
	}
	plan, err := store.policy.Plan(snapshot)
	if err != nil {
		return 0, err
	}
	for _, item := range plan {
		deliveryID, idErr := store.nextID()
		if idErr != nil {
			return 0, idErr
		}
		tag, insertErr := transaction.Exec(ctx, `
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
			string(deliveryID),
			string(snapshot.IncidentID),
			snapshot.Generation,
			string(item.Channel),
			string(item.Status),
			item.Actionable,
			nullableTime(item.NextAttemptAt),
			nullableString(item.LastResultCode),
			string(snapshot.Status),
			string(snapshot.Severity),
			string(snapshot.Category),
			string(snapshot.Component),
			snapshot.TransitionedAt,
		)
		if insertErr != nil {
			return 0, insertErr
		}
		queued += int(tag.RowsAffected())
	}
	if err = transaction.Commit(ctx); err != nil {
		return 0, err
	}
	return queued, nil
}

func (store *PostgresStore) AcknowledgeLocal(
	ctx context.Context,
	incidentID metadata.UUID,
	generation uint64,
	deliveredAt time.Time,
) (err error) {
	if ctx == nil ||
		metadataUUID(incidentID) == "" ||
		generation == 0 ||
		deliveredAt.IsZero() {
		return ErrInvalidDelivery
	}
	deliveredAt = deliveredAt.UTC()
	transaction, err := store.database.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer rollback(ctx, transaction, &err)
	var deliveryID string
	err = transaction.QueryRow(ctx, `
		SELECT alert_delivery_id::text
		FROM alert_deliveries
		WHERE incident_id = $1
		  AND incident_generation = $2
		  AND channel = 'local_macos'
		FOR UPDATE
	`, string(incidentID), generation).Scan(&deliveryID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrDeliveryNotFound
	}
	if err != nil {
		return err
	}
	_, err = transaction.Exec(ctx, `
		UPDATE alert_deliveries
		SET delivery_status = 'delivered',
		    delivered_at = COALESCE(delivered_at, $2),
		    locally_acknowledged_at = COALESCE(locally_acknowledged_at, $2),
		    next_attempt_at = NULL,
		    last_result_code = 'local_delivered',
		    updated_at = CURRENT_TIMESTAMP
		WHERE alert_delivery_id = $1
	`, deliveryID, deliveredAt)
	if err != nil {
		return err
	}
	return transaction.Commit(ctx)
}

func (store *PostgresStore) ClaimDue(
	ctx context.Context,
	workerID metadata.UUID,
	channel Channel,
	at time.Time,
	limit int,
) (deliveries []Delivery, err error) {
	if ctx == nil ||
		metadataUUID(workerID) == "" ||
		(channel != ChannelTelegram && channel != ChannelMorningDigest) ||
		at.IsZero() ||
		limit <= 0 ||
		limit > maxClaimBatch {
		return nil, ErrInvalidDelivery
	}
	at = at.UTC()
	transaction, err := store.database.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer rollback(ctx, transaction, &err)
	rows, err := transaction.Query(ctx, `
		SELECT
			d.alert_delivery_id::text,
			d.incident_id::text,
			d.incident_generation,
			d.channel,
			d.snapshot_status,
			d.snapshot_severity,
			d.snapshot_category,
			d.snapshot_component,
			d.actionable,
			d.snapshot_transitioned_at,
			d.attempt_count
		FROM alert_deliveries d
		WHERE d.channel = $1
		  AND d.delivery_status IN ('pending', 'failed')
		  AND d.next_attempt_at <= $2
		  AND (d.claim_until IS NULL OR d.claim_until <= $2)
		ORDER BY d.next_attempt_at, d.created_at, d.alert_delivery_id
		LIMIT $3
		FOR UPDATE OF d SKIP LOCKED
	`, string(channel), at, limit)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		delivery, scanErr := scanDelivery(rows)
		if scanErr != nil {
			rows.Close()
			return nil, scanErr
		}
		deliveries = append(deliveries, delivery)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	claimUntil := at.Add(store.policy.LeaseDuration)
	for index := range deliveries {
		attempt := deliveries[index].AttemptCount
		if attempt < 1000 {
			attempt++
		}
		tag, updateErr := transaction.Exec(ctx, `
			UPDATE alert_deliveries
			SET claim_owner = $2,
			    claim_until = $3,
			    attempt_count = $4,
			    updated_at = CURRENT_TIMESTAMP
			WHERE alert_delivery_id = $1
		`,
			string(deliveries[index].DeliveryID),
			string(workerID),
			claimUntil,
			attempt,
		)
		if updateErr != nil || tag.RowsAffected() != 1 {
			if updateErr != nil {
				return nil, updateErr
			}
			return nil, ErrDeliveryClaimLost
		}
		deliveries[index].AttemptCount = attempt
	}
	if err = transaction.Commit(ctx); err != nil {
		return nil, err
	}
	return deliveries, nil
}

func (store *PostgresStore) Complete(
	ctx context.Context,
	workerID metadata.UUID,
	deliveryIDs []metadata.UUID,
	completion Completion,
	at time.Time,
) (err error) {
	if ctx == nil ||
		metadataUUID(workerID) == "" ||
		len(deliveryIDs) == 0 ||
		len(deliveryIDs) > maxClaimBatch ||
		(completion != CompletionDelivered && completion != CompletionUnavailable) ||
		at.IsZero() {
		return ErrInvalidDelivery
	}
	seen := make(map[metadata.UUID]struct{}, len(deliveryIDs))
	for _, deliveryID := range deliveryIDs {
		if metadataUUID(deliveryID) == "" {
			return ErrInvalidDelivery
		}
		if _, duplicate := seen[deliveryID]; duplicate {
			return ErrInvalidDelivery
		}
		seen[deliveryID] = struct{}{}
	}
	at = at.UTC()
	transaction, err := store.database.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer rollback(ctx, transaction, &err)
	for _, deliveryID := range deliveryIDs {
		var (
			channel      Channel
			attemptCount uint32
			claimOwner   *string
		)
		err = transaction.QueryRow(ctx, `
			SELECT channel, attempt_count, claim_owner::text
			FROM alert_deliveries
			WHERE alert_delivery_id = $1
			FOR UPDATE
		`, string(deliveryID)).Scan(&channel, &attemptCount, &claimOwner)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrDeliveryNotFound
		}
		if err != nil {
			return err
		}
		if claimOwner == nil || *claimOwner != string(workerID) {
			return ErrDeliveryClaimLost
		}
		switch completion {
		case CompletionDelivered:
			_, err = transaction.Exec(ctx, `
				UPDATE alert_deliveries
				SET delivery_status = 'delivered',
				    delivered_at = $2,
				    next_attempt_at = NULL,
				    claim_owner = NULL,
				    claim_until = NULL,
				    last_result_code = $3,
				    updated_at = CURRENT_TIMESTAMP
				WHERE alert_delivery_id = $1
			`, string(deliveryID), at, string(channel)+"_ok")
		case CompletionUnavailable:
			_, err = transaction.Exec(ctx, `
				UPDATE alert_deliveries
				SET delivery_status = 'failed',
				    next_attempt_at = $2,
				    claim_owner = NULL,
				    claim_until = NULL,
				    last_result_code = $3,
				    updated_at = CURRENT_TIMESTAMP
				WHERE alert_delivery_id = $1
			`,
				string(deliveryID),
				at.Add(store.policy.retryDelay(attemptCount)),
				string(channel)+"_unavailable",
			)
		}
		if err != nil {
			return err
		}
	}
	return transaction.Commit(ctx)
}

func loadGeneration(
	ctx context.Context,
	transaction pgx.Tx,
	incidentID metadata.UUID,
	generation uint64,
) (Snapshot, error) {
	var (
		incidentIDString string
		nodeIDString     *string
		storedGeneration int64
		snapshot         Snapshot
	)
	err := transaction.QueryRow(ctx, `
		SELECT
			i.incident_id::text,
			i.node_id::text,
			i.generation,
			t.to_status,
			i.severity,
			i.category,
			i.component,
			i.requires_action,
			t.transitioned_at
		FROM incidents i
		JOIN incident_transitions t
		  ON t.incident_id = i.incident_id
		 AND t.generation = i.generation
		WHERE i.incident_id = $1
		  AND i.generation = $2
		FOR SHARE OF i
	`, string(incidentID), generation).Scan(
		&incidentIDString,
		&nodeIDString,
		&storedGeneration,
		&snapshot.Status,
		&snapshot.Severity,
		&snapshot.Category,
		&snapshot.Component,
		&snapshot.RequiresAction,
		&snapshot.TransitionedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Snapshot{}, ErrDeliveryNotFound
	}
	if err != nil {
		return Snapshot{}, err
	}
	snapshot.IncidentID, err = metadata.ParseUUID(incidentIDString)
	if err != nil || storedGeneration <= 0 {
		return Snapshot{}, ErrInvalidDelivery
	}
	snapshot.Generation = uint64(storedGeneration)
	if nodeIDString != nil {
		snapshot.NodeID, err = metadata.ParseUUID(*nodeIDString)
		if err != nil {
			return Snapshot{}, ErrInvalidDelivery
		}
	}
	snapshot.TransitionedAt = snapshot.TransitionedAt.UTC()
	if err := validateSnapshot(snapshot); err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}

type deliveryScanner interface {
	Scan(...any) error
}

func scanDelivery(scanner deliveryScanner) (Delivery, error) {
	var (
		deliveryIDString string
		incidentIDString string
		generation       int64
		attemptCount     int64
		delivery         Delivery
	)
	err := scanner.Scan(
		&deliveryIDString,
		&incidentIDString,
		&generation,
		&delivery.Channel,
		&delivery.Snapshot.Status,
		&delivery.Snapshot.Severity,
		&delivery.Snapshot.Category,
		&delivery.Snapshot.Component,
		&delivery.Actionable,
		&delivery.Snapshot.TransitionedAt,
		&attemptCount,
	)
	if err != nil {
		return Delivery{}, err
	}
	delivery.DeliveryID, err = metadata.ParseUUID(deliveryIDString)
	if err != nil {
		return Delivery{}, ErrInvalidDelivery
	}
	delivery.Snapshot.IncidentID, err = metadata.ParseUUID(incidentIDString)
	if err != nil || generation <= 0 || attemptCount < 0 || attemptCount > 1000 {
		return Delivery{}, ErrInvalidDelivery
	}
	delivery.Snapshot.Generation = uint64(generation)
	delivery.AttemptCount = uint32(attemptCount)
	delivery.Snapshot.RequiresAction = delivery.Actionable
	if delivery.Channel != ChannelTelegram &&
		delivery.Channel != ChannelMorningDigest {
		return Delivery{}, ErrInvalidDelivery
	}
	delivery.Snapshot.TransitionedAt = delivery.Snapshot.TransitionedAt.UTC()
	if validateSnapshot(delivery.Snapshot) != nil {
		return Delivery{}, ErrInvalidDelivery
	}
	return delivery, nil
}

func validateSnapshot(snapshot Snapshot) error {
	if metadataUUID(snapshot.IncidentID) == "" ||
		snapshot.Generation == 0 ||
		snapshot.TransitionedAt.IsZero() {
		return ErrInvalidDelivery
	}
	if snapshot.NodeID != "" && metadataUUID(snapshot.NodeID) == "" {
		return ErrInvalidDelivery
	}
	switch snapshot.Status {
	case cloudincident.StatusOpen,
		cloudincident.StatusAcknowledged,
		cloudincident.StatusResolved:
	default:
		return ErrInvalidDelivery
	}
	switch snapshot.Severity {
	case event.SeverityInfo, event.SeverityWarning, event.SeverityCritical:
	default:
		return ErrInvalidDelivery
	}
	switch snapshot.Category {
	case event.IncidentAvailability,
		event.IncidentRecoveryBudget,
		event.IncidentSpoolOverflow,
		event.IncidentSecurityValidation,
		event.IncidentDeployment:
	default:
		return ErrInvalidDelivery
	}
	switch snapshot.Component {
	case control.ComponentNetwork,
		control.ComponentTunnel,
		control.ComponentRoutes,
		control.ComponentPritunl,
		control.ComponentCodex,
		control.ComponentTelegram,
		control.ComponentRuntime:
	default:
		return ErrInvalidDelivery
	}
	return nil
}

func rollback(ctx context.Context, transaction pgx.Tx, resultErr *error) {
	rollbackErr := transaction.Rollback(ctx)
	if *resultErr == nil &&
		rollbackErr != nil &&
		!errors.Is(rollbackErr, pgx.ErrTxClosed) {
		*resultErr = rollbackErr
	}
}

func (store *PostgresStore) nextID() (metadata.UUID, error) {
	store.randomMu.Lock()
	defer store.randomMu.Unlock()
	return metadata.NewUUID(store.random)
}

func metadataUUID(value metadata.UUID) metadata.UUID {
	parsed, err := metadata.ParseUUID(string(value))
	if err != nil {
		return ""
	}
	return parsed
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
