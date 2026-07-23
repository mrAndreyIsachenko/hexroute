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
  hexroute-baseline-restore verify [--package DIRECTORY]
  hexroute-baseline-restore restore --destination DIRECTORY [--package DIRECTORY]
EOF
}

readonly SELF_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEFAULT_PACKAGE="$(cd "${SELF_DIR}/.." && pwd)"
PACKAGE="$DEFAULT_PACKAGE"
COMMAND="${1:-}"
shift || true
DESTINATION=""

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
  [[ -f "${PACKAGE}/metadata.tsv" ]] || die "package metadata is missing"
  verify_tree "$PAYLOAD"
}

case "$COMMAND" in
  verify)
    verify_package
    printf 'ok: baseline package verified\n'
    ;;
  restore)
    verify_package
    [[ -n "$DESTINATION" ]] || die "restore requires --destination"
    [[ "$DESTINATION" != "/" ]] || die "baseline validation cannot restore to the system root"
    install -d -m 700 "$DESTINATION"
    ditto --norsrc --noextattr --noqtn --noacl --nopersistRootless "$PAYLOAD" "$DESTINATION"
    verify_tree "$DESTINATION"
    printf 'ok: isolated baseline payload restored\n'
    ;;
  *)
    usage >&2
    exit 2
    ;;
esac
