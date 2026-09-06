package databasemigrate

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"

	"github.com/jackc/pgx/v5"

	"github.com/mrAndreyIsachenko/hexroute/internal/database/migrations"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

const (
	advisoryLockName      = "hexroute-schema-migrations-v1"
	legacyBaselineVersion = uint64(11)
	ledgerVersion         = uint64(12)
)

var ErrMigration = errors.New("database migration failed")

type Database interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

type Runner struct {
	database Database
	random   io.Reader
}

func New(database Database, randomSource io.Reader) (*Runner, error) {
	if database == nil {
		return nil, ErrMigration
	}
	if randomSource == nil {
		randomSource = rand.Reader
	}
	return &Runner{database: database, random: randomSource}, nil
}

func (runner *Runner) Apply(
	ctx context.Context,
	username string,
	displayName string,
) (err error) {
	if ctx == nil || username == "" || displayName == "" {
		return ErrMigration
	}
	manifest, err := migrations.PostgreSQL()
	if err != nil || len(manifest) == 0 {
		return ErrMigration
	}
	transaction, err := runner.database.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ErrMigration
	}
	defer func() {
		rollbackErr := transaction.Rollback(ctx)
		if err == nil && rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			err = ErrMigration
		}
	}()

	if _, err = transaction.Exec(
		ctx,
		"SELECT pg_advisory_xact_lock(hashtextextended($1, 0))",
		advisoryLockName,
	); err != nil {
		return ErrMigration
	}
	if err = reconcileManifest(ctx, transaction, manifest); err != nil {
		return ErrMigration
	}
	if err = runner.seedPrincipal(ctx, transaction, username, displayName); err != nil {
		return ErrMigration
	}
	if err = transaction.Commit(ctx); err != nil {
		return ErrMigration
	}
	return nil
}

func reconcileManifest(
	ctx context.Context,
	transaction pgx.Tx,
	manifest []migrations.Migration,
) error {
	ledgerIndex := int(ledgerVersion - 1)
	if len(manifest) < int(ledgerVersion) ||
		manifest[ledgerIndex].Version != ledgerVersion ||
		manifest[ledgerIndex].Name != "schema_migration_ledger" {
		return ErrMigration
	}

	var ledgerExists bool
	if err := transaction.QueryRow(ctx, `
		SELECT to_regclass('public.hexroute_schema_migrations') IS NOT NULL
	`).Scan(&ledgerExists); err != nil {
		return err
	}
	if !ledgerExists {
		valid, err := verifyLegacyBaseline(ctx, transaction)
		if err != nil || !valid {
			return ErrMigration
		}
		if _, err = transaction.Exec(ctx, manifest[ledgerIndex].Up); err != nil {
			return err
		}
		if err = recordManifest(ctx, transaction, manifest[:ledgerIndex+1]); err != nil {
			return err
		}
		return applyManifest(ctx, transaction, manifest[ledgerIndex+1:])
	}

	applied, err := loadApplied(ctx, transaction)
	if err != nil {
		return err
	}
	if len(applied) == 0 {
		valid, verifyErr := verifyCurrentBaseline(ctx, transaction)
		if verifyErr != nil || !valid {
			return ErrMigration
		}
		if err = recordManifest(ctx, transaction, manifest[:ledgerIndex+1]); err != nil {
			return err
		}
		return applyManifest(ctx, transaction, manifest[ledgerIndex+1:])
	}
	if len(applied) > len(manifest) {
		return ErrMigration
	}
	for index, stored := range applied {
		expected := manifest[index]
		if stored.version != expected.Version ||
			stored.name != expected.Name ||
			stored.checksum != expected.UpChecksum {
			return ErrMigration
		}
	}
	return applyManifest(ctx, transaction, manifest[len(applied):])
}

func applyManifest(
	ctx context.Context,
	transaction pgx.Tx,
	manifest []migrations.Migration,
) error {
	for _, migration := range manifest {
		if _, err := transaction.Exec(ctx, migration.Up); err != nil {
			return err
		}
		if err := recordMigration(ctx, transaction, migration); err != nil {
			return err
		}
	}
	return nil
}

type appliedMigration struct {
	version  uint64
	name     string
	checksum string
}

func loadApplied(ctx context.Context, transaction pgx.Tx) ([]appliedMigration, error) {
	rows, err := transaction.Query(ctx, `
		SELECT version, name, up_sha256
		FROM hexroute_schema_migrations
		ORDER BY version
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]appliedMigration, 0)
	for rows.Next() {
		var item appliedMigration
		if err = rows.Scan(&item.version, &item.name, &item.checksum); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	if rows.Err() != nil {
		return nil, rows.Err()
	}
	return result, nil
}

func recordManifest(
	ctx context.Context,
	transaction pgx.Tx,
	manifest []migrations.Migration,
) error {
	for _, migration := range manifest {
		if err := recordMigration(ctx, transaction, migration); err != nil {
			return err
		}
	}
	return nil
}

func recordMigration(
	ctx context.Context,
	transaction pgx.Tx,
	migration migrations.Migration,
) error {
	_, err := transaction.Exec(ctx, `
		INSERT INTO hexroute_schema_migrations (version, name, up_sha256)
		VALUES ($1, $2, $3)
	`, migration.Version, migration.Name, migration.UpChecksum)
	return err
}

func (runner *Runner) seedPrincipal(
	ctx context.Context,
	transaction pgx.Tx,
	username string,
	displayName string,
) error {
	var count int
	if err := transaction.QueryRow(ctx, `
		SELECT count(*) FROM dashboard_principals
	`).Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return nil
	}
	principalID, err := metadata.NewUUID(runner.random)
	if err != nil {
		return err
	}
	userHandle := make([]byte, 32)
	if _, err = io.ReadFull(runner.random, userHandle); err != nil {
		return err
	}
	commandTag, err := transaction.Exec(ctx, `
		INSERT INTO dashboard_principals (
			principal_id,
			username,
			display_name,
			webauthn_user_handle,
			enabled
		) VALUES ($1, $2, $3, $4, TRUE)
	`, string(principalID), username, displayName, userHandle)
	if err != nil || commandTag.RowsAffected() != 1 {
		return fmt.Errorf("%w: seed principal", ErrMigration)
	}
	return nil
}

var legacyTables = []string{
	"nodes", "node_public_keys", "batches", "events",
	"node_sequence_cursors", "sequence_gaps", "security_audit_records",
	"latest_component_states", "sleep_intervals", "incidents",
	"incident_events", "incident_transitions", "incident_bundles",
	"config_versions", "deployments", "worker_heartbeats",
	"dashboard_principals", "passkey_credentials", "alert_deliveries",
	"slo_aggregates", "slo_incident_links", "incident_alert_outbox",
}

var baselineIndexes = []string{
	"sleep_intervals_one_open_per_node_idx",
	"sleep_intervals_start_event_idx",
	"sleep_intervals_end_event_idx",
	"alert_deliveries_claimable_idx",
	"incident_transitions_retention_idx",
	"sleep_intervals_retention_idx",
	"sequence_gaps_retention_idx",
	"alert_deliveries_terminal_retention_idx",
	"batches_retention_idx",
	"incident_bundles_incident_content_uidx",
	"incident_bundles_delete_due_idx",
}

var baselineColumns = []string{
	"alert_deliveries.claim_owner",
	"alert_deliveries.claim_until",
	"alert_deliveries.snapshot_status",
	"alert_deliveries.snapshot_severity",
	"alert_deliveries.snapshot_category",
	"alert_deliveries.snapshot_component",
	"alert_deliveries.snapshot_transitioned_at",
	"incident_bundles.delete_claim_owner",
	"incident_bundles.delete_claim_until",
	"incident_bundles.delete_attempt_count",
	"incident_bundles.next_delete_attempt_at",
	"incident_bundles.last_delete_result_code",
	"passkey_credentials.user_present",
	"passkey_credentials.user_verified",
	"passkey_credentials.backup_eligible",
	"passkey_credentials.backup_state",
	"passkey_credentials.clone_warning",
	"passkey_credentials.authenticator_attachment",
}

func verifyLegacyBaseline(ctx context.Context, transaction pgx.Tx) (bool, error) {
	return verifyBaseline(ctx, transaction, legacyTables)
}

func verifyCurrentBaseline(ctx context.Context, transaction pgx.Tx) (bool, error) {
	tables := append(append([]string(nil), legacyTables...), "hexroute_schema_migrations")
	return verifyBaseline(ctx, transaction, tables)
}

func verifyBaseline(
	ctx context.Context,
	transaction pgx.Tx,
	tables []string,
) (bool, error) {
	var valid bool
	err := transaction.QueryRow(ctx, `
		WITH expected_tables(name) AS (
			SELECT unnest($1::text[])
		),
		expected_indexes(name) AS (
			SELECT unnest($2::text[])
		),
		expected_columns(qualified_name) AS (
			SELECT unnest($3::text[])
		),
		expected_roles(name) AS (
			VALUES
				('hexroute_migrator'),
				('hexroute_ingest'),
				('hexroute_dashboard'),
				('hexroute_dashboard_auth'),
				('hexroute_maintenance')
		)
		SELECT
			NOT EXISTS (
				SELECT 1 FROM expected_tables
				WHERE to_regclass(format('%I.%I', 'public', name)) IS NULL
			)
			AND NOT EXISTS (
				SELECT 1
				FROM expected_tables e
				JOIN pg_class c ON c.oid = to_regclass(format('%I.%I', 'public', e.name))
				WHERE pg_get_userbyid(c.relowner) <> 'hexroute_migrator'
			)
			AND NOT EXISTS (
				SELECT 1 FROM expected_indexes
				WHERE to_regclass(format('%I.%I', 'public', name)) IS NULL
			)
			AND NOT EXISTS (
				SELECT 1 FROM expected_columns e
				WHERE NOT EXISTS (
					SELECT 1 FROM information_schema.columns c
					WHERE c.table_schema = 'public'
					  AND c.table_name = split_part(e.qualified_name, '.', 1)
					  AND c.column_name = split_part(e.qualified_name, '.', 2)
				)
			)
			AND (
				SELECT count(*) = 5 FROM pg_roles r
				JOIN expected_roles e ON e.name = r.rolname
				WHERE NOT r.rolcanlogin AND NOT r.rolsuper AND NOT r.rolcreatedb
				  AND NOT r.rolcreaterole AND NOT r.rolreplication
				  AND NOT r.rolbypassrls
			)
			AND pg_has_role(CURRENT_USER, 'hexroute_migrator', 'member')
			AND has_table_privilege('hexroute_ingest', 'worker_heartbeats', 'SELECT')
	`, tables, baselineIndexes, baselineColumns).Scan(&valid)
	return valid, err
}
