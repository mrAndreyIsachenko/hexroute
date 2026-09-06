package configpublish

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ErrLabelReused is a version label already naming different content. It is
// refused rather than resolved: a label is how an operator and a host refer to
// the same bytes, and two meanings for one label would make every later
// reference ambiguous.
var ErrLabelReused = errors.New("configuration version label already names different content")

// Database is the pool the ledger writes through.
type Database interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

// PostgresLedger writes the rows of the configuration ledger.
type PostgresLedger struct {
	database Database
}

func NewPostgresLedger(database Database) (*PostgresLedger, error) {
	if database == nil {
		return nil, fmt.Errorf("%w: no database", ErrPublish)
	}
	return &PostgresLedger{database: database}, nil
}

// RecordVersion writes one row, or accepts that the identical row is already
// there. A publish interrupted after the row and before the object is retried
// by repeating it, so repeating has to be free.
func (ledger *PostgresLedger) RecordVersion(
	ctx context.Context,
	record Record,
) (err error) {
	if ledger == nil || ledger.database == nil || ctx == nil {
		return fmt.Errorf("%w: no ledger", ErrPublish)
	}
	if err := record.Validate(); err != nil {
		return err
	}
	transaction, err := ledger.database.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() {
		rollbackErr := transaction.Rollback(ctx)
		if err == nil && rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			err = rollbackErr
		}
	}()

	var inserted string
	err = transaction.QueryRow(ctx, `
		INSERT INTO config_versions (
			config_version_id,
			target_kind,
			target_key,
			schema_version,
			version_label,
			content_sha256,
			signing_key_id,
			lifecycle_status,
			created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, 'staged', $8
		)
		ON CONFLICT (target_kind, target_key, version_label) DO NOTHING
		RETURNING config_version_id::text
	`,
		string(record.ConfigVersionID),
		record.TargetKind,
		record.TargetKey,
		record.SchemaVersion,
		record.VersionLabel,
		record.ContentSHA256[:],
		record.SigningKeyID,
		record.CreatedAt,
	).Scan(&inserted)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if err == nil {
		return transaction.Commit(ctx)
	}

	// The label already exists. It may be this same version being published
	// again, which is fine, or a different one wearing the same name, which
	// is not.
	var existingDigest []byte
	var existingKey string
	if err = transaction.QueryRow(ctx, `
		SELECT content_sha256, signing_key_id
		FROM config_versions
		WHERE target_kind = $1 AND target_key = $2 AND version_label = $3
	`, record.TargetKind, record.TargetKey, record.VersionLabel).
		Scan(&existingDigest, &existingKey); err != nil {
		return err
	}
	if len(existingDigest) != 32 || existingKey != record.SigningKeyID ||
		string(existingDigest) != string(record.ContentSHA256[:]) {
		return ErrLabelReused
	}
	return transaction.Commit(ctx)
}
