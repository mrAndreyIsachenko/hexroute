#!/bin/bash

set -euo pipefail

umask 077

readonly USER_UID="$(id -u)"
readonly DEFAULT_OUTPUT_DIR="${HOME}/Library/Application Support/Hexroute/baseline"
readonly OUTPUT_DIR="${HEXROUTE_BASELINE_DIR:-$DEFAULT_OUTPUT_DIR}"
readonly TIMESTAMP="$(date -u '+%Y%m%dT%H%M%SZ')"
readonly OUTPUT="${1:-${OUTPUT_DIR}/twilight-inventory-${TIMESTAMP}.txt}"
readonly CHECKSUM="${OUTPUT}.sha256"

readonly -a LAUNCHD_SERVICES=(
  "system/com.twilight.supervisor"
  "gui/${USER_UID}/com.twilight.pritunl-otp-watchdog"
  "gui/${USER_UID}/com.twilight.chatgpt-proxy"
)

readonly -a RUNTIME_PATHS=(
  "/Library/Application Support/twilight/supervisor"
  "/Library/LaunchDaemons/com.twilight.supervisor.plist"
  "${HOME}/Library/Application Support/twilight/otp-watchdog"
  "${HOME}/Library/LaunchAgents/com.twilight.pritunl-otp-watchdog.plist"
  "${HOME}/Library/LaunchAgents/com.twilight.chatgpt-proxy.plist"
)

mkdir -p "$(dirname "$OUTPUT")"
chmod 700 "$(dirname "$OUTPUT")"

readonly TEMP_OUTPUT="$(mktemp "${TMPDIR:-/tmp}/hexroute-inventory.XXXXXX")"
trap 'rm -f "$TEMP_OUTPUT"' EXIT

record_command_version() {
  local name="$1"
  shift

  printf '%-20s ' "$name"
  if command -v "$1" >/dev/null 2>&1; then
    "$@" 2>&1 | head -n 1 || printf 'version unavailable\n'
  else
    printf 'not installed\n'
  fi
}

record_launchd_service() {
  local service="$1"

  printf '\n[%s]\n' "$service"
  if ! launchctl print "$service" 2>/dev/null |
    awk '
      NR == 1 ||
      /^[[:space:]]*(path|state|program|stdout path|stderr path|pid|runs|last exit code) =/ ||
      /^[[:space:]]*properties =/
    '; then
    printf 'not loaded or not readable\n'
  fi
}

record_runtime_path() {
  local path="$1"

  if [[ ! -e "$path" ]]; then
    printf 'missing\t%s\n' "$path"
    return
  fi

  if [[ -f "$path" ]]; then
    stat -f 'file	mode=%Sp	uid=%u	gid=%g	size=%z	mtime=%m	%N' "$path"
    if [[ -r "$path" ]]; then
      shasum -a 256 "$path" | awk '{ print "sha256\t" $1 }'
    else
      printf 'sha256\tunreadable\n'
    fi
    return
  fi

  stat -f 'directory	mode=%Sp	uid=%u	gid=%g	mtime=%m	%N' "$path"
  find -x "$path" -type f -print 2>/dev/null |
    LC_ALL=C sort |
    while IFS= read -r file; do
      stat -f 'file	mode=%Sp	uid=%u	gid=%g	size=%z	mtime=%m	%N' "$file"
      if [[ -r "$file" ]]; then
        shasum -a 256 "$file" | awk '{ print "sha256\t" $1 }'
      else
        printf 'sha256\tunreadable\n'
      fi
    done
}

{
  printf 'schema=hexroute.twilight-inventory.v1\n'
  printf 'collected_at_utc=%s\n' "$TIMESTAMP"
  printf 'collector_uid=%s\n' "$USER_UID"
  printf 'mutation_authority=none\n'

  printf '\n== platform ==\n'
  sw_vers
  uname -mrs

  printf '\n== versions ==\n'
  record_command_version "sing-box" sing-box version
  record_command_version "pritunl-client" pritunl-client --version
  record_command_version "bash" bash --version
  record_command_version "go" go version

  printf '\n== launchd ==\n'
  for service in "${LAUNCHD_SERVICES[@]}"; do
    record_launchd_service "$service"
  done

  printf '\n== relevant processes (arguments intentionally omitted) ==\n'
  ps -axo pid=,ppid=,uid=,comm= |
    awk 'BEGIN { IGNORECASE = 1 }
      /twilight|sing-box|pritunl|caffeinate|adguard|chatgpt/ { print }'

  printf '\n== ipv4 routes (private evidence; never commit) ==\n'
  netstat -rn -f inet

  printf '\n== network interfaces ==\n'
  ifconfig -l

  printf '\n== installed runtime inventory ==\n'
  for path in "${RUNTIME_PATHS[@]}"; do
    record_runtime_path "$path"
  done
} >"$TEMP_OUTPUT"

chmod 600 "$TEMP_OUTPUT"
mv "$TEMP_OUTPUT" "$OUTPUT"
shasum -a 256 "$OUTPUT" >"$CHECKSUM"
chmod 600 "$OUTPUT" "$CHECKSUM"

printf 'inventory=%s\n' "$OUTPUT"
printf 'checksum=%s\n' "$CHECKSUM"
