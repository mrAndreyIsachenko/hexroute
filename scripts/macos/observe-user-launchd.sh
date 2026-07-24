#!/usr/bin/env bash
set -euo pipefail

LABEL="com.hexroute.observe.userd"
UID_VALUE="$(id -u)"
DOMAIN="gui/$UID_VALUE"
ROOT_DIR="$HOME/Library/Application Support/Hexroute/observe-user"
BIN_DIR="$ROOT_DIR/bin"
CONFIG_DIR="$ROOT_DIR/config"
STATE_DIR="$ROOT_DIR/state"
LOG_DIR="$HOME/Library/Logs/Hexroute/observe-user"
PLIST_SOURCE="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)/deploy/macos/$LABEL.plist"
PLIST_DEST="$HOME/Library/LaunchAgents/$LABEL.plist"

die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

require_user() {
  [[ "$UID_VALUE" != "0" ]] || die "run as the target login user, not root"
}

require_regular_file() {
  [[ -f "$1" && ! -L "$1" ]] || die "regular non-symlink file required"
}

set_plist_string() {
  local key="${1//./:}"
  /usr/libexec/PlistBuddy -c "Set :$key $2" "$PLIST_DEST"
}

install_observer() {
  require_user
  local binary="${1:-}"
  local config="${2:-}"
  [[ -n "$binary" && -n "$config" ]] || die "usage: $0 install BINARY PRIVATE_CONFIG"
  require_regular_file "$binary"
  require_regular_file "$config"
  require_regular_file "$PLIST_SOURCE"

  /usr/bin/install -d -m 0700 \
    "$ROOT_DIR" "$BIN_DIR" "$CONFIG_DIR" "$STATE_DIR" "$LOG_DIR"
  if [[ ! -d "$HOME/Library/LaunchAgents" ]]; then
    /usr/bin/install -d -m 0755 "$HOME/Library/LaunchAgents"
  fi
  /usr/bin/install -m 0755 "$binary" "$BIN_DIR/hexroute-userd"
  /usr/bin/install -m 0600 "$config" "$CONFIG_DIR/user-observe.json"
  /usr/bin/install -m 0644 "$PLIST_SOURCE" "$PLIST_DEST"

  set_plist_string "ProgramArguments.0" "$BIN_DIR/hexroute-userd"
  set_plist_string "ProgramArguments.3" "$CONFIG_DIR/user-observe.json"
  set_plist_string "ProgramArguments.5" "$STATE_DIR/pritunl-planner.json"
  set_plist_string "WorkingDirectory" "$STATE_DIR"
  set_plist_string "StandardOutPath" "$LOG_DIR/hexroute-userd.log"
  set_plist_string "StandardErrorPath" "$LOG_DIR/hexroute-userd.err.log"
  /bin/chmod 0644 "$PLIST_DEST"

  "$BIN_DIR/hexroute-userd" --check --config "$CONFIG_DIR/user-observe.json"
  /bin/launchctl bootout "$DOMAIN/$LABEL" >/dev/null 2>&1 || true
  /bin/launchctl bootstrap "$DOMAIN" "$PLIST_DEST"
  /bin/launchctl kickstart -k "$DOMAIN/$LABEL"
  printf 'installed %s in observe-only mode\n' "$LABEL"
}

uninstall_observer() {
  require_user
  /bin/launchctl bootout "$DOMAIN/$LABEL" >/dev/null 2>&1 || true
  /bin/rm -f "$PLIST_DEST"
  /bin/rm -rf "$ROOT_DIR" "$LOG_DIR"
  printf 'removed observe-only %s\n' "$LABEL"
}

status_observer() {
  /bin/launchctl print "$DOMAIN/$LABEL"
}

logs_observer() {
  /usr/bin/tail -n 120 \
    "$LOG_DIR/hexroute-userd.log" \
    "$LOG_DIR/hexroute-userd.err.log"
}

case "${1:-}" in
  install)
    shift
    install_observer "$@"
    ;;
  uninstall)
    uninstall_observer
    ;;
  status)
    status_observer
    ;;
  logs)
    logs_observer
    ;;
  *)
    die "usage: $0 {install BINARY PRIVATE_CONFIG|uninstall|status|logs}"
    ;;
esac
