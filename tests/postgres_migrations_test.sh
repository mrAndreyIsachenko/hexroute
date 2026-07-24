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
