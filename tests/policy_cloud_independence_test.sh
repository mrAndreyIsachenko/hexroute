#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

export GOCACHE="${GOCACHE:-${TMPDIR:-/tmp}/hexroute-go-cache}"

# Binaries that run on the operator's machine and must never reach cloud,
# database or worker authority.
#
# This is a declaration and cannot be derived: inferring it from "has no cloud
# dependency" would make the check below assert itself. So every command is in
# one of the two lists, and the census at the end fails on a command in
# neither — which is how five of these came to be unguarded. They were added
# after the list was written and nothing asked where they belonged.
local_binaries=(
  ./cmd/hexrouted
  ./cmd/hexroute-userd
  ./cmd/hexroute-policy
  ./cmd/hexroutectl
  ./cmd/hexroute-sentinel
  ./cmd/hexroute-archive-report
  ./cmd/hexroute-archive-annotate
  ./cmd/hexroute-connectivity-replay
  ./cmd/hexroute-connectivity-watch
  ./cmd/hexroute-connectivity-qualify
  ./cmd/hexroute-policy-installer
  ./cmd/hexroute-policy-qualification
)

# Binaries that are the cloud, or that build and inspect releases rather than
# running on the host. They are exempt from the check above by being what it
# protects against.
cloud_binaries=(
  ./cmd/hexroute-ingest
  ./cmd/hexroute-ingress-observer
  ./cmd/hexroute-ingress-probe
  ./cmd/hexroute-package-observer
)

cloud_dependencies='(^|/)internal/(cloudruntime|cloudingest|cloudconnectivity|database|databasemigrate|dashboard|alertdelivery|cloudincident|incidentbundle|retention|slo|silentnode)$|^github\.com/jackc/pgx'
local_dependencies="$(go list -deps "${local_binaries[@]}")"
if printf '%s\n' "$local_dependencies" | grep -Eq "$cloud_dependencies"; then
  printf 'local policy path imports cloud, PostgreSQL, API, or worker authority:\n' >&2
  printf '%s\n' "$local_dependencies" | grep -E "$cloud_dependencies" >&2
  exit 1
fi

# The read model is a local authority too. A cloud runtime that could import
# the reducer, the acceptor or the checkpoint store could derive a snapshot the
# host never produced, and then the dashboard would be showing a conclusion
# nobody on the host reached.
local_authority='(^|/)internal/(ipc|operator|policycontrol|policystore|actionlease|actionplan|resumeexecutor|resumeplan|rootdaemon|userdaemon|routeplan|pritunlplan|pritunlrescue|credentials|observe|connectivityaccept|connectivityreduce|connectivitycheckpoint|connectivitycollect|connectivityjournal|connectivityruntime)$'
cloud_runtime_dependencies="$(go list -deps ./cmd/hexroute-ingest)"
if printf '%s\n' "$cloud_runtime_dependencies" | grep -Eq "$local_authority"; then
  printf 'cloud runtime imports local mutation or activation authority:\n' >&2
  printf '%s\n' "$cloud_runtime_dependencies" | grep -E "$local_authority" >&2
  exit 1
fi

# The cloud read model is downstream of the typed event schema and nothing
# else. If it could reach the reducer it would be able to recompute rather than
# store, and "the cloud renders what the host concluded" would stop being true.
projection_authority='(^|/)internal/(connectivityaccept|connectivityreduce|connectivitycheckpoint|connectivitycollect|connectivityjournal|connectivityruntime|connectivityview)$'
projection_dependencies="$(go list -deps ./internal/cloudconnectivity)"
if printf '%s\n' "$projection_dependencies" | grep -Eq "$projection_authority"; then
  printf 'the cloud read model imports local reduction authority:\n' >&2
  printf '%s\n' "$projection_dependencies" | grep -E "$projection_authority" >&2
  exit 1
fi

# A control endpoint is the thing this whole boundary exists to prevent. The
# cloud stores and renders; it never names an operation a host would perform.
if grep -RniE 'func .*(Enqueue|Dispatch|Request|Command|Execute|Apply|Mutate)[A-Za-z]*\(' \
  --include='*.go' internal/cloudconnectivity \
  | grep -viE '_test\.go' \
  | grep -q .; then
  printf 'the cloud read model exports something that reads as a control operation:\n' >&2
  grep -RniE 'func .*(Enqueue|Dispatch|Request|Command|Execute|Apply|Mutate)[A-Za-z]*\(' \
    --include='*.go' internal/cloudconnectivity | grep -viE '_test\.go' >&2
  exit 1
fi


# Every command is in one of the two lists. A binary in neither is one nobody
# decided the side of, and it is unguarded until someone does.
census_status=0
for directory in cmd/*/; do
  command_path="./${directory%/}"
  listed=0
  for known in "${local_binaries[@]}" "${cloud_binaries[@]}"; do
    [[ "$known" == "$command_path" ]] && listed=1 && break
  done
  if [[ "$listed" == "0" ]]; then
    printf '%s is on neither the local nor the cloud list\n' "$command_path" >&2
    printf '  nothing checks which authority it may reach until it is on one\n' >&2
    census_status=1
  fi
done

for known in "${local_binaries[@]}" "${cloud_binaries[@]}"; do
  [[ -d "${known#./}" ]] || {
    printf '%s is listed here and does not exist\n' "$known" >&2
    census_status=1
  }
done

[[ "$census_status" -eq 0 ]] || exit 1

printf 'ok: local policy paths are independent of cloud services and cloud runtime has no local mutation authority\n'
