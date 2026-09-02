#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# The migrations below come from this checkout, and so must the packages tested
# against them. Without this the script applies one tree's schema and then runs
# whatever tree the caller happened to be standing in.
cd "$repo_root"
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

# Both waits below say what they saw when they give up. This script used to
# fail here without printing anything: it ran `docker exec` before the
# container was necessarily running, and its readiness check discarded stdout —
# which is exactly where pg_isready writes its diagnosis. An intermittent
# failure that explains nothing is worse than a slower one that does, because
# the next person has to reproduce it before they can start.

report_container_state() {
  printf '  status: %s\n' \
    "$(docker inspect --format '{{.State.Status}}' "$container" 2>&1)" >&2
  docker logs --tail 50 "$container" 2>&1 | sed 's/^/  /' >&2 || true
}

wait_for_container() {
  local deadline=$((SECONDS + 60))
  while [ "$SECONDS" -lt "$deadline" ]; do
    if [ "$(docker inspect --format '{{.State.Status}}' "$container" 2>/dev/null)" \
      = running ]; then
      return 0
    fi
    sleep 1
  done
  printf 'container %s never reached the running state\n' "$container" >&2
  report_container_state
  return 1
}

# The first server this container runs is not the one the tests use.
#
# The postgres image bootstraps by starting a temporary server, initialising
# the cluster against it, then shutting it down and starting the real one.
# pg_isready answers yes to that temporary server, so a single successful probe
# can land in the window before the shutdown — which is how a run failed with
# `connection to server on socket ... failed: No such file or directory` two
# seconds after the image finished pulling.
#
# So readiness is three consecutive successful queries, a second apart. The
# bootstrap shutdown falls inside that span, and a real server stays up across
# it. A query rather than pg_isready, because accepting a connection and
# answering are different claims.
wait_for_postgres() {
  local deadline=$((SECONDS + 90))
  local consecutive=0
  while [ "$SECONDS" -lt "$deadline" ]; do
    if docker exec "$container" psql --username postgres --dbname postgres \
      --no-psqlrc --quiet --tuples-only --command 'select 1' >/dev/null 2>&1; then
      consecutive=$((consecutive + 1))
      if [ "$consecutive" -ge 3 ]; then
        return 0
      fi
    else
      consecutive=0
    fi
    sleep 1
  done
  printf 'PostgreSQL in %s did not answer three queries in a row within 90s\n' \
    "$container" >&2
  docker exec "$container" pg_isready -U postgres -d postgres 2>&1 \
    | sed 's/^/  /' >&2 || true
  report_container_state
  return 1
}

wait_for_container
docker exec "$container" mkdir -p /migrations
docker cp "$migration_dir/." "$container:/migrations"
wait_for_postgres

mapfile_compat() {
  local pattern="$1"
  find "$migration_dir" -maxdepth 1 -type f -name "$pattern" -print | sort
}

while IFS= read -r migration; do
	[[ "$(basename "$migration")" < "000012_" ]] || continue
  docker exec -i "$container" psql \
    --username postgres \
    --dbname postgres \
    --set ON_ERROR_STOP=1 \
    --single-transaction <"$migration" >/dev/null
done < <(mapfile_compat '*.up.sql')

published_address="$(docker port "$container" 5432/tcp | tail -n 1)"
postgres_port="${published_address##*:}"
HEXROUTE_TEST_POSTGRES_ADMIN_DSN="postgres://postgres@127.0.0.1:${postgres_port}/postgres?sslmode=disable" \
GOCACHE=/tmp/hexroute-postgres-go-cache \
  go test ./internal/databasemigrate \
    -run TestPostgresRunnerAdoptsBaselineAndSeedsOnePrincipal \
    -count=1

required_tables=(
  nodes node_public_keys batches events node_sequence_cursors sequence_gaps
  security_audit_records latest_component_states sleep_intervals incidents
  incident_events incident_transitions incident_bundles config_versions
  deployments worker_heartbeats dashboard_principals passkey_credentials
  alert_deliveries incident_alert_outbox slo_aggregates slo_incident_links
  connectivity_snapshots connectivity_snapshot_components
  connectivity_snapshot_proposal_classes
  hexroute_schema_migrations cutover_write_control
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
  hexroute_migrator hexroute_ingest hexroute_dashboard hexroute_dashboard_auth
  hexroute_maintenance
)
restricted_count="$(docker exec "$container" psql --username postgres --dbname postgres \
  --tuples-only --no-align --command \
  "SELECT count(*) FROM pg_roles
   WHERE rolname IN ('hexroute_migrator', 'hexroute_ingest', 'hexroute_dashboard', 'hexroute_dashboard_auth', 'hexroute_maintenance')
     AND NOT rolcanlogin
     AND NOT rolsuper
     AND NOT rolcreatedb
     AND NOT rolcreaterole
     AND NOT rolreplication
     AND NOT rolbypassrls;")"
[[ "$restricted_count" == "5" ]] || {
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
expect_allowed hexroute_ingest \
  'SELECT write_frozen, frozen_at, deadline_at FROM cutover_write_control'
expect_allowed hexroute_dashboard \
  'SELECT incident_id FROM incidents LIMIT 0'
expect_allowed hexroute_dashboard_auth \
  'SELECT credential_id, cose_public_key FROM passkey_credentials LIMIT 0'
expect_allowed hexroute_dashboard_auth \
  'UPDATE passkey_credentials SET sign_count = sign_count WHERE FALSE'
expect_allowed hexroute_dashboard_auth \
  "INSERT INTO passkey_credentials (
     passkey_credential_id,
     principal_id,
     credential_id,
     cose_public_key
   )
   SELECT
     '40000000-0000-4000-8000-000000000004',
     principal_id,
     decode(repeat('00', 16), 'hex'),
     decode('00', 'hex')
   FROM dashboard_principals
   WHERE FALSE"
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
expect_allowed hexroute_maintenance \
  'UPDATE incident_alert_outbox SET last_result_code = last_result_code WHERE FALSE'

expect_denied hexroute_ingest \
  'SELECT principal_id FROM passkey_credentials LIMIT 0'
expect_denied hexroute_ingest \
  'SELECT incident_id FROM incidents LIMIT 0'
expect_denied hexroute_ingest \
  "UPDATE nodes SET lifecycle_status = 'revoked' WHERE FALSE"
expect_denied hexroute_ingest \
  'ALTER TABLE events ADD COLUMN forbidden_ingest_column TEXT'
expect_denied hexroute_ingest \
  'UPDATE cutover_write_control SET write_frozen = FALSE WHERE singleton'
expect_denied hexroute_dashboard \
  'SELECT payload FROM events LIMIT 0'
expect_denied hexroute_dashboard \
  'SELECT credential_id FROM passkey_credentials LIMIT 0'
expect_denied hexroute_dashboard \
  'UPDATE passkey_credentials SET sign_count = sign_count WHERE FALSE'
expect_denied hexroute_dashboard_auth \
  'SELECT incident_id FROM incidents LIMIT 0'
expect_denied hexroute_dashboard_auth \
  'SELECT node_id FROM nodes LIMIT 0'
expect_denied hexroute_dashboard \
  'SELECT incident_id FROM incident_alert_outbox LIMIT 0'
expect_denied hexroute_dashboard \
  "INSERT INTO security_audit_records (
     audit_record_id, category, reason_code
   ) VALUES (
     '30000000-0000-4000-8000-000000000003', 'schema', 'forbidden'
   )"
expect_allowed hexroute_dashboard \
  'SELECT aggregate_state, open_gaps FROM connectivity_snapshots LIMIT 0'
expect_allowed hexroute_dashboard \
  'SELECT component, component_state FROM connectivity_snapshot_components LIMIT 0'
expect_allowed hexroute_maintenance \
  "UPDATE connectivity_snapshots SET aggregate_state = 'ready' WHERE FALSE"

# The read model is derived by the worker and read by the page. Neither the
# ingest role nor the dashboard may write it: ingestion stores signed events
# and nothing more, and a rendering path that could write is a control path.
expect_denied hexroute_ingest \
  'SELECT aggregate_state FROM connectivity_snapshots LIMIT 0'
expect_denied hexroute_dashboard \
  "UPDATE connectivity_snapshots SET aggregate_state = 'ready' WHERE FALSE"
expect_denied hexroute_dashboard \
  'DELETE FROM connectivity_snapshot_components WHERE FALSE'

# The projection alphabet is enforced by the schema, not only by the encoder.
expect_denied hexroute_maintenance \
  "INSERT INTO connectivity_snapshot_components (
     node_id, component, component_state, freshness, diff_reason
   ) VALUES (
     '50000000-0000-4000-8000-000000000005',
     '198.51.100.7', 'ready', 'fresh', 'none'
   )"

expect_denied hexroute_maintenance \
  'SELECT credential_id FROM passkey_credentials LIMIT 0'
expect_denied hexroute_maintenance \
  'UPDATE dashboard_principals SET enabled = enabled WHERE FALSE'
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
             GRANT hexroute_maintenance TO hexroute_test_maintenance;
             CREATE ROLE hexroute_test_dashboard LOGIN;
             GRANT hexroute_dashboard TO hexroute_test_dashboard;
             CREATE ROLE hexroute_test_dashboard_auth LOGIN;
             GRANT hexroute_dashboard_auth TO hexroute_test_dashboard_auth;" >/dev/null

HEXROUTE_TEST_POSTGRES_ADMIN_DSN="postgres://postgres@127.0.0.1:${postgres_port}/postgres?sslmode=disable" \
HEXROUTE_TEST_POSTGRES_INGEST_DSN="postgres://hexroute_test_ingest@127.0.0.1:${postgres_port}/postgres?sslmode=disable" \
GOCACHE=/tmp/hexroute-postgres-go-cache \
  go test ./internal/cutoverfreeze \
    -run TestPostgresFreezeDrainsInflightWritesAndRejectsLaterWrites \
    -count=1

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

# The connectivity projection is the one cloud read model an operator reads
# when they cannot reach the host, so a row rendered as current when it is not
# is the one way telemetry could mislead them. The schema test is also the only
# mechanized form of the claim that an address, a path or a digest is
# unstorable here even if an encoder regressed.
HEXROUTE_TEST_POSTGRES_ADMIN_DSN="postgres://postgres@127.0.0.1:${postgres_port}/postgres?sslmode=disable" \
HEXROUTE_TEST_POSTGRES_MAINTENANCE_DSN="postgres://hexroute_test_maintenance@127.0.0.1:${postgres_port}/postgres?sslmode=disable" \
GOCACHE=/tmp/hexroute-postgres-go-cache \
  go test ./internal/cloudconnectivity \
    -run 'TestPostgresProjection(IsOrderedIdempotentAndStaleSafe|SchemaRefusesUnboundedTokens)' \
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
    -run 'TestPostgres(AlertQueueLeasesRetriesAndKeepsLocalAckIsolated|IncidentOutboxQueuesSnapshotExactlyOnce)' \
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
  go test ./internal/slo \
    -run TestPostgresSLOUpsertIsIdempotentAndPreservesIncidentLinks \
    -count=1

HEXROUTE_TEST_POSTGRES_ADMIN_DSN="postgres://postgres@127.0.0.1:${postgres_port}/postgres?sslmode=disable" \
HEXROUTE_TEST_POSTGRES_AUTH_DSN="postgres://hexroute_test_dashboard_auth@127.0.0.1:${postgres_port}/postgres?sslmode=disable" \
GOCACHE=/tmp/hexroute-postgres-go-cache \
  go test ./internal/dashboardauth \
    -run 'TestPostgresPasskey(StoreUsesNarrowAuthRole|LoginAuthorizesSessionAndAdvancesCounter)' \
    -count=1

HEXROUTE_TEST_POSTGRES_ADMIN_DSN="postgres://postgres@127.0.0.1:${postgres_port}/postgres?sslmode=disable" \
HEXROUTE_TEST_POSTGRES_DASHBOARD_DSN="postgres://hexroute_test_dashboard@127.0.0.1:${postgres_port}/postgres?sslmode=disable" \
GOCACHE=/tmp/hexroute-postgres-go-cache \
  go test ./internal/dashboard \
    -run TestPostgresDashboardLoadsBoundedReadOnlySnapshot \
    -count=1

HEXROUTE_TEST_POSTGRES_ADMIN_DSN="postgres://postgres@127.0.0.1:${postgres_port}/postgres?sslmode=disable" \
HEXROUTE_TEST_POSTGRES_INGEST_DSN="postgres://hexroute_test_ingest@127.0.0.1:${postgres_port}/postgres?sslmode=disable" \
HEXROUTE_TEST_POSTGRES_DASHBOARD_DSN="postgres://hexroute_test_dashboard@127.0.0.1:${postgres_port}/postgres?sslmode=disable" \
HEXROUTE_TEST_POSTGRES_AUTH_DSN="postgres://hexroute_test_dashboard_auth@127.0.0.1:${postgres_port}/postgres?sslmode=disable" \
HEXROUTE_TEST_POSTGRES_MAINTENANCE_DSN="postgres://hexroute_test_maintenance@127.0.0.1:${postgres_port}/postgres?sslmode=disable" \
GOCACHE=/tmp/hexroute-postgres-go-cache \
  go test ./internal/cloudruntime \
    -run 'TestPostgres(APIRuntimeRequiresExclusiveRolesAndBuildsReadOnlySurface|WorkerRuntimeHeartbeatsAndShutsDown)' \
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
             DROP ROLE hexroute_test_maintenance;
             DROP ROLE hexroute_test_dashboard;
             DROP ROLE hexroute_test_dashboard_auth;" >/dev/null

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
