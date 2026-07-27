package migrations

import (
	"regexp"
	"strings"
	"testing"
)

func TestPostgreSQLMigrationManifest(t *testing.T) {
	migrations, err := PostgreSQL()
	if err != nil {
		t.Fatalf("PostgreSQL() error = %v", err)
	}

	wantNames := []string{
		"identity_and_ingest",
		"operations",
		"access_delivery_and_slo",
		"access_roles",
		"ingest_readiness_grant",
		"sleep_interval_invariants",
		"alert_delivery_leases",
		"retention_indexes",
		"incident_bundle_expiry",
		"dashboard_passkey_auth",
		"incident_alert_outbox",
		"schema_migration_ledger",
		"cutover_write_freeze",
	}
	if len(migrations) != len(wantNames) {
		t.Fatalf("migration count = %d, want %d", len(migrations), len(wantNames))
	}
	for index, migration := range migrations {
		wantVersion := uint64(index + 1)
		if migration.Version != wantVersion || migration.Name != wantNames[index] {
			t.Fatalf("migration[%d] = %06d_%s", index, migration.Version, migration.Name)
		}
		if len(migration.UpChecksum) != 64 {
			t.Fatalf("migration[%d] checksum length = %d, want 64", index, len(migration.UpChecksum))
		}
		if strings.Contains(strings.ToUpper(migration.Up), "DROP TABLE") {
			t.Fatalf("migration[%d] up step contains destructive DROP TABLE", index)
		}
	}
}

func TestPostgreSQLMigrationsDefineRequiredCloudData(t *testing.T) {
	migrations, err := PostgreSQL()
	if err != nil {
		t.Fatalf("PostgreSQL() error = %v", err)
	}
	var up strings.Builder
	for _, migration := range migrations {
		up.WriteString(migration.Up)
	}
	schema := strings.ToLower(up.String())

	requiredTables := []string{
		"nodes",
		"node_public_keys",
		"batches",
		"events",
		"node_sequence_cursors",
		"sequence_gaps",
		"security_audit_records",
		"latest_component_states",
		"sleep_intervals",
		"incidents",
		"incident_events",
		"incident_transitions",
		"incident_bundles",
		"config_versions",
		"deployments",
		"worker_heartbeats",
		"dashboard_principals",
		"passkey_credentials",
		"alert_deliveries",
		"incident_alert_outbox",
		"slo_aggregates",
		"slo_incident_links",
		"hexroute_schema_migrations",
		"cutover_write_control",
	}
	for _, table := range requiredTables {
		if !strings.Contains(schema, "create table "+table) {
			t.Errorf("required table %q is missing", table)
		}
	}

	requiredFragments := []string{
		"public_key bytea",
		"content_sha256 bytea",
		"payload jsonb",
		"unique (node_id, boot_session_id, sequence)",
		"credential_id bytea",
		"cose_public_key bytea",
		"check (granularity in ('hour', 'day'))",
		"create role %i nologin nosuperuser",
		"grant select on nodes, node_public_keys to hexroute_ingest",
		"to hexroute_dashboard",
		"to hexroute_maintenance",
		"message = 'write_frozen'",
		"for share",
		"create trigger hexroute_write_gate",
	}
	for _, fragment := range requiredFragments {
		if !strings.Contains(schema, fragment) {
			t.Errorf("required schema fragment %q is missing", fragment)
		}
	}
}

func TestPostgreSQLWriteGateCoversEveryRuntimeMutableTable(t *testing.T) {
	migrations, err := PostgreSQL()
	if err != nil {
		t.Fatalf("PostgreSQL() error = %v", err)
	}
	gate := strings.ToLower(migrations[len(migrations)-1].Up)
	protected := []string{
		"nodes", "batches", "events", "node_sequence_cursors",
		"sequence_gaps", "security_audit_records", "latest_component_states",
		"sleep_intervals", "incidents", "incident_events",
		"incident_transitions", "incident_bundles", "worker_heartbeats",
		"dashboard_principals", "passkey_credentials", "alert_deliveries",
		"incident_alert_outbox", "slo_aggregates", "slo_incident_links",
	}
	for _, table := range protected {
		if !strings.Contains(gate, "'"+table+"'") {
			t.Errorf("runtime mutable table %q is not write-gated", table)
		}
	}
	if strings.Contains(gate, "'cutover_write_control'") {
		t.Fatal("cutover control table must not gate its own operator mutation")
	}
}

func TestPostgreSQLMigrationsDoNotModelCredentialsOrRawLogs(t *testing.T) {
	migrations, err := PostgreSQL()
	if err != nil {
		t.Fatalf("PostgreSQL() error = %v", err)
	}
	forbidden := regexp.MustCompile(`(?i)\b(password|email|totp|otp_seed|pin_code|private_key|vless|reality_secret|raw_log|packet_capture)\b`)
	for _, migration := range migrations {
		if match := forbidden.FindString(migration.Up); match != "" {
			t.Fatalf("migration %06d contains forbidden credential/raw-data field %q", migration.Version, match)
		}
	}
}

func TestPostgreSQLRoleHardeningSupportsManagedAdministrators(t *testing.T) {
	migrations, err := PostgreSQL()
	if err != nil {
		t.Fatalf("PostgreSQL() error = %v", err)
	}
	for _, migration := range migrations {
		normalized := strings.ToUpper(migration.Up)
		for _, block := range strings.Split(normalized, "ALTER ROLE")[1:] {
			statement := strings.SplitN(block, ";", 2)[0]
			for _, privilegedAttribute := range []string{
				"NOSUPERUSER",
				"NOREPLICATION",
				"NOBYPASSRLS",
			} {
				if strings.Contains(statement, privilegedAttribute) {
					t.Fatalf(
						"migration %06d alters managed-only role attribute %s",
						migration.Version,
						privilegedAttribute,
					)
				}
			}
		}
	}
}

func TestPostgreSQLDownMigrationsAreExplicit(t *testing.T) {
	migrations, err := PostgreSQL()
	if err != nil {
		t.Fatalf("PostgreSQL() error = %v", err)
	}
	for _, migration := range migrations {
		down := strings.ToLower(migration.Down)
		if !strings.Contains(down, "drop table") &&
			!strings.Contains(down, "drop role") &&
			!strings.Contains(down, "drop index") &&
			!strings.Contains(down, "revoke") {
			t.Fatalf("migration %06d has no explicit test rollback", migration.Version)
		}
	}
}
