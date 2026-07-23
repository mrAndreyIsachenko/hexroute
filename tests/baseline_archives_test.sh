#!/bin/bash

set -euo pipefail

umask 077

readonly ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
readonly TEMP="$(mktemp -d "${TMPDIR:-/tmp}/hexroute-baseline-test.XXXXXX")"
trap 'rm -rf "$TEMP"' EXIT

readonly TARGET_USER="$(id -un)"
readonly SOURCE_ROOT="${TEMP}/installed"
readonly TARGET_HOME="${SOURCE_ROOT}/Users/${TARGET_USER}"
readonly ROOT_RUNTIME="${SOURCE_ROOT}/Library/Application Support/twilight/supervisor"
readonly ROOT_PLIST="${SOURCE_ROOT}/Library/LaunchDaemons/com.twilight.supervisor.plist"
readonly USER_RUNTIME="${TARGET_HOME}/Library/Application Support/twilight"
readonly USER_OTP_PLIST="${TARGET_HOME}/Library/LaunchAgents/com.twilight.pritunl-otp-watchdog.plist"
readonly USER_CHATGPT_PLIST="${TARGET_HOME}/Library/LaunchAgents/com.twilight.chatgpt-proxy.plist"
readonly ROOT_OUTPUT="${TEMP}/output/root"
readonly USER_OUTPUT="${TEMP}/output/user"
readonly SECRET_CANARY="HEXROUTE_BASELINE_FIXTURE_SECRET_MUST_NOT_LEAK"

mkdir -p \
  "$ROOT_RUNTIME/client" \
  "$(dirname "$ROOT_PLIST")" \
  "$USER_RUNTIME/otp-watchdog" \
  "$USER_RUNTIME/chatgpt-proxy" \
  "$(dirname "$USER_OTP_PLIST")"
printf 'secret=%s\n' "$SECRET_CANARY" >"$ROOT_RUNTIME/.env"
printf '{"fixture":"root"}\n' >"$ROOT_RUNTIME/client/twilight-sing-box-tun.json"
printf '<plist><string>root</string></plist>\n' >"$ROOT_PLIST"
printf 'profile=synthetic\nsecret=%s\n' "$SECRET_CANARY" >"$USER_RUNTIME/otp-watchdog/.env"
printf 'proxy=synthetic\n' >"$USER_RUNTIME/chatgpt-proxy/.env"
printf '<plist><string>otp</string></plist>\n' >"$USER_OTP_PLIST"
printf '<plist><string>chatgpt</string></plist>\n' >"$USER_CHATGPT_PLIST"
chmod 600 \
  "$ROOT_RUNTIME/.env" \
  "$ROOT_RUNTIME/client/twilight-sing-box-tun.json" \
  "$ROOT_PLIST" \
  "$USER_RUNTIME/otp-watchdog/.env" \
  "$USER_RUNTIME/chatgpt-proxy/.env" \
  "$USER_OTP_PLIST" \
  "$USER_CHATGPT_PLIST"

before="$(
  find "$SOURCE_ROOT" -type f -exec shasum -a 256 {} \; |
    LC_ALL=C sort
)"

output="$(
  HEXROUTE_ALLOW_UNPRIVILEGED_BUILD=1 \
  HEXROUTE_TARGET_USER="$TARGET_USER" \
  HEXROUTE_TARGET_HOME="$TARGET_HOME" \
  HEXROUTE_TWILIGHT_RUNTIME="$ROOT_RUNTIME" \
  HEXROUTE_TWILIGHT_PLIST="$ROOT_PLIST" \
  HEXROUTE_TWILIGHT_USER_RUNTIME="$USER_RUNTIME" \
  HEXROUTE_TWILIGHT_OTP_PLIST="$USER_OTP_PLIST" \
  HEXROUTE_TWILIGHT_CHATGPT_PLIST="$USER_CHATGPT_PLIST" \
  HEXROUTE_ROOT_BASELINE_ROOT="$ROOT_OUTPUT" \
  HEXROUTE_USER_BASELINE_ROOT="$USER_OUTPUT" \
  HEXROUTE_BASELINE_ID="fixture" \
    "$ROOT/scripts/baseline/build-baseline-archives.sh"
)"

if [[ "$output" == *"$SECRET_CANARY"* ]]; then
  printf 'secret canary leaked from baseline builder\n' >&2
  exit 1
fi

root_archive="$(printf '%s\n' "$output" | awk -F= '$1 == "root_archive" { print substr($0, index($0, "=") + 1) }')"
user_archive="$(printf '%s\n' "$output" | awk -F= '$1 == "user_archive" { print substr($0, index($0, "=") + 1) }')"

[[ -f "$root_archive" && -f "${root_archive}.sha256" ]]
[[ -f "$user_archive" && -f "${user_archive}.sha256" ]]
shasum -a 256 -c "${root_archive}.sha256" >/dev/null
shasum -a 256 -c "${user_archive}.sha256" >/dev/null

grep -q $'launchd_label\tsystem/com.twilight.supervisor' "$ROOT_OUTPUT/root/metadata.tsv"
grep -q $'launchd_label\tgui/' "$USER_OUTPUT/user/metadata.tsv"
grep -q $'keychain_export\tnone' "$ROOT_OUTPUT/root/metadata.tsv"
grep -q $'keychain_export\tnone' "$USER_OUTPUT/user/metadata.tsv"

after="$(
  find "$SOURCE_ROOT" -type f -exec shasum -a 256 {} \; |
    LC_ALL=C sort
)"
[[ "$before" == "$after" ]]

if grep -Eq '(^|[[:space:]])(launchctl|security)([[:space:]]|$)' \
  "$ROOT/scripts/baseline/build-baseline-archives.sh"; then
  printf 'baseline builder contains a forbidden runtime or Keychain command\n' >&2
  exit 1
fi

printf 'ok: root/user baseline archives are exact, isolated and secret-safe\n'
