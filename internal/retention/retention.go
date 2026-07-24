package retention

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	DetailRetention     = 30 * 24 * time.Hour
	TransitionRetention = 180 * 24 * time.Hour
	maxBatchSize        = 5000
)

type Database interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

type Worker struct {
	database  Database
	batchSize int
}

type Result struct {
	DetailEvents        int64
	TransitionEvents    int64
	SecurityAudit       int64
	SleepIntervals      int64
	ResolvedGaps        int64
	IncidentAlertOutbox int64
	IncidentTransitions int64
	TerminalAlerts      int64
	OrphanBatches       int64
}

var ErrInvalidRetention = errors.New("invalid retention configuration")

func NewWorker(database Database, batchSize int) (*Worker, error) {
	if database == nil || batchSize <= 0 || batchSize > maxBatchSize {
		return nil, ErrInvalidRetention
	}
	return &Worker{database: database, batchSize: batchSize}, nil
}

func (worker *Worker) RunOnce(
	ctx context.Context,
	at time.Time,
) (result Result, err error) {
	if worker == nil ||
		worker.database == nil ||
		ctx == nil ||
		at.IsZero() {
		return Result{}, ErrInvalidRetention
	}
	detailCutoff := at.UTC().Add(-DetailRetention)
	transitionCutoff := at.UTC().Add(-TransitionRetention)
	transaction, err := worker.database.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Result{}, err
	}
	defer func() {
		rollbackErr := transaction.Rollback(ctx)
		if err == nil &&
			rollbackErr != nil &&
			!errors.Is(rollbackErr, pgx.ErrTxClosed) {
			err = rollbackErr
		}
	}()
	if _, err = transaction.Exec(
		ctx,
		"SELECT pg_advisory_xact_lock(hashtextextended('hexroute-retention-v1', 0))",
	); err != nil {
		return result, err
	}

	result.DetailEvents, err = worker.delete(
		ctx,
		transaction,
		deleteDetailEvents,
		detailCutoff,
	)
	if err != nil {
		return result, err
	}
	result.TransitionEvents, err = worker.delete(
		ctx,
		transaction,
		deleteTransitionEvents,
		transitionCutoff,
	)
	if err != nil {
		return result, err
	}
	result.SecurityAudit, err = worker.delete(
		ctx,
		transaction,
		deleteSecurityAudit,
		detailCutoff,
	)
	if err != nil {
		return result, err
	}
	result.SleepIntervals, err = worker.delete(
		ctx,
		transaction,
		deleteSleepIntervals,
		detailCutoff,
	)
	if err != nil {
		return result, err
	}
	result.ResolvedGaps, err = worker.delete(
		ctx,
		transaction,
		deleteResolvedGaps,
		detailCutoff,
	)
	if err != nil {
		return result, err
	}
	result.IncidentAlertOutbox, err = worker.delete(
		ctx,
		transaction,
		deleteIncidentAlertOutbox,
		transitionCutoff,
	)
	if err != nil {
		return result, err
	}
	result.IncidentTransitions, err = worker.delete(
		ctx,
		transaction,
		deleteIncidentTransitions,
		transitionCutoff,
	)
	if err != nil {
		return result, err
	}
	result.TerminalAlerts, err = worker.delete(
		ctx,
		transaction,
		deleteTerminalAlerts,
		transitionCutoff,
	)
	if err != nil {
		return result, err
	}
	result.OrphanBatches, err = worker.delete(
		ctx,
		transaction,
		deleteOrphanBatches,
		detailCutoff,
	)
	if err != nil {
		return result, err
	}
	if err = transaction.Commit(ctx); err != nil {
		return result, err
	}
	return result, nil
}

func (result Result) Total() int64 {
	return result.DetailEvents +
		result.TransitionEvents +
		result.SecurityAudit +
		result.SleepIntervals +
		result.ResolvedGaps +
		result.IncidentAlertOutbox +
		result.IncidentTransitions +
		result.TerminalAlerts +
		result.OrphanBatches
}

func (worker *Worker) delete(
	ctx context.Context,
	transaction pgx.Tx,
	statement string,
	cutoff time.Time,
) (int64, error) {
	tag, err := transaction.Exec(ctx, statement, cutoff, worker.batchSize)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

const deleteDetailEvents = `
	WITH candidates AS (
		SELECT event_id
		FROM events
		WHERE received_at < $1
		  AND schema_name IN (
			  'component.observation',
			  'runtime.diagnostic',
			  'node.sleep'
		  )
		ORDER BY received_at, event_id
		LIMIT $2
	)
	DELETE FROM events e
	USING candidates c
	WHERE e.event_id = c.event_id
`

const deleteTransitionEvents = `
	WITH candidates AS (
		SELECT event_id
		FROM events
		WHERE received_at < $1
		  AND schema_name IN (
			  'state.transition',
			  'recovery.action',
			  'incident.lifecycle',
			  'deployment.lifecycle',
			  'config.lifecycle'
		  )
		ORDER BY received_at, event_id
		LIMIT $2
	)
	DELETE FROM events e
	USING candidates c
	WHERE e.event_id = c.event_id
`

const deleteSecurityAudit = `
	WITH candidates AS (
		SELECT audit_record_id
		FROM security_audit_records
		WHERE occurred_at < $1
		ORDER BY occurred_at, audit_record_id
		LIMIT $2
	)
	DELETE FROM security_audit_records a
	USING candidates c
	WHERE a.audit_record_id = c.audit_record_id
`

const deleteSleepIntervals = `
	WITH candidates AS (
		SELECT sleep_interval_id
		FROM sleep_intervals
		WHERE ended_at < $1
		ORDER BY ended_at, sleep_interval_id
		LIMIT $2
	)
	DELETE FROM sleep_intervals s
	USING candidates c
	WHERE s.sleep_interval_id = c.sleep_interval_id
`

const deleteResolvedGaps = `
	WITH candidates AS (
		SELECT sequence_gap_id
		FROM sequence_gaps
		WHERE resolved_at < $1
		ORDER BY resolved_at, sequence_gap_id
		LIMIT $2
	)
	DELETE FROM sequence_gaps g
	USING candidates c
	WHERE g.sequence_gap_id = c.sequence_gap_id
`

const deleteIncidentTransitions = `
	WITH candidates AS (
		SELECT incident_transition_id
		FROM incident_transitions
		WHERE transitioned_at < $1
		ORDER BY transitioned_at, incident_transition_id
		LIMIT $2
	)
	DELETE FROM incident_transitions t
	USING candidates c
	WHERE t.incident_transition_id = c.incident_transition_id
`

const deleteIncidentAlertOutbox = `
	WITH candidates AS (
		SELECT incident_id, incident_generation
		FROM incident_alert_outbox
		WHERE processed_at < $1
		ORDER BY processed_at, incident_id, incident_generation
		LIMIT $2
	)
	DELETE FROM incident_alert_outbox outbox
	USING candidates candidate
	WHERE outbox.incident_id = candidate.incident_id
	  AND outbox.incident_generation = candidate.incident_generation
`

const deleteTerminalAlerts = `
	WITH candidates AS (
		SELECT alert_delivery_id
		FROM alert_deliveries
		WHERE delivery_status IN ('delivered', 'suppressed')
		  AND updated_at < $1
		ORDER BY updated_at, alert_delivery_id
		LIMIT $2
	)
	DELETE FROM alert_deliveries d
	USING candidates c
	WHERE d.alert_delivery_id = c.alert_delivery_id
`

const deleteOrphanBatches = `
	WITH candidates AS (
		SELECT b.batch_id
		FROM batches b
		WHERE b.received_at < $1
		  AND NOT EXISTS (
			  SELECT 1 FROM events e WHERE e.batch_id = b.batch_id
		  )
		  AND NOT EXISTS (
			  SELECT 1 FROM sequence_gaps g WHERE g.detected_batch_id = b.batch_id
		  )
		ORDER BY b.received_at, b.batch_id
		LIMIT $2
	)
	DELETE FROM batches b
	USING candidates c
	WHERE b.batch_id = c.batch_id
`
