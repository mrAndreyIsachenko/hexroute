#!/usr/bin/env bash
set -euo pipefail

LABEL="com.hexroute.observe.hexrouted"
ROOT_DIR="/Library/Application Support/Hexroute/observe-root"
BIN_DIR="$ROOT_DIR/bin"
CONFIG_DIR="$ROOT_DIR/config"
STATE_DIR="$ROOT_DIR/state"
SOCKET_DIR="/var/run/hexroute-observe"
LOG_DIR="/Library/Logs/Hexroute/observe-root"
PLIST_SOURCE="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)/deploy/macos/$LABEL.plist"
PLIST_DEST="/Library/LaunchDaemons/$LABEL.plist"

die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

require_root() {
  [[ "$(id -u)" == "0" ]] || die "run as root"
}

require_regular_file() {
  [[ -f "$1" && ! -L "$1" ]] || die "regular non-symlink file required"
}

bootstrap_with_retry() {
  local domain="$1"
  local plist="$2"
  local attempt

  for attempt in 1 2 3; do
    if /bin/launchctl bootstrap "$domain" "$plist"; then
      return 0
    fi
    [[ "$attempt" == "3" ]] || /bin/sleep 1
  done
  return 1
}

install_observer() {
  require_root
  local binary="${1:-}"
  local config="${2:-}"
  [[ -n "$binary" && -n "$config" ]] || die "usage: $0 install BINARY PRIVATE_CONFIG"
  require_regular_file "$binary"
  require_regular_file "$config"
  require_regular_file "$PLIST_SOURCE"

  "$binary" \
    --check \
    --config "$config" \
    --socket "$SOCKET_DIR/hexrouted.sock"

  /usr/bin/install -d -o root -g wheel -m 0700 \
    "$ROOT_DIR" "$BIN_DIR" "$CONFIG_DIR" "$STATE_DIR" "$LOG_DIR"
  /usr/bin/install -d -o root -g wheel -m 0711 "$SOCKET_DIR"
  /usr/bin/install -o root -g wheel -m 0755 "$binary" "$BIN_DIR/hexrouted"
  /usr/bin/install -o root -g wheel -m 0600 "$config" "$CONFIG_DIR/root-observe.json"
  /usr/bin/install -o root -g wheel -m 0644 "$PLIST_SOURCE" "$PLIST_DEST"

  "$BIN_DIR/hexrouted" \
    --check \
    --config "$CONFIG_DIR/root-observe.json" \
    --socket "$SOCKET_DIR/hexrouted.sock"
  /bin/launchctl bootout "system/$LABEL" >/dev/null 2>&1 || true
  bootstrap_with_retry system "$PLIST_DEST"
  /bin/launchctl kickstart -k "system/$LABEL"
  printf 'installed %s in observe-only mode\n' "$LABEL"
}

uninstall_observer() {
  require_root
  /bin/launchctl bootout "system/$LABEL" >/dev/null 2>&1 || true
  /bin/rm -f "$PLIST_DEST"
  /bin/rm -rf "$ROOT_DIR" "$SOCKET_DIR" "$LOG_DIR"
  printf 'removed observe-only %s\n' "$LABEL"
}

status_observer() {
  /bin/launchctl print "system/$LABEL"
}

logs_observer() {
  /usr/bin/tail -n 120 "$LOG_DIR/hexrouted.log" "$LOG_DIR/hexrouted.err.log"
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
