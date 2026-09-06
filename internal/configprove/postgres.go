package configprove

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

// Database is the pool the ledger reads and writes through.
type Database interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

// PostgresLedger records lifecycle and deployments.
//
// It has no INSERT on config_versions and does not want one: nothing that runs
// unattended may bring a version into existence, and this runs unattended.
type PostgresLedger struct {
	database Database
	// applicationVersion is what the deployment row records as the software
	// that carried this configuration. It is the build, not the version.
	applicationVersion string
}

func NewPostgresLedger(database Database, applicationVersion string) (*PostgresLedger, error) {
	if database == nil || applicationVersion == "" || len(applicationVersion) > 128 {
		return nil, fmt.Errorf("%w: no ledger", ErrProve)
	}
	return &PostgresLedger{database: database, applicationVersion: applicationVersion}, nil
}

// LatestVersion is the most recently published version for a target that has
// not been retired.
func (ledger *PostgresLedger) LatestVersion(
	ctx context.Context,
	kind string,
	key string,
) (Version, error) {
	if ledger == nil || ledger.database == nil || ctx == nil || kind == "" || key == "" {
		return Version{}, fmt.Errorf("%w: no ledger", ErrProve)
	}
	transaction, err := ledger.database.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Version{}, err
	}
	defer func() { _ = transaction.Rollback(ctx) }()

	var (
		version     Version
		versionID   string
		lifecycle   string
		digest      []byte
		activatedAt *time.Time
	)
	err = transaction.QueryRow(ctx, `
		SELECT config_version_id::text, target_kind, target_key, version_label,
		       content_sha256, lifecycle_status, created_at, activated_at
		FROM config_versions
		WHERE target_kind = $1 AND target_key = $2
		ORDER BY created_at DESC, version_label DESC
		LIMIT 1
	`, kind, key).Scan(
		&versionID, &version.TargetKind, &version.TargetKey, &version.VersionLabel,
		&digest, &lifecycle, &version.CreatedAt, &activatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Version{}, ErrNoVersion
	}
	if err != nil {
		return Version{}, err
	}
	if len(digest) != 32 {
		return Version{}, fmt.Errorf("%w: content digest", ErrProve)
	}
	version.ConfigVersionID = metadata.UUID(versionID)
	version.Lifecycle = Lifecycle(lifecycle)
	copy(version.ContentSHA256[:], digest)
	version.CreatedAt = version.CreatedAt.UTC()
	if activatedAt != nil {
		version.ActivatedAt = activatedAt.UTC()
	}
	return version, nil
}

// MarkActive records that a host was seen running this version, and opens the
// deployment. The insert is conditional so a prover that ran twice records one
// deployment rather than two.
func (ledger *PostgresLedger) MarkActive(
	ctx context.Context,
	version Version,
	deploymentID metadata.UUID,
	at time.Time,
) error {
	if _, err := metadata.ParseUUID(string(deploymentID)); err != nil {
		return fmt.Errorf("%w: deployment id", ErrProve)
	}
	return ledger.transact(ctx, func(transaction pgx.Tx) error {
		if _, err := transaction.Exec(ctx, `
			UPDATE config_versions
			SET lifecycle_status = 'active',
			    activated_at = COALESCE(activated_at, $2),
			    unproven_reason = NULL
			WHERE config_version_id = $1 AND lifecycle_status = 'staged'
		`, string(version.ConfigVersionID), at); err != nil {
			return err
		}
		_, err := transaction.Exec(ctx, `
			INSERT INTO deployments (
				deployment_id, target_key, application_version, artifact_sha256,
				config_version_id, deployment_status, started_at
			) VALUES ($1, $2, $3, $4, $5, 'started', $6)
			ON CONFLICT (config_version_id, target_key) DO NOTHING
		`,
			string(deploymentID), version.TargetKey, ledger.applicationVersion,
			version.ContentSHA256[:], string(version.ConfigVersionID), at,
		)
		return err
	})
}

// MarkProven records that the version reported healthy for its whole window.
func (ledger *PostgresLedger) MarkProven(
	ctx context.Context,
	version Version,
	at time.Time,
) error {
	return ledger.transact(ctx, func(transaction pgx.Tx) error {
		if _, err := transaction.Exec(ctx, `
			UPDATE config_versions
			SET lifecycle_status = 'proven',
			    proven_at = $2,
			    unproven_reason = NULL
			WHERE config_version_id = $1 AND lifecycle_status = 'active'
		`, string(version.ConfigVersionID), at); err != nil {
			return err
		}
		_, err := transaction.Exec(ctx, `
			UPDATE deployments
			SET deployment_status = 'healthy', completed_at = $2
			WHERE config_version_id = $1 AND target_key = $3
		`, string(version.ConfigVersionID), at, version.TargetKey)
		return err
	})
}

// MarkRejected records that the version never proved, and why.
//
// The deployment is rolled back rather than failed: the host has already
// returned to what it was running, and 'failed' would say nothing is serving.
func (ledger *PostgresLedger) MarkRejected(
	ctx context.Context,
	version Version,
	at time.Time,
	reason string,
) error {
	if reason == "" || len(reason) > 64 {
		return fmt.Errorf("%w: reason", ErrProve)
	}
	return ledger.transact(ctx, func(transaction pgx.Tx) error {
		if _, err := transaction.Exec(ctx, `
			UPDATE config_versions
			SET lifecycle_status = 'rejected',
			    retired_at = $2,
			    unproven_reason = $3
			WHERE config_version_id = $1
			  AND lifecycle_status IN ('staged', 'active')
		`, string(version.ConfigVersionID), at, reason); err != nil {
			return err
		}
		_, err := transaction.Exec(ctx, `
			UPDATE deployments
			SET deployment_status = 'rolled_back', completed_at = $2
			WHERE config_version_id = $1 AND target_key = $3
		`, string(version.ConfigVersionID), at, version.TargetKey)
		return err
	})
}

func (ledger *PostgresLedger) transact(
	ctx context.Context,
	body func(pgx.Tx) error,
) (err error) {
	if ledger == nil || ledger.database == nil || ctx == nil {
		return fmt.Errorf("%w: no ledger", ErrProve)
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
	if err := body(transaction); err != nil {
		return err
	}
	return transaction.Commit(ctx)
}
