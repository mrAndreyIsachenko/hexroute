package cloudingest

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/signing"
)

type Database interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type PostgresStore struct {
	database Database
	random   io.Reader
}

type sequenceGap struct {
	id              metadata.UUID
	first           uint64
	last            uint64
	detectedBatchID metadata.UUID
	detectedAt      time.Time
}

func NewPostgresStore(database Database, random io.Reader) (*PostgresStore, error) {
	if database == nil {
		return nil, ErrInvalidRequest
	}
	if random == nil {
		random = rand.Reader
	}
	return &PostgresStore{database: database, random: random}, nil
}

func (store *PostgresStore) LookupKey(
	ctx context.Context,
	nodeID metadata.UUID,
	keyID metadata.UUID,
) (KeyRecord, error) {
	var (
		storedNodeID string
		storedKeyID  string
		publicKey    []byte
		status       string
		validFrom    time.Time
		validUntil   *time.Time
	)
	err := store.database.QueryRow(ctx, `
		SELECT node_id::text, key_id, public_key, key_status, valid_from, valid_until
		FROM node_public_keys
		WHERE node_id = $1 AND key_id = $2
	`, string(nodeID), string(keyID)).Scan(
		&storedNodeID,
		&storedKeyID,
		&publicKey,
		&status,
		&validFrom,
		&validUntil,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return KeyRecord{}, ErrKeyNotFound
	}
	if err != nil {
		return KeyRecord{}, err
	}
	parsedNodeID, nodeErr := metadata.ParseUUID(storedNodeID)
	parsedKeyID, keyErr := metadata.ParseUUID(storedKeyID)
	if nodeErr != nil || keyErr != nil || len(publicKey) != ed25519.PublicKeySize {
		return KeyRecord{}, ErrInvalidRequest
	}
	keyStatus := signing.KeyStatus(status)
	switch keyStatus {
	case signing.KeyActive, signing.KeyRetired, signing.KeyRevoked:
	default:
		return KeyRecord{}, ErrInvalidRequest
	}
	record := KeyRecord{
		NodeID:    parsedNodeID,
		KeyID:     parsedKeyID,
		PublicKey: append(ed25519.PublicKey(nil), publicKey...),
		Status:    keyStatus,
		ValidFrom: validFrom.UTC(),
	}
	if validUntil != nil {
		record.ValidUntil = validUntil.UTC()
	}
	return record, nil
}

func (store *PostgresStore) AcceptBatch(
	ctx context.Context,
	request BatchRequest,
) (err error) {
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

	if err = insertBatch(ctx, transaction, request); err != nil {
		return fmt.Errorf("insert batch: %w", err)
	}
	if err = lockSessions(ctx, transaction, request); err != nil {
		return fmt.Errorf("lock sessions: %w", err)
	}
	for _, item := range request.Events {
		inserted, insertErr := insertEvent(ctx, transaction, request, item)
		if insertErr != nil {
			return fmt.Errorf("insert event: %w", insertErr)
		}
		if inserted {
			if err = store.advanceSequence(ctx, transaction, request, item); err != nil {
				return fmt.Errorf("advance sequence: %w", err)
			}
		}
	}
	if _, err = transaction.Exec(ctx, `
		UPDATE nodes
		SET first_seen_at = COALESCE(first_seen_at, $2),
		    last_seen_at = CASE
		        WHEN last_seen_at IS NULL OR last_seen_at < $2 THEN $2
		        ELSE last_seen_at
		    END,
		    updated_at = $2
		WHERE node_id = $1
	`, string(request.NodeID), request.ReceivedAt); err != nil {
		return fmt.Errorf("update node receipt: %w", err)
	}
	if err = transaction.Commit(ctx); err != nil {
		return err
	}
	return nil
}

func (store *PostgresStore) RecordAudit(
	ctx context.Context,
	record AuditRecord,
) (err error) {
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
	_, err = transaction.Exec(ctx, `
		INSERT INTO security_audit_records (
			audit_record_id,
			node_id,
			request_id,
			category,
			reason_code,
			occurred_at
		) VALUES (
			$1,
			(SELECT node_id FROM nodes WHERE node_id = $2),
			$3,
			$4,
			$5,
			$6
		)
	`,
		string(record.AuditRecordID),
		nullableUUID(record.NodeID),
		nullableUUID(record.RequestID),
		string(record.Category),
		record.ReasonCode,
		record.OccurredAt,
	)
	if err != nil {
		return err
	}
	return transaction.Commit(ctx)
}

func insertBatch(
	ctx context.Context,
	transaction pgx.Tx,
	request BatchRequest,
) error {
	var batchID string
	err := transaction.QueryRow(ctx, `
		INSERT INTO batches (
			batch_id,
			request_id,
			node_id,
			signing_key_id,
			protocol_version,
			first_sequence,
			last_sequence,
			event_count,
			compressed_bytes,
			content_sha256,
			signed_at,
			received_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
		)
		ON CONFLICT (request_id) DO NOTHING
		RETURNING batch_id::text
	`,
		string(request.BatchID),
		string(request.RequestID),
		string(request.NodeID),
		string(request.SigningKeyID),
		request.ProtocolVersion,
		int64(request.FirstSequence),
		int64(request.LastSequence),
		len(request.Events),
		request.CompressedBytes,
		request.ContentSHA256[:],
		request.SignedAt,
		request.ReceivedAt,
	).Scan(&batchID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrReplay
	}
	if err != nil {
		return classifyPostgresConflict(err)
	}
	return nil
}

func lockSessions(
	ctx context.Context,
	transaction pgx.Tx,
	request BatchRequest,
) error {
	sessions := make(map[metadata.UUID]struct{})
	for _, item := range request.Events {
		sessions[item.Metadata.SessionID] = struct{}{}
	}
	ordered := make([]string, 0, len(sessions))
	for sessionID := range sessions {
		ordered = append(ordered, string(sessionID))
	}
	sort.Strings(ordered)
	for _, sessionID := range ordered {
		lockKey := string(request.NodeID) + "/" + sessionID
		if _, err := transaction.Exec(
			ctx,
			"SELECT pg_advisory_xact_lock(hashtextextended($1, 0))",
			lockKey,
		); err != nil {
			return err
		}
	}
	return nil
}

func insertEvent(
	ctx context.Context,
	transaction pgx.Tx,
	request BatchRequest,
	item StoredEvent,
) (bool, error) {
	var eventID string
	err := transaction.QueryRow(ctx, `
		INSERT INTO events (
			event_id,
			batch_id,
			node_id,
			boot_session_id,
			sequence,
			occurred_at,
			monotonic_offset_ns,
			schema_name,
			schema_version,
			priority,
			payload,
			received_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::jsonb, $12
		)
		ON CONFLICT (event_id) DO NOTHING
		RETURNING event_id::text
	`,
		string(item.Metadata.EventID),
		string(request.BatchID),
		string(item.Metadata.NodeID),
		string(item.Metadata.SessionID),
		int64(item.Metadata.Sequence),
		item.Metadata.WallClock,
		item.Metadata.MonotonicNanos,
		string(item.Schema),
		item.Version,
		string(item.Priority),
		string(item.Payload),
		request.ReceivedAt,
	).Scan(&eventID)
	if err == nil {
		return true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return false, classifyPostgresConflict(err)
	}

	var same bool
	err = transaction.QueryRow(ctx, `
		SELECT
			node_id = $2
			AND boot_session_id = $3
			AND sequence = $4
			AND occurred_at = $5
			AND monotonic_offset_ns = $6
			AND schema_name = $7
			AND schema_version = $8
			AND priority = $9
			AND payload = $10::jsonb
		FROM events
		WHERE event_id = $1
	`,
		string(item.Metadata.EventID),
		string(item.Metadata.NodeID),
		string(item.Metadata.SessionID),
		int64(item.Metadata.Sequence),
		item.Metadata.WallClock,
		item.Metadata.MonotonicNanos,
		string(item.Schema),
		item.Version,
		string(item.Priority),
		string(item.Payload),
	).Scan(&same)
	if errors.Is(err, pgx.ErrNoRows) || !same {
		return false, ErrEventConflict
	}
	if err != nil {
		return false, err
	}
	return false, nil
}

func (store *PostgresStore) advanceSequence(
	ctx context.Context,
	transaction pgx.Tx,
	request BatchRequest,
	item StoredEvent,
) error {
	nodeID := string(item.Metadata.NodeID)
	sessionID := string(item.Metadata.SessionID)
	sequence := int64(item.Metadata.Sequence)
	_, err := transaction.Exec(ctx, `
		INSERT INTO node_sequence_cursors (
			node_id,
			boot_session_id,
			highest_sequence,
			next_expected_sequence,
			updated_at
		) VALUES ($1, $2, 0, 1, $3)
		ON CONFLICT (node_id, boot_session_id) DO NOTHING
	`, nodeID, sessionID, request.ReceivedAt)
	if err != nil {
		return err
	}

	var highest int64
	err = transaction.QueryRow(ctx, `
		SELECT highest_sequence
		FROM node_sequence_cursors
		WHERE node_id = $1 AND boot_session_id = $2
		FOR UPDATE
	`, nodeID, sessionID).Scan(&highest)
	if err != nil {
		return err
	}
	if sequence > highest+1 {
		gapID, idErr := metadata.NewUUID(store.random)
		if idErr != nil {
			return idErr
		}
		_, err = transaction.Exec(ctx, `
			INSERT INTO sequence_gaps (
				sequence_gap_id,
				node_id,
				boot_session_id,
				first_sequence,
				last_sequence,
				detected_batch_id,
				detected_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (
				node_id,
				boot_session_id,
				first_sequence,
				last_sequence
			) DO NOTHING
		`,
			string(gapID),
			nodeID,
			sessionID,
			highest+1,
			sequence-1,
			string(request.BatchID),
			request.ReceivedAt,
		)
		if err != nil {
			return err
		}
	}
	if sequence > highest {
		_, err = transaction.Exec(ctx, `
			UPDATE node_sequence_cursors
			SET highest_sequence = $3::bigint,
			    next_expected_sequence = $3::bigint + 1,
			    updated_at = $4
			WHERE node_id = $1 AND boot_session_id = $2
		`, nodeID, sessionID, sequence, request.ReceivedAt)
		if err != nil {
			return err
		}
	}
	return store.fillSequenceGap(ctx, transaction, request, item)
}

func (store *PostgresStore) fillSequenceGap(
	ctx context.Context,
	transaction pgx.Tx,
	request BatchRequest,
	item StoredEvent,
) error {
	rows, err := transaction.Query(ctx, `
		SELECT
			sequence_gap_id::text,
			first_sequence,
			last_sequence,
			detected_batch_id::text,
			detected_at
		FROM sequence_gaps
		WHERE node_id = $1
		  AND boot_session_id = $2
		  AND resolved_at IS NULL
		  AND first_sequence <= $3
		  AND last_sequence >= $3
		FOR UPDATE
	`,
		string(item.Metadata.NodeID),
		string(item.Metadata.SessionID),
		int64(item.Metadata.Sequence),
	)
	if err != nil {
		return err
	}
	gaps := make([]sequenceGap, 0, 1)
	for rows.Next() {
		var (
			gapID         string
			first         int64
			last          int64
			detectedBatch string
			detectedAt    time.Time
		)
		if err := rows.Scan(
			&gapID,
			&first,
			&last,
			&detectedBatch,
			&detectedAt,
		); err != nil {
			rows.Close()
			return err
		}
		parsedGapID, gapErr := metadata.ParseUUID(gapID)
		parsedBatchID, batchErr := metadata.ParseUUID(detectedBatch)
		if gapErr != nil || batchErr != nil {
			rows.Close()
			return ErrInvalidRequest
		}
		gaps = append(gaps, sequenceGap{
			id:              parsedGapID,
			first:           uint64(first),
			last:            uint64(last),
			detectedBatchID: parsedBatchID,
			detectedAt:      detectedAt,
		})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	for _, gap := range gaps {
		sequence := item.Metadata.Sequence
		switch {
		case gap.first == gap.last:
			_, err = transaction.Exec(ctx, `
				UPDATE sequence_gaps
				SET resolved_at = $2
				WHERE sequence_gap_id = $1
			`, string(gap.id), request.ReceivedAt)
		case sequence == gap.first:
			_, err = transaction.Exec(ctx, `
				UPDATE sequence_gaps
				SET first_sequence = first_sequence + 1
				WHERE sequence_gap_id = $1
			`, string(gap.id))
		case sequence == gap.last:
			_, err = transaction.Exec(ctx, `
				UPDATE sequence_gaps
				SET last_sequence = last_sequence - 1
				WHERE sequence_gap_id = $1
			`, string(gap.id))
		default:
			_, err = transaction.Exec(ctx, `
				UPDATE sequence_gaps
				SET last_sequence = $2
				WHERE sequence_gap_id = $1
			`, string(gap.id), sequence-1)
			if err == nil {
				var splitID metadata.UUID
				splitID, err = metadata.NewUUID(store.random)
				if err == nil {
					_, err = transaction.Exec(ctx, `
						INSERT INTO sequence_gaps (
							sequence_gap_id,
							node_id,
							boot_session_id,
							first_sequence,
							last_sequence,
							detected_batch_id,
							detected_at
						) VALUES ($1, $2, $3, $4, $5, $6, $7)
					`,
						string(splitID),
						string(item.Metadata.NodeID),
						string(item.Metadata.SessionID),
						sequence+1,
						gap.last,
						string(gap.detectedBatchID),
						gap.detectedAt,
					)
				}
			}
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func classifyPostgresConflict(err error) error {
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError.Code != "23505" {
		return err
	}
	switch postgresError.ConstraintName {
	case "batches_request_id_key":
		return ErrReplay
	case "batches_pkey":
		return ErrBatchConflict
	case "events_pkey":
		return ErrEventConflict
	case "events_node_id_boot_session_id_sequence_key":
		return ErrSequenceConflict
	default:
		return err
	}
}

func nullableUUID(value metadata.UUID) any {
	if value == "" {
		return nil
	}
	return string(value)
}
