#!/bin/bash

set -euo pipefail

umask 077

die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

usage() {
  cat <<'EOF'
usage:
  hexroute-emergency verify [--package DIRECTORY]
  hexroute-emergency status [--package DIRECTORY]
  hexroute-emergency restore-shell --destination DIRECTORY [--package DIRECTORY]
  hexroute-emergency restore-shell --live --confirm RESTORE_TWILIGHT_SHELL [--package DIRECTORY]
EOF
}

readonly SELF_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEFAULT_PACKAGE="$(cd "${SELF_DIR}/.." && pwd)"
PACKAGE="$DEFAULT_PACKAGE"
COMMAND="${1:-}"
shift || true

DESTINATION=""
LIVE=0
CONFIRMATION=""

while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --package)
      [[ "$#" -ge 2 ]] || die "--package requires a directory"
      PACKAGE="$2"
      shift 2
      ;;
    --destination)
      [[ "$#" -ge 2 ]] || die "--destination requires a directory"
      DESTINATION="$2"
      shift 2
      ;;
    --live)
      LIVE=1
      DESTINATION="/"
      shift
      ;;
    --confirm)
      [[ "$#" -ge 2 ]] || die "--confirm requires a value"
      CONFIRMATION="$2"
      shift 2
      ;;
    *)
      usage >&2
      die "unknown argument"
      ;;
  esac
done

readonly MANIFEST="${PACKAGE}/manifest.tsv"
readonly MANIFEST_CHECKSUM="${PACKAGE}/manifest.tsv.sha256"
readonly PAYLOAD="${PACKAGE}/payload"

verify_manifest_checksum() {
  [[ -f "$MANIFEST" && -f "$MANIFEST_CHECKSUM" ]] || die "package manifest is missing"
  (
    cd "$PACKAGE"
    shasum -a 256 -c "$(basename "$MANIFEST_CHECKSUM")" >/dev/null
  ) || die "package manifest checksum failed"
}

verify_tree() {
  local root="$1"
  local kind mode uid gid size hash relative path actual

  while IFS=$'\t' read -r kind mode uid gid size hash relative; do
    [[ -n "$relative" ]] || die "manifest contains an empty path"
    path="${root}/${relative}"

    case "$kind" in
      directory)
        [[ -d "$path" && ! -L "$path" ]] || die "directory is missing"
        ;;
      file)
        [[ -f "$path" && ! -L "$path" ]] || die "file is missing"
        [[ "$(stat -f '%z' "$path")" == "$size" ]] || die "file size mismatch"
        actual="$(shasum -a 256 "$path" | awk '{ print $1 }')"
        [[ "$actual" == "$hash" ]] || die "file checksum mismatch"
        ;;
      symlink)
        [[ -L "$path" ]] || die "symlink is missing"
        actual="$(readlink "$path" | shasum -a 256 | awk '{ print $1 }')"
        [[ "$actual" == "$hash" ]] || die "symlink checksum mismatch"
        ;;
      *)
        die "manifest contains an unsupported entry type"
        ;;
    esac

    [[ "$(stat -f '%Sp' "$path")" == "$mode" ]] || die "mode mismatch"
    [[ "$(stat -f '%u' "$path")" == "$uid" ]] || die "owner mismatch"
    [[ "$(stat -f '%g' "$path")" == "$gid" ]] || die "group mismatch"
  done <"$MANIFEST"
}

verify_package() {
  verify_manifest_checksum
  verify_tree "$PAYLOAD"
}

restore_payload() {
  local destination="$1"

  [[ -d "$PAYLOAD" ]] || die "package payload is missing"
  install -d -m 700 "$destination"
  ditto --norsrc --noextattr --noqtn --noacl --nopersistRootless "$PAYLOAD" "$destination"
  verify_tree "$destination"
}

case "$COMMAND" in
  verify)
    verify_package
    printf 'ok: emergency package verified\n'
    ;;
  status)
    verify_package
    printf 'package=%s\n' "$PACKAGE"
    printf 'payload=verified\n'
    printf 'live_restore=explicit_only\n'
    ;;
  restore-shell)
    verify_package
    [[ -n "$DESTINATION" ]] || die "restore requires --destination or --live"

    if [[ "$LIVE" == "1" ]]; then
      [[ "$EUID" -eq 0 ]] || die "live restore requires root"
      [[ "$CONFIRMATION" == "RESTORE_TWILIGHT_SHELL" ]] || die "live restore confirmation is missing"
      restore_payload "/"
      launchctl bootout system/com.twilight.supervisor 2>/dev/null || true
      launchctl bootstrap system /Library/LaunchDaemons/com.twilight.supervisor.plist
      launchctl kickstart -k system/com.twilight.supervisor
      printf 'ok: live Twilight shell supervisor restored and started\n'
    else
      [[ "$DESTINATION" != "/" ]] || die "use --live for the system root"
      restore_payload "$DESTINATION"
      printf 'ok: isolated Twilight shell payload restored\n'
    fi
    ;;
  *)
    usage >&2
    exit 2
    ;;
esac
