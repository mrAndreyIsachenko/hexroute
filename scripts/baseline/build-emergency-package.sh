#!/bin/bash

set -euo pipefail

umask 077

readonly SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly EMERGENCY_SCRIPT="${SCRIPT_DIR}/hexroute-emergency.sh"
readonly SOURCE_RUNTIME="${HEXROUTE_TWILIGHT_RUNTIME:-/Library/Application Support/twilight/supervisor}"
readonly SOURCE_PLIST="${HEXROUTE_TWILIGHT_PLIST:-/Library/LaunchDaemons/com.twilight.supervisor.plist}"
readonly OUTPUT_ROOT="${HEXROUTE_EMERGENCY_ROOT:-/Library/Application Support/Hexroute/emergency}"
readonly PACKAGE_ID="${HEXROUTE_EMERGENCY_ID:-twilight-shell-$(date -u '+%Y%m%dT%H%M%SZ')}"
readonly ALLOW_UNPRIVILEGED="${HEXROUTE_ALLOW_UNPRIVILEGED_BUILD:-0}"

die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

if [[ "$EUID" -ne 0 && "$ALLOW_UNPRIVILEGED" != "1" ]]; then
  die "root is required to capture the installed supervisor exactly"
fi
if [[ "$ALLOW_UNPRIVILEGED" == "1" && "$OUTPUT_ROOT" != /tmp/* && "$OUTPUT_ROOT" != "${TMPDIR:-/tmp}/"* ]]; then
  die "unprivileged fixture builds must stay below TMPDIR"
fi

[[ -d "$SOURCE_RUNTIME" ]] || die "runtime source is missing"
[[ -f "$SOURCE_PLIST" ]] || die "launchd plist source is missing"
[[ -x "$EMERGENCY_SCRIPT" ]] || die "emergency restore script is missing or not executable"

install -d -m 700 "$OUTPUT_ROOT"

readonly STAGING="${OUTPUT_ROOT}/.staging-${PACKAGE_ID}-$$"
readonly PACKAGE="${OUTPUT_ROOT}/${PACKAGE_ID}"
readonly ARCHIVE="${OUTPUT_ROOT}/${PACKAGE_ID}.tar"
readonly ARCHIVE_CHECKSUM="${ARCHIVE}.sha256"

[[ ! -e "$PACKAGE" && ! -e "$ARCHIVE" ]] || die "package id already exists"

cleanup() {
  rm -rf "$STAGING"
}
trap cleanup EXIT

install -d -m 700 \
  "$STAGING/bin" \
  "$STAGING/payload/Library/Application Support/twilight" \
  "$STAGING/payload/Library/LaunchDaemons"

ditto --norsrc --noextattr --noqtn --noacl --nopersistRootless \
  "$SOURCE_RUNTIME" \
  "$STAGING/payload/Library/Application Support/twilight/supervisor"
cp -p "$SOURCE_PLIST" "$STAGING/payload/Library/LaunchDaemons/com.twilight.supervisor.plist"
install -m 700 "$EMERGENCY_SCRIPT" "$STAGING/bin/hexroute-emergency"

manifest_entry() {
  local root="$1"
  local path="$2"
  local relative="${path#"$root"/}"
  local kind mode uid gid size hash

  mode="$(stat -f '%Sp' "$path")"
  uid="$(stat -f '%u' "$path")"
  gid="$(stat -f '%g' "$path")"

  if [[ -L "$path" ]]; then
    kind="symlink"
    size="0"
    hash="$(readlink "$path" | shasum -a 256 | awk '{ print $1 }')"
  elif [[ -d "$path" ]]; then
    kind="directory"
    size="0"
    hash="-"
  elif [[ -f "$path" ]]; then
    kind="file"
    size="$(stat -f '%z' "$path")"
    hash="$(shasum -a 256 "$path" | awk '{ print $1 }')"
  else
    die "unsupported payload entry type"
  fi

  printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
    "$kind" "$mode" "$uid" "$gid" "$size" "$hash" "$relative"
}

while IFS= read -r path; do
  manifest_entry "$STAGING/payload" "$path"
done < <(find "$STAGING/payload" -mindepth 1 -print | LC_ALL=C sort) >"$STAGING/manifest.tsv"

(
  cd "$STAGING"
  shasum -a 256 manifest.tsv >manifest.tsv.sha256
)
chmod 600 "$STAGING/manifest.tsv" "$STAGING/manifest.tsv.sha256"

"$STAGING/bin/hexroute-emergency" verify --package "$STAGING" >/dev/null

mv "$STAGING" "$PACKAGE"
trap - EXIT

tar -cpf "${ARCHIVE}.tmp" -C "$PACKAGE" .
mv "${ARCHIVE}.tmp" "$ARCHIVE"
shasum -a 256 "$ARCHIVE" >"$ARCHIVE_CHECKSUM"
chmod 600 "$ARCHIVE" "$ARCHIVE_CHECKSUM"

printf 'package=%s\n' "$PACKAGE"
printf 'archive=%s\n' "$ARCHIVE"
printf 'checksum=%s\n' "$ARCHIVE_CHECKSUM"
