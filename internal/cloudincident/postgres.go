package cloudincident

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/event"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/silentnode"
)

const (
	reasonConditionDetected    = "condition_detected"
	reasonConditionUpdated     = "condition_updated"
	reasonOperatorAcknowledged = "operator_acknowledged"
)

type Database interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

type PostgresStore struct {
	database Database
	random   io.Reader
	randomMu sync.Mutex
}

type incidentRow struct {
	incidentID     metadata.UUID
	nodeID         metadata.UUID
	correlationKey string
	category       event.IncidentCategory
	component      control.Component
	severity       event.IncidentSeverity
	status         Status
	requiresAction bool
	generation     uint64
	openedAt       time.Time
	lastObservedAt time.Time
}

func NewPostgresStore(database Database, randomSource io.Reader) (*PostgresStore, error) {
	if database == nil {
		return nil, ErrInvalidSignal
	}
	if randomSource == nil {
		randomSource = rand.Reader
	}
	return &PostgresStore{database: database, random: randomSource}, nil
}

func (store *PostgresStore) Reconcile(
	ctx context.Context,
	signal Signal,
) (result Result, err error) {
	if ctx == nil {
		return Result{}, ErrInvalidSignal
	}
	signal.ObservedAt = signal.ObservedAt.UTC()
	if err := validateSignal(signal); err != nil {
		return Result{}, err
	}
	transaction, err := store.database.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Result{}, err
	}
	defer func() {
		rollbackErr := transaction.Rollback(ctx)
		if err == nil && rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			err = rollbackErr
		}
	}()

	if err = lockCorrelation(ctx, transaction, signal.CorrelationKey); err != nil {
		return Result{}, err
	}
	current, err := loadLatestIncident(ctx, transaction, signal.CorrelationKey)
	if err != nil {
		return Result{}, err
	}
	switch signal.State {
	case ConditionDetected:
		result, err = store.reconcileDetected(ctx, transaction, current, signal)
	case ConditionCleared:
		result, err = store.reconcileCleared(ctx, transaction, current, signal)
	default:
		err = ErrInvalidSignal
	}
	if err != nil {
		return Result{}, err
	}
	if err = transaction.Commit(ctx); err != nil {
		return Result{}, err
	}
	return result, nil
}

func (store *PostgresStore) ReconcileSilent(
	ctx context.Context,
	decision silentnode.Decision,
) (Result, error) {
	signal, err := SignalFromSilentDecision(decision)
	if err != nil {
		return Result{}, err
	}
	return store.Reconcile(ctx, signal)
}

func (store *PostgresStore) Acknowledge(
	ctx context.Context,
	correlationKey string,
	acknowledgedAt time.Time,
	evidence []Evidence,
) (result Result, err error) {
	if ctx == nil ||
		!validCorrelationKey(correlationKey) ||
		acknowledgedAt.IsZero() ||
		len(evidence) > maxEvidencePerSignal {
		return Result{}, ErrInvalidSignal
	}
	seen := make(map[metadata.UUID]struct{}, len(evidence))
	for _, item := range evidence {
		if _, parseErr := metadata.ParseUUID(string(item.EventID)); parseErr != nil ||
			item.Role != EvidenceSupporting {
			return Result{}, ErrInvalidSignal
		}
		if _, exists := seen[item.EventID]; exists {
			return Result{}, ErrInvalidSignal
		}
		seen[item.EventID] = struct{}{}
	}
	acknowledgedAt = acknowledgedAt.UTC()
	transaction, err := store.database.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Result{}, err
	}
	defer func() {
		rollbackErr := transaction.Rollback(ctx)
		if err == nil && rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			err = rollbackErr
		}
	}()
	if err = lockCorrelation(ctx, transaction, correlationKey); err != nil {
		return Result{}, err
	}
	current, err := loadLatestIncident(ctx, transaction, correlationKey)
	if err != nil {
		return Result{}, err
	}
	if current == nil || current.status == StatusResolved {
		return Result{}, ErrIncidentNotFound
	}
	result = resultFromRow(current, false)
	if acknowledgedAt.Before(current.lastObservedAt) {
		return Result{}, ErrInvalidSignal
	}

	evidenceChanged, err := linkEvidence(ctx, transaction, current.incidentID, evidence)
	if err != nil {
		return Result{}, err
	}
	if current.status == StatusAcknowledged {
		result = resultFromRow(current, evidenceChanged)
		if err = transaction.Commit(ctx); err != nil {
			return Result{}, err
		}
		return result, nil
	}

	transitionID, err := store.nextID()
	if err != nil {
		return Result{}, err
	}
	current.generation++
	_, err = transaction.Exec(ctx, `
		UPDATE incidents
		SET incident_status = 'acknowledged',
		    generation = $2,
		    acknowledged_at = $3,
		    updated_at = CURRENT_TIMESTAMP
		WHERE incident_id = $1
	`, string(current.incidentID), current.generation, acknowledgedAt)
	if err != nil {
		return Result{}, err
	}
	if err = insertTransition(
		ctx,
		transaction,
		transitionID,
		current.incidentID,
		current.generation,
		string(current.status),
		StatusAcknowledged,
		reasonOperatorAcknowledged,
		acknowledgedAt,
	); err != nil {
		return Result{}, err
	}
	current.status = StatusAcknowledged
	result = resultFromRow(current, true)
	if err = transaction.Commit(ctx); err != nil {
		return Result{}, err
	}
	return result, nil
}

func (store *PostgresStore) reconcileDetected(
	ctx context.Context,
	transaction pgx.Tx,
	current *incidentRow,
	signal Signal,
) (Result, error) {
	if current != nil && !sameIdentity(current, signal) {
		return Result{}, ErrIncidentConflict
	}
	if current != nil && current.status == StatusResolved {
		if !signal.ObservedAt.After(current.lastObservedAt) {
			return resultFromRow(current, false), nil
		}
		current = nil
	}
	if current == nil {
		return store.openIncident(ctx, transaction, signal)
	}
	if signal.ObservedAt.Before(current.lastObservedAt) {
		return resultFromRow(current, false), nil
	}
	evidenceChanged, err := linkEvidence(
		ctx,
		transaction,
		current.incidentID,
		signal.Evidence,
	)
	if err != nil {
		return Result{}, err
	}
	observedChanged := signal.ObservedAt.After(current.lastObservedAt)
	materialChange := current.severity != signal.Severity ||
		current.requiresAction != signal.RequiresAction
	if !materialChange {
		if observedChanged {
			_, err = transaction.Exec(ctx, `
				UPDATE incidents
				SET last_observed_at = $2, updated_at = CURRENT_TIMESTAMP
				WHERE incident_id = $1
			`, string(current.incidentID), signal.ObservedAt)
			if err != nil {
				return Result{}, err
			}
			current.lastObservedAt = signal.ObservedAt
		}
		return resultFromRow(current, evidenceChanged || observedChanged), nil
	}

	fromStatus := current.status
	toStatus := fromStatus
	clearAcknowledgement := false
	if fromStatus == StatusAcknowledged &&
		isEscalation(
			current.severity,
			signal.Severity,
			current.requiresAction,
			signal.RequiresAction,
		) {
		toStatus = StatusOpen
		clearAcknowledgement = true
	}
	transitionID, err := store.nextID()
	if err != nil {
		return Result{}, err
	}
	current.generation++
	_, err = transaction.Exec(ctx, `
		UPDATE incidents
		SET severity = $2,
		    requires_action = $3,
		    incident_status = $4,
		    generation = $5,
		    last_observed_at = GREATEST(last_observed_at, $6),
		    acknowledged_at = CASE WHEN $7 THEN NULL ELSE acknowledged_at END,
		    updated_at = CURRENT_TIMESTAMP
		WHERE incident_id = $1
	`,
		string(current.incidentID),
		string(signal.Severity),
		signal.RequiresAction,
		string(toStatus),
		current.generation,
		signal.ObservedAt,
		clearAcknowledgement,
	)
	if err != nil {
		return Result{}, err
	}
	if err := insertTransition(
		ctx,
		transaction,
		transitionID,
		current.incidentID,
		current.generation,
		string(fromStatus),
		toStatus,
		reasonConditionUpdated,
		signal.ObservedAt,
	); err != nil {
		return Result{}, err
	}
	current.severity = signal.Severity
	current.requiresAction = signal.RequiresAction
	current.status = toStatus
	if signal.ObservedAt.After(current.lastObservedAt) {
		current.lastObservedAt = signal.ObservedAt
	}
	return resultFromRow(current, true), nil
}

func (store *PostgresStore) reconcileCleared(
	ctx context.Context,
	transaction pgx.Tx,
	current *incidentRow,
	signal Signal,
) (Result, error) {
	if current == nil {
		return Result{}, nil
	}
	if !sameIdentity(current, signal) {
		return Result{}, ErrIncidentConflict
	}
	if current.status == StatusResolved {
		return resultFromRow(current, false), nil
	}
	if signal.ObservedAt.Before(current.lastObservedAt) {
		return resultFromRow(current, false), nil
	}
	transitionID, err := store.nextID()
	if err != nil {
		return Result{}, err
	}
	if _, err := linkEvidence(
		ctx,
		transaction,
		current.incidentID,
		signal.Evidence,
	); err != nil {
		return Result{}, err
	}
	current.generation++
	_, err = transaction.Exec(ctx, `
		UPDATE incidents
		SET incident_status = 'resolved',
		    generation = $2,
		    last_observed_at = GREATEST(last_observed_at, $3),
		    resolved_at = $3,
		    updated_at = CURRENT_TIMESTAMP
		WHERE incident_id = $1
	`, string(current.incidentID), current.generation, signal.ObservedAt)
	if err != nil {
		return Result{}, err
	}
	if err := insertTransition(
		ctx,
		transaction,
		transitionID,
		current.incidentID,
		current.generation,
		string(current.status),
		StatusResolved,
		string(signal.ResolutionReason),
		signal.ObservedAt,
	); err != nil {
		return Result{}, err
	}
	current.status = StatusResolved
	if signal.ObservedAt.After(current.lastObservedAt) {
		current.lastObservedAt = signal.ObservedAt
	}
	return resultFromRow(current, true), nil
}

func (store *PostgresStore) openIncident(
	ctx context.Context,
	transaction pgx.Tx,
	signal Signal,
) (Result, error) {
	incidentID, err := store.nextID()
	if err != nil {
		return Result{}, err
	}
	transitionID, err := store.nextID()
	if err != nil {
		return Result{}, err
	}
	_, err = transaction.Exec(ctx, `
		INSERT INTO incidents (
			incident_id,
			node_id,
			correlation_key,
			category,
			component,
			severity,
			incident_status,
			requires_action,
			generation,
			opened_at,
			last_observed_at,
			updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, 'open', $7, 1, $8, $8, $8)
	`,
		string(incidentID),
		nullableUUID(signal.NodeID),
		signal.CorrelationKey,
		string(signal.Category),
		string(signal.Component),
		string(signal.Severity),
		signal.RequiresAction,
		signal.ObservedAt,
	)
	if err != nil {
		return Result{}, err
	}
	if err := insertTransition(
		ctx,
		transaction,
		transitionID,
		incidentID,
		1,
		"new",
		StatusOpen,
		reasonConditionDetected,
		signal.ObservedAt,
	); err != nil {
		return Result{}, err
	}
	if _, err := linkEvidence(
		ctx,
		transaction,
		incidentID,
		signal.Evidence,
	); err != nil {
		return Result{}, err
	}
	return Result{
		IncidentID: incidentID,
		Status:     StatusOpen,
		Generation: 1,
		Found:      true,
		Changed:    true,
	}, nil
}

func lockCorrelation(
	ctx context.Context,
	transaction pgx.Tx,
	correlationKey string,
) error {
	_, err := transaction.Exec(
		ctx,
		"SELECT pg_advisory_xact_lock(hashtextextended($1, 0))",
		correlationKey,
	)
	return err
}

func loadLatestIncident(
	ctx context.Context,
	transaction pgx.Tx,
	correlationKey string,
) (*incidentRow, error) {
	var (
		incidentIDString string
		nodeIDString     *string
		row              incidentRow
		generation       int64
	)
	err := transaction.QueryRow(ctx, `
		SELECT
			incident_id::text,
			node_id::text,
			correlation_key,
			category,
			component,
			severity,
			incident_status,
			requires_action,
			generation,
			opened_at,
			last_observed_at
		FROM incidents
		WHERE correlation_key = $1
		ORDER BY opened_at DESC, created_at DESC
		LIMIT 1
		FOR UPDATE
	`, correlationKey).Scan(
		&incidentIDString,
		&nodeIDString,
		&row.correlationKey,
		&row.category,
		&row.component,
		&row.severity,
		&row.status,
		&row.requiresAction,
		&generation,
		&row.openedAt,
		&row.lastObservedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	row.incidentID, err = metadata.ParseUUID(incidentIDString)
	if err != nil || generation <= 0 {
		return nil, ErrIncidentConflict
	}
	row.generation = uint64(generation)
	if nodeIDString != nil {
		row.nodeID, err = metadata.ParseUUID(*nodeIDString)
		if err != nil {
			return nil, ErrIncidentConflict
		}
	}
	if row.status != StatusOpen &&
		row.status != StatusAcknowledged &&
		row.status != StatusResolved {
		return nil, ErrIncidentConflict
	}
	row.openedAt = row.openedAt.UTC()
	row.lastObservedAt = row.lastObservedAt.UTC()
	return &row, nil
}

func linkEvidence(
	ctx context.Context,
	transaction pgx.Tx,
	incidentID metadata.UUID,
	evidence []Evidence,
) (bool, error) {
	changed := false
	for _, item := range evidence {
		tag, err := transaction.Exec(ctx, `
			INSERT INTO incident_events (incident_id, event_id, evidence_role)
			VALUES ($1, $2, $3)
			ON CONFLICT (incident_id, event_id) DO NOTHING
		`, string(incidentID), string(item.EventID), string(item.Role))
		if err != nil {
			return false, err
		}
		if tag.RowsAffected() > 0 {
			changed = true
			continue
		}
		var existing EvidenceRole
		err = transaction.QueryRow(ctx, `
			SELECT evidence_role
			FROM incident_events
			WHERE incident_id = $1 AND event_id = $2
		`, string(incidentID), string(item.EventID)).Scan(&existing)
		if err != nil {
			return false, err
		}
		if existing != item.Role {
			return false, ErrEvidenceConflict
		}
	}
	return changed, nil
}

func insertTransition(
	ctx context.Context,
	transaction pgx.Tx,
	transitionID metadata.UUID,
	incidentID metadata.UUID,
	generation uint64,
	fromStatus string,
	toStatus Status,
	reason string,
	at time.Time,
) error {
	_, err := transaction.Exec(ctx, `
		INSERT INTO incident_transitions (
			incident_transition_id,
			incident_id,
			generation,
			from_status,
			to_status,
			reason_code,
			transitioned_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
	`,
		string(transitionID),
		string(incidentID),
		generation,
		fromStatus,
		string(toStatus),
		reason,
		at,
	)
	return err
}

func (store *PostgresStore) nextID() (metadata.UUID, error) {
	store.randomMu.Lock()
	defer store.randomMu.Unlock()
	return metadata.NewUUID(store.random)
}

func sameIdentity(current *incidentRow, signal Signal) bool {
	return current.nodeID == signal.NodeID &&
		current.correlationKey == signal.CorrelationKey &&
		current.category == signal.Category &&
		current.component == signal.Component
}

func isEscalation(
	from event.IncidentSeverity,
	to event.IncidentSeverity,
	fromRequiresAction bool,
	toRequiresAction bool,
) bool {
	return severityRank(to) > severityRank(from) ||
		(!fromRequiresAction && toRequiresAction)
}

func severityRank(value event.IncidentSeverity) int {
	switch value {
	case event.SeverityInfo:
		return 1
	case event.SeverityWarning:
		return 2
	case event.SeverityCritical:
		return 3
	default:
		return 0
	}
}

func resultFromRow(row *incidentRow, changed bool) Result {
	if row == nil {
		return Result{}
	}
	return Result{
		IncidentID: row.incidentID,
		Status:     row.status,
		Generation: row.generation,
		Found:      true,
		Changed:    changed,
	}
}

func nullableUUID(value metadata.UUID) any {
	if value == "" {
		return nil
	}
	return string(value)
}
