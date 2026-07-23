#!/bin/bash

set -euo pipefail

umask 077

readonly ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
readonly TEMP="$(mktemp -d "${TMPDIR:-/tmp}/hexroute-emergency-test.XXXXXX")"
trap 'rm -rf "$TEMP"' EXIT

readonly SOURCE_RUNTIME="${TEMP}/source/supervisor"
readonly SOURCE_PLIST="${TEMP}/source/com.twilight.supervisor.plist"
readonly OUTPUT_ROOT="${TEMP}/packages"
readonly RESTORE_ROOT="${TEMP}/restored"
readonly SECRET_CANARY="HEXROUTE_FIXTURE_SECRET_MUST_NOT_BE_PRINTED"

mkdir -p "$SOURCE_RUNTIME/client"
printf 'fixture=true\nsecret=%s\n' "$SECRET_CANARY" >"$SOURCE_RUNTIME/.env"
printf '{"fixture":"sing-box"}\n' >"$SOURCE_RUNTIME/client/twilight-sing-box-tun.json"
printf '<plist><dict><key>Label</key><string>com.twilight.supervisor</string></dict></plist>\n' >"$SOURCE_PLIST"
chmod 600 "$SOURCE_RUNTIME/.env" "$SOURCE_RUNTIME/client/twilight-sing-box-tun.json" "$SOURCE_PLIST"

before_runtime="$(find "$SOURCE_RUNTIME" -type f -exec shasum -a 256 {} \; | LC_ALL=C sort)"
before_plist="$(shasum -a 256 "$SOURCE_PLIST")"

build_output="$(
  HEXROUTE_ALLOW_UNPRIVILEGED_BUILD=1 \
  HEXROUTE_TWILIGHT_RUNTIME="$SOURCE_RUNTIME" \
  HEXROUTE_TWILIGHT_PLIST="$SOURCE_PLIST" \
  HEXROUTE_EMERGENCY_ROOT="$OUTPUT_ROOT" \
  HEXROUTE_EMERGENCY_ID="fixture" \
    "$ROOT/scripts/baseline/build-emergency-package.sh"
)"

if [[ "$build_output" == *"$SECRET_CANARY"* ]]; then
  printf 'secret canary leaked from package builder\n' >&2
  exit 1
fi

package="$(printf '%s\n' "$build_output" | awk -F= '$1 == "package" { print substr($0, index($0, "=") + 1) }')"
[[ -n "$package" && -d "$package" ]]

"$package/bin/hexroute-emergency" verify --package "$package" >/dev/null
restore_output="$(
  "$package/bin/hexroute-emergency" restore-shell \
    --package "$package" \
    --destination "$RESTORE_ROOT"
)"

if [[ "$restore_output" == *"$SECRET_CANARY"* ]]; then
  printf 'secret canary leaked from emergency restore\n' >&2
  exit 1
fi

diff -rq "$SOURCE_RUNTIME" "$RESTORE_ROOT/Library/Application Support/twilight/supervisor"
cmp "$SOURCE_PLIST" "$RESTORE_ROOT/Library/LaunchDaemons/com.twilight.supervisor.plist"

after_runtime="$(find "$SOURCE_RUNTIME" -type f -exec shasum -a 256 {} \; | LC_ALL=C sort)"
after_plist="$(shasum -a 256 "$SOURCE_PLIST")"
[[ "$before_runtime" == "$after_runtime" ]]
[[ "$before_plist" == "$after_plist" ]]

printf 'ok: isolated emergency restore is exact and source remains unchanged\n'
