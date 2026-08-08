#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

export GOCACHE="${GOCACHE:-${TMPDIR:-/tmp}/hexroute-go-cache}"

local_binaries=(
  ./cmd/hexrouted
  ./cmd/hexroute-userd
  ./cmd/hexroute-policy
  ./cmd/hexroutectl
)

cloud_dependencies='(^|/)internal/(cloudruntime|cloudingest|database|databasemigrate|dashboard|alertdelivery|cloudincident|incidentbundle|retention|slo|silentnode)$|^github\.com/jackc/pgx'
local_dependencies="$(go list -deps "${local_binaries[@]}")"
if printf '%s\n' "$local_dependencies" | grep -Eq "$cloud_dependencies"; then
  printf 'local policy path imports cloud, PostgreSQL, API, or worker authority:\n' >&2
  printf '%s\n' "$local_dependencies" | grep -E "$cloud_dependencies" >&2
  exit 1
fi

local_authority='(^|/)internal/(ipc|operator|policycontrol|policystore|actionlease|actionplan|resumeexecutor|resumeplan|rootdaemon|userdaemon|routeplan|pritunlplan|pritunlrescue|credentials|observe)$'
cloud_runtime_dependencies="$(go list -deps ./cmd/hexroute-ingest)"
if printf '%s\n' "$cloud_runtime_dependencies" | grep -Eq "$local_authority"; then
  printf 'cloud runtime imports local mutation or activation authority:\n' >&2
  printf '%s\n' "$cloud_runtime_dependencies" | grep -E "$local_authority" >&2
  exit 1
fi

printf 'ok: local policy paths are independent of cloud services and cloud runtime has no local mutation authority\n'
