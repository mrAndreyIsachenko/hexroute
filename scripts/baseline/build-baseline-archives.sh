#!/bin/bash

set -euo pipefail

umask 077

readonly SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly RESTORE_SCRIPT="${SCRIPT_DIR}/hexroute-baseline-restore.sh"
readonly ALLOW_UNPRIVILEGED="${HEXROUTE_ALLOW_UNPRIVILEGED_BUILD:-0}"
readonly PACKAGE_ID="${HEXROUTE_BASELINE_ID:-twilight-baseline-$(date -u '+%Y%m%dT%H%M%SZ')}"

die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

if [[ "$EUID" -ne 0 && "$ALLOW_UNPRIVILEGED" != "1" ]]; then
  die "root is required to capture both ownership domains exactly"
fi

TARGET_USER="${HEXROUTE_TARGET_USER:-${SUDO_USER:-}}"
[[ -n "$TARGET_USER" && "$TARGET_USER" != "root" ]] || die "target user is required"
readonly TARGET_USER

TARGET_UID="$(id -u "$TARGET_USER")"
TARGET_GROUP="$(id -gn "$TARGET_USER")"
TARGET_HOME="${HEXROUTE_TARGET_HOME:-}"
if [[ -z "$TARGET_HOME" ]]; then
  TARGET_HOME="$(dscl . -read "/Users/${TARGET_USER}" NFSHomeDirectory | awk '{ print $2 }')"
fi
[[ -d "$TARGET_HOME" ]] || die "target user home is missing"
readonly TARGET_UID TARGET_GROUP TARGET_HOME

readonly ROOT_RUNTIME="${HEXROUTE_TWILIGHT_RUNTIME:-/Library/Application Support/twilight/supervisor}"
readonly ROOT_PLIST="${HEXROUTE_TWILIGHT_PLIST:-/Library/LaunchDaemons/com.twilight.supervisor.plist}"
readonly USER_RUNTIME="${HEXROUTE_TWILIGHT_USER_RUNTIME:-${TARGET_HOME}/Library/Application Support/twilight}"
readonly USER_OTP_PLIST="${HEXROUTE_TWILIGHT_OTP_PLIST:-${TARGET_HOME}/Library/LaunchAgents/com.twilight.pritunl-otp-watchdog.plist}"
readonly USER_CHATGPT_PLIST="${HEXROUTE_TWILIGHT_CHATGPT_PLIST:-${TARGET_HOME}/Library/LaunchAgents/com.twilight.chatgpt-proxy.plist}"
readonly ROOT_OUTPUT="${HEXROUTE_ROOT_BASELINE_ROOT:-/Library/Application Support/Hexroute/baseline/${PACKAGE_ID}}"
readonly USER_OUTPUT="${HEXROUTE_USER_BASELINE_ROOT:-${TARGET_HOME}/Library/Application Support/Hexroute/baseline/${PACKAGE_ID}}"

if [[ "$ALLOW_UNPRIVILEGED" == "1" ]]; then
  case "$ROOT_OUTPUT:$USER_OUTPUT" in
    /tmp/*:/tmp/*|"${TMPDIR:-/tmp}/"*:"${TMPDIR:-/tmp}/"*) ;;
    *) die "unprivileged fixture builds must stay below TMPDIR" ;;
  esac
fi

for source in "$ROOT_RUNTIME" "$ROOT_PLIST" "$USER_RUNTIME" "$USER_OTP_PLIST" "$USER_CHATGPT_PLIST"; do
  [[ -e "$source" ]] || die "required baseline source is missing"
done
[[ -x "$RESTORE_SCRIPT" ]] || die "baseline restore script is missing or not executable"
[[ ! -e "$ROOT_OUTPUT" && ! -e "$USER_OUTPUT" ]] || die "baseline package id already exists"

readonly TEMP="$(mktemp -d "${TMPDIR:-/tmp}/hexroute-baseline-build.XXXXXX")"
trap 'rm -rf "$TEMP"' EXIT
readonly ROOT_STAGE="${TEMP}/root"
readonly USER_STAGE="${TEMP}/user"

install -d -m 700 "$ROOT_STAGE/bin" "$ROOT_STAGE/payload" "$USER_STAGE/bin" "$USER_STAGE/payload"
install -m 700 "$RESTORE_SCRIPT" "$ROOT_STAGE/bin/hexroute-baseline-restore"
install -m 700 "$RESTORE_SCRIPT" "$USER_STAGE/bin/hexroute-baseline-restore"

mirror_directory_metadata() {
  local source="$1"
  local destination="$2"
  local mode uid gid

  [[ -d "$source" ]] || die "source parent directory is missing"
  mkdir -p "$destination"
  mode="$(stat -f '%Lp' "$source")"
  uid="$(stat -f '%u' "$source")"
  gid="$(stat -f '%g' "$source")"
  chmod "$mode" "$destination"

  if [[ "$EUID" -eq 0 ]]; then
    chown "${uid}:${gid}" "$destination"
  elif [[ "$uid" != "$EUID" ]]; then
    die "unprivileged fixture parent owner differs from the current user"
  fi
}

copy_exact() {
  local source="$1"
  local destination="$2"

  mkdir -p "$(dirname "$destination")"
  if [[ -d "$source" && ! -L "$source" ]]; then
    ditto --norsrc --noextattr --noqtn --noacl --nopersistRootless "$source" "$destination"
  else
    cp -p "$source" "$destination"
  fi
}

readonly ROOT_TWILIGHT_DIR="$(dirname "$ROOT_RUNTIME")"
readonly ROOT_APPLICATION_SUPPORT="$(dirname "$ROOT_TWILIGHT_DIR")"
readonly ROOT_LIBRARY="$(dirname "$ROOT_APPLICATION_SUPPORT")"
readonly ROOT_LAUNCH_DAEMONS="$(dirname "$ROOT_PLIST")"
readonly USER_APPLICATION_SUPPORT="$(dirname "$USER_RUNTIME")"
readonly USER_LIBRARY="$(dirname "$USER_APPLICATION_SUPPORT")"
readonly USER_HOME_SOURCE="$(dirname "$USER_LIBRARY")"
readonly USERS_SOURCE="$(dirname "$USER_HOME_SOURCE")"
readonly USER_LAUNCH_AGENTS="$(dirname "$USER_OTP_PLIST")"

mirror_directory_metadata "$ROOT_LIBRARY" "$ROOT_STAGE/payload/Library"
mirror_directory_metadata "$ROOT_APPLICATION_SUPPORT" "$ROOT_STAGE/payload/Library/Application Support"
mirror_directory_metadata "$ROOT_TWILIGHT_DIR" "$ROOT_STAGE/payload/Library/Application Support/twilight"
mirror_directory_metadata "$ROOT_LAUNCH_DAEMONS" "$ROOT_STAGE/payload/Library/LaunchDaemons"
mirror_directory_metadata "$USERS_SOURCE" "$USER_STAGE/payload/Users"
mirror_directory_metadata "$USER_HOME_SOURCE" "$USER_STAGE/payload/Users/${TARGET_USER}"
mirror_directory_metadata "$USER_LIBRARY" "$USER_STAGE/payload/Users/${TARGET_USER}/Library"
mirror_directory_metadata "$USER_APPLICATION_SUPPORT" "$USER_STAGE/payload/Users/${TARGET_USER}/Library/Application Support"
mirror_directory_metadata "$USER_LAUNCH_AGENTS" "$USER_STAGE/payload/Users/${TARGET_USER}/Library/LaunchAgents"

copy_exact "$ROOT_RUNTIME" "$ROOT_STAGE/payload/Library/Application Support/twilight/supervisor"
copy_exact "$ROOT_PLIST" "$ROOT_STAGE/payload/Library/LaunchDaemons/com.twilight.supervisor.plist"
copy_exact "$USER_RUNTIME" "$USER_STAGE/payload/Users/${TARGET_USER}/Library/Application Support/twilight"
copy_exact "$USER_OTP_PLIST" "$USER_STAGE/payload/Users/${TARGET_USER}/Library/LaunchAgents/com.twilight.pritunl-otp-watchdog.plist"
copy_exact "$USER_CHATGPT_PLIST" "$USER_STAGE/payload/Users/${TARGET_USER}/Library/LaunchAgents/com.twilight.chatgpt-proxy.plist"

cat >"$ROOT_STAGE/metadata.tsv" <<EOF
schema	hexroute.twilight-baseline.v1
scope	root
launchd_label	system/com.twilight.supervisor
installed_path	/Library/Application Support/twilight/supervisor
installed_path	/Library/LaunchDaemons/com.twilight.supervisor.plist
keychain_export	none
EOF

cat >"$USER_STAGE/metadata.tsv" <<EOF
schema	hexroute.twilight-baseline.v1
scope	user
launchd_label	gui/${TARGET_UID}/com.twilight.pritunl-otp-watchdog
launchd_label	gui/${TARGET_UID}/com.twilight.chatgpt-proxy
installed_path	${TARGET_HOME}/Library/Application Support/twilight
installed_path	${TARGET_HOME}/Library/LaunchAgents/com.twilight.pritunl-otp-watchdog.plist
installed_path	${TARGET_HOME}/Library/LaunchAgents/com.twilight.chatgpt-proxy.plist
keychain_export	none
EOF

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

write_manifest() {
  local package="$1"

  while IFS= read -r path; do
    manifest_entry "$package/payload" "$path"
  done < <(find "$package/payload" -mindepth 1 -print | LC_ALL=C sort) >"$package/manifest.tsv"

  (
    cd "$package"
    shasum -a 256 manifest.tsv >manifest.tsv.sha256
  )
  chmod 600 "$package/metadata.tsv" "$package/manifest.tsv" "$package/manifest.tsv.sha256"
}

write_manifest "$ROOT_STAGE"
write_manifest "$USER_STAGE"

"$ROOT_STAGE/bin/hexroute-baseline-restore" verify --package "$ROOT_STAGE" >/dev/null
"$USER_STAGE/bin/hexroute-baseline-restore" verify --package "$USER_STAGE" >/dev/null

install_package() {
  local stage="$1"
  local output="$2"
  local name="$3"
  local owner="$4"
  local group="$5"
  local package="${output}/${name}"
  local archive="${output}/${name}.tar"
  local checksum="${archive}.sha256"
  local validation="${TEMP}/validate-${name}"
  local restored="${TEMP}/restore-${name}"

  install -d -m 700 "$output"
  mv "$stage" "$package"

  if [[ "$EUID" -eq 0 ]]; then
    chown "${owner}:${group}" \
      "$output" \
      "$package" \
      "$package/bin" \
      "$package/bin/hexroute-baseline-restore" \
      "$package/payload" \
      "$package/metadata.tsv" \
      "$package/manifest.tsv" \
      "$package/manifest.tsv.sha256"
  fi

  "$package/bin/hexroute-baseline-restore" verify --package "$package" >/dev/null
  tar -cpf "$archive" -C "$package" .
  shasum -a 256 "$archive" >"$checksum"
  chmod 600 "$archive" "$checksum"
  if [[ "$EUID" -eq 0 ]]; then
    chown "${owner}:${group}" "$archive" "$checksum"
  fi

  install -d -m 700 "$validation"
  tar -xpf "$archive" -C "$validation"
  "$validation/bin/hexroute-baseline-restore" verify --package "$validation" >/dev/null
  "$validation/bin/hexroute-baseline-restore" restore \
    --package "$validation" \
    --destination "$restored" >/dev/null

  "$package/bin/hexroute-baseline-restore" verify --package "$package" >/dev/null
  shasum -a 256 -c "$checksum" >/dev/null

  printf '%s_package=%s\n' "$name" "$package"
  printf '%s_archive=%s\n' "$name" "$archive"
  printf '%s_checksum=%s\n' "$name" "$checksum"
}

install_package "$ROOT_STAGE" "$ROOT_OUTPUT" "root" "root" "wheel"
install_package "$USER_STAGE" "$USER_OUTPUT" "user" "$TARGET_USER" "$TARGET_GROUP"
