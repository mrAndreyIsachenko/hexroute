#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
migration_dir="$repo_root/internal/database/migrations/postgres"
container="hexroute-postgres-migrations-$$"

cleanup() {
  docker rm -f "$container" >/dev/null 2>&1 || true
}
trap cleanup EXIT

docker run --detach --rm \
  --name "$container" \
  --env POSTGRES_HOST_AUTH_METHOD=trust \
  --publish 127.0.0.1::5432 \
  postgres:17-alpine >/dev/null

docker exec "$container" mkdir -p /migrations
docker cp "$migration_dir/." "$container:/migrations"

for _ in $(seq 1 60); do
  if docker exec "$container" pg_isready -U postgres -d postgres >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
docker exec "$container" pg_isready -U postgres -d postgres >/dev/null

mapfile_compat() {
  local pattern="$1"
  find "$migration_dir" -maxdepth 1 -type f -name "$pattern" -print | sort
}

while IFS= read -r migration; do
  docker exec -i "$container" psql \
    --username postgres \
    --dbname postgres \
    --set ON_ERROR_STOP=1 \
    --single-transaction <"$migration" >/dev/null
done < <(mapfile_compat '*.up.sql')

required_tables=(
  nodes node_public_keys batches events node_sequence_cursors sequence_gaps
  security_audit_records latest_component_states sleep_intervals incidents
  incident_events incident_transitions incident_bundles config_versions
  deployments worker_heartbeats dashboard_principals passkey_credentials
  alert_deliveries slo_aggregates slo_incident_links
)
for table in "${required_tables[@]}"; do
  found="$(docker exec "$container" psql --username postgres --dbname postgres \
    --tuples-only --no-align --command \
    "SELECT to_regclass('public.$table') IS NOT NULL;")"
  [[ "$found" == "t" ]] || {
    printf 'missing PostgreSQL table: %s\n' "$table" >&2
    exit 1
  }
done

restricted_roles=(
  hexroute_migrator hexroute_ingest hexroute_dashboard hexroute_maintenance
)
restricted_count="$(docker exec "$container" psql --username postgres --dbname postgres \
  --tuples-only --no-align --command \
  "SELECT count(*) FROM pg_roles
   WHERE rolname IN ('hexroute_migrator', 'hexroute_ingest', 'hexroute_dashboard', 'hexroute_maintenance')
     AND NOT rolcanlogin
     AND NOT rolsuper
     AND NOT rolcreatedb
     AND NOT rolcreaterole
     AND NOT rolreplication
     AND NOT rolbypassrls;")"
[[ "$restricted_count" == "4" ]] || {
  printf 'PostgreSQL role hardening mismatch: %s\n' "$restricted_count" >&2
  exit 1
}

public_create="$(docker exec "$container" psql --username postgres --dbname postgres \
  --tuples-only --no-align --command \
  "SELECT has_schema_privilege('hexroute_dashboard', 'public', 'CREATE');")"
[[ "$public_create" == "f" ]] || {
  printf 'dashboard unexpectedly has public schema CREATE\n' >&2
  exit 1
}

owned_count="$(docker exec "$container" psql --username postgres --dbname postgres \
  --tuples-only --no-align --command \
  "SELECT count(*) FROM pg_class
   WHERE relnamespace = 'public'::regnamespace
     AND relkind IN ('r', 'p')
     AND pg_get_userbyid(relowner) = 'hexroute_migrator';")"
[[ "$owned_count" == "${#required_tables[@]}" ]] || {
  printf 'migrator owns %s tables, expected %s\n' \
    "$owned_count" "${#required_tables[@]}" >&2
  exit 1
}

run_as() {
  local role="$1"
  local statement="$2"
  docker exec "$container" psql \
    --username postgres \
    --dbname postgres \
    --set ON_ERROR_STOP=1 \
    --command "BEGIN; SET LOCAL ROLE $role; $statement; ROLLBACK;" >/dev/null
}

expect_allowed() {
  local role="$1"
  local statement="$2"
  if ! run_as "$role" "$statement"; then
    printf 'expected PostgreSQL operation to be allowed for %s\n' "$role" >&2
    exit 1
  fi
}

expect_denied() {
  local role="$1"
  local statement="$2"
  if run_as "$role" "$statement" 2>/dev/null; then
    printf 'expected PostgreSQL operation to be denied for %s\n' "$role" >&2
    exit 1
  fi
}

expect_allowed hexroute_migrator \
  'CREATE TABLE role_ddl_probe (probe_id BIGINT PRIMARY KEY)'
expect_allowed hexroute_ingest \
  "INSERT INTO security_audit_records (
     audit_record_id, category, reason_code
   ) VALUES (
     '10000000-0000-4000-8000-000000000001', 'schema', 'integration_probe'
   )"
expect_allowed hexroute_ingest \
  'UPDATE nodes SET last_seen_at = CURRENT_TIMESTAMP WHERE FALSE'
expect_allowed hexroute_ingest \
  'SELECT heartbeat_at FROM worker_heartbeats LIMIT 0'
expect_allowed hexroute_dashboard \
  'SELECT incident_id FROM incidents LIMIT 0'
expect_allowed hexroute_dashboard \
  'SELECT credential_id, cose_public_key FROM passkey_credentials LIMIT 0'
expect_allowed hexroute_maintenance \
  "INSERT INTO worker_heartbeats (
     worker_name, instance_id, application_version, started_at, heartbeat_at
   ) VALUES (
     'integration-probe',
     '20000000-0000-4000-8000-000000000002',
     'test',
     CURRENT_TIMESTAMP,
     CURRENT_TIMESTAMP
   )"
expect_allowed hexroute_maintenance \
  'DELETE FROM events WHERE FALSE'
expect_allowed hexroute_maintenance \
  "UPDATE latest_component_states
   SET reason_code = 'worker_probe'
   WHERE FALSE"

expect_denied hexroute_ingest \
  'SELECT principal_id FROM passkey_credentials LIMIT 0'
expect_denied hexroute_ingest \
  "UPDATE nodes SET lifecycle_status = 'revoked' WHERE FALSE"
expect_denied hexroute_ingest \
  'ALTER TABLE events ADD COLUMN forbidden_ingest_column TEXT'
expect_denied hexroute_dashboard \
  'SELECT payload FROM events LIMIT 0'
expect_denied hexroute_dashboard \
  "INSERT INTO security_audit_records (
     audit_record_id, category, reason_code
   ) VALUES (
     '30000000-0000-4000-8000-000000000003', 'schema', 'forbidden'
   )"
expect_denied hexroute_maintenance \
  'SELECT credential_id FROM passkey_credentials LIMIT 0'
expect_denied hexroute_maintenance \
  'ALTER TABLE incidents ADD COLUMN forbidden_maintenance_column TEXT'
expect_denied hexroute_maintenance \
  'GRANT hexroute_dashboard TO hexroute_maintenance'

docker exec "$container" psql \
  --username postgres \
  --dbname postgres \
  --set ON_ERROR_STOP=1 \
  --command "CREATE ROLE hexroute_test_ingest LOGIN;
             GRANT hexroute_ingest TO hexroute_test_ingest;
             CREATE ROLE hexroute_test_maintenance LOGIN;
             GRANT hexroute_maintenance TO hexroute_test_maintenance;" >/dev/null

published_address="$(docker port "$container" 5432/tcp | tail -n 1)"
postgres_port="${published_address##*:}"
HEXROUTE_TEST_POSTGRES_ADMIN_DSN="postgres://postgres@127.0.0.1:${postgres_port}/postgres?sslmode=disable" \
HEXROUTE_TEST_POSTGRES_INGEST_DSN="postgres://hexroute_test_ingest@127.0.0.1:${postgres_port}/postgres?sslmode=disable" \
GOCACHE=/tmp/hexroute-postgres-go-cache \
  go test ./internal/cloudingest \
    -run TestPostgresStorePersistsDeduplicatesAndTracksSequenceGaps \
    -count=1

HEXROUTE_TEST_POSTGRES_ADMIN_DSN="postgres://postgres@127.0.0.1:${postgres_port}/postgres?sslmode=disable" \
HEXROUTE_TEST_POSTGRES_INGEST_DSN="postgres://hexroute_test_ingest@127.0.0.1:${postgres_port}/postgres?sslmode=disable" \
HEXROUTE_TEST_POSTGRES_MAINTENANCE_DSN="postgres://hexroute_test_maintenance@127.0.0.1:${postgres_port}/postgres?sslmode=disable" \
GOCACHE=/tmp/hexroute-postgres-go-cache \
  go test ./internal/cloudhealth \
    -run TestPostgresHeartbeatDrivesReadiness \
    -count=1

HEXROUTE_TEST_POSTGRES_ADMIN_DSN="postgres://postgres@127.0.0.1:${postgres_port}/postgres?sslmode=disable" \
HEXROUTE_TEST_POSTGRES_MAINTENANCE_DSN="postgres://hexroute_test_maintenance@127.0.0.1:${postgres_port}/postgres?sslmode=disable" \
GOCACHE=/tmp/hexroute-postgres-go-cache \
  go test ./internal/silentnode \
    -run TestPostgresSleepProjectionSuppressesOnlyExplicitSleep \
    -count=1

HEXROUTE_TEST_POSTGRES_ADMIN_DSN="postgres://postgres@127.0.0.1:${postgres_port}/postgres?sslmode=disable" \
HEXROUTE_TEST_POSTGRES_MAINTENANCE_DSN="postgres://hexroute_test_maintenance@127.0.0.1:${postgres_port}/postgres?sslmode=disable" \
GOCACHE=/tmp/hexroute-postgres-go-cache \
  go test ./internal/cloudincident \
    -run TestPostgresIncidentLifecycleIsIdempotentAndSleepAware \
    -count=1

HEXROUTE_TEST_POSTGRES_ADMIN_DSN="postgres://postgres@127.0.0.1:${postgres_port}/postgres?sslmode=disable" \
HEXROUTE_TEST_POSTGRES_MAINTENANCE_DSN="postgres://hexroute_test_maintenance@127.0.0.1:${postgres_port}/postgres?sslmode=disable" \
GOCACHE=/tmp/hexroute-postgres-go-cache \
  go test ./internal/alertdelivery \
    -run TestPostgresAlertQueueLeasesRetriesAndKeepsLocalAckIsolated \
    -count=1

HEXROUTE_TEST_POSTGRES_ADMIN_DSN="postgres://postgres@127.0.0.1:${postgres_port}/postgres?sslmode=disable" \
HEXROUTE_TEST_POSTGRES_MAINTENANCE_DSN="postgres://hexroute_test_maintenance@127.0.0.1:${postgres_port}/postgres?sslmode=disable" \
GOCACHE=/tmp/hexroute-postgres-go-cache \
  go test ./internal/incidentbundle \
    -run TestPostgresIncidentBundleIsPrivateBoundedAndExpires \
    -count=1

HEXROUTE_TEST_POSTGRES_ADMIN_DSN="postgres://postgres@127.0.0.1:${postgres_port}/postgres?sslmode=disable" \
HEXROUTE_TEST_POSTGRES_MAINTENANCE_DSN="postgres://hexroute_test_maintenance@127.0.0.1:${postgres_port}/postgres?sslmode=disable" \
GOCACHE=/tmp/hexroute-postgres-go-cache \
  go test ./internal/retention \
    -run TestPostgresRetentionIsBoundedAndPreservesDurableRecords \
    -count=1

docker exec "$container" psql \
  --username postgres \
  --dbname postgres \
  --set ON_ERROR_STOP=1 \
  --command "DROP ROLE hexroute_test_ingest;
             DROP ROLE hexroute_test_maintenance;" >/dev/null

while IFS= read -r migration; do
  docker exec -i "$container" psql \
    --username postgres \
    --dbname postgres \
    --set ON_ERROR_STOP=1 \
    --single-transaction <"$migration" >/dev/null
done < <(mapfile_compat '*.down.sql' | sort -r)

remaining="$(docker exec "$container" psql --username postgres --dbname postgres \
  --tuples-only --no-align --command \
  "SELECT count(*) FROM pg_tables WHERE schemaname = 'public';")"
[[ "$remaining" == "0" ]] || {
  printf 'PostgreSQL rollback left %s public tables\n' "$remaining" >&2
  exit 1
}

printf 'ok: PostgreSQL migrations apply and roll back on PostgreSQL 17\n'
