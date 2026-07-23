#!/bin/bash

set -euo pipefail

umask 077

readonly SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly HEXROUTE_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
readonly DEFAULT_TWILIGHT_ROOT="$(cd "${HEXROUTE_ROOT}/.." && pwd)/twilight"
readonly TWILIGHT_ROOT="${HEXROUTE_TWILIGHT_ROOT:-$DEFAULT_TWILIGHT_ROOT}"
readonly OUTPUT_DIR="${HEXROUTE_BASELINE_DIR:-${HOME}/Library/Application Support/Hexroute/baseline}"
readonly TIMESTAMP="$(date -u '+%Y%m%dT%H%M%SZ')"
readonly OUTPUT="${1:-${OUTPUT_DIR}/live-access-${TIMESTAMP}.txt}"
readonly CHECKSUM="${OUTPUT}.sha256"

[[ -d "$TWILIGHT_ROOT" ]] || {
  printf 'error: Twilight checkout is missing\n' >&2
  exit 1
}

if [[ -f "$TWILIGHT_ROOT/.env" ]]; then
  set -a
  # shellcheck disable=SC1091
  . "$TWILIGHT_ROOT/.env"
  set +a
fi

readonly PRITUNL_ENDPOINT="${TWILIGHT_PRITUNL_ENDPOINT:-${TWILIGHT_PRITUNL_ENDPOINTS:-}}"
readonly PRITUNL_TARGET="${PRITUNL_ENDPOINT%% *}"
readonly GITLAB_TARGET="${GITLAB_HOST:-gitlab.invalid}"
readonly TEMP_OUTPUT="$(mktemp "${TMPDIR:-/tmp}/hexroute-live-access.XXXXXX")"
status=0

cleanup() {
  rm -f "$TEMP_OUTPUT"
}
trap cleanup EXIT

run_check() {
  local name="$1"
  shift

  printf '\n== %s ==\n' "$name"
  if "$@"; then
    printf 'result=ok\n'
  else
    printf 'result=fail\n'
    status=1
  fi
}

codex_probe() {
  local output

  output="$("$TWILIGHT_ROOT/scripts/twilight-codex-fallback.sh" probe 2>&1)"
  printf '%s\n' "$output"
  [[ "$output" == *"normal=ok"* && "$output" == *"twilight=ok"* ]]
}

http_probe() {
  local name="$1"
  local url="$2"
  local code rc=0

  code="$(
    curl -4 -k -sS \
      --connect-timeout 5 \
      --max-time 10 \
      -o /dev/null \
      -w '%{http_code}' \
      "$url" 2>/dev/null
  )" || rc=$?
  printf 'probe=%s curl_rc=%s http=%s\n' "$name" "$rc" "${code:-000}"
  [[ "$rc" == "0" && -n "$code" && "$code" != "000" ]]
}

adguard_state() {
  local result=0

  if pgrep -x adguard-tcpkill >/dev/null 2>&1; then
    printf 'adguard_tcpkill=running\n'
    result=1
  else
    printf 'adguard_tcpkill=not_running\n'
  fi

  if pgrep -if 'adguard|com\.adguard' >/dev/null 2>&1; then
    printf 'adguard_process=present\n'
  else
    printf 'adguard_process=not_observed\n'
  fi
  return "$result"
}

mkdir -p "$(dirname "$OUTPUT")"
chmod 700 "$(dirname "$OUTPUT")"

{
  printf 'schema=hexroute.live-access-baseline.v1\n'
  printf 'collected_at_utc=%s\n' "$TIMESTAMP"
  printf 'mutation_authority=none\n'

  run_check "normal and Twilight Codex" codex_probe
  run_check "Twilight runtime and ingress routes" "$TWILIGHT_ROOT/scripts/twilight-check.sh"
  run_check "Twilight scoped routes" "$TWILIGHT_ROOT/scripts/twilight-routes.sh" status

  if [[ -n "$PRITUNL_TARGET" ]]; then
    run_check "Pritunl outer TLS" http_probe "pritunl_outer_tls" "https://${PRITUNL_TARGET}/"
  else
    printf '\n== Pritunl outer TLS ==\nresult=fail\nreason=endpoint_not_configured\n'
    status=1
  fi

  run_check "GitLab HTTPS" http_probe "gitlab_https" "https://${GITLAB_TARGET}/"
  run_check "GitLab SSH and route policy" "$TWILIGHT_ROOT/scripts/gitlab-git.sh" health
  run_check "AdGuard coexistence state" adguard_state

  printf '\n== summary ==\n'
  if [[ "$status" == "0" ]]; then
    printf 'overall=ok\n'
  else
    printf 'overall=fail\n'
  fi
} >"$TEMP_OUTPUT" 2>&1

chmod 600 "$TEMP_OUTPUT"
mv "$TEMP_OUTPUT" "$OUTPUT"
trap - EXIT
shasum -a 256 "$OUTPUT" >"$CHECKSUM"
chmod 600 "$OUTPUT" "$CHECKSUM"

printf 'snapshot=%s\n' "$OUTPUT"
printf 'checksum=%s\n' "$CHECKSUM"
exit "$status"
