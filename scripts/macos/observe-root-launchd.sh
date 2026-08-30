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
  local error_file

  error_file="$(/usr/bin/mktemp /private/tmp/hexroute-launchctl-bootstrap.XXXXXX)"

  for attempt in 1 2 3; do
    if /bin/launchctl bootstrap "$domain" "$plist" 2>"$error_file"; then
      /bin/rm -f "$error_file"
      return 0
    fi
    [[ "$attempt" == "3" ]] || /bin/sleep 1
  done
  /bin/cat "$error_file" >&2
  /bin/rm -f "$error_file"
  return 1
}

# add_qualification splices the soak observer's arguments into the installed
# plist.
#
# They are added here rather than kept in the versioned plist because a session
# identity is what keeps one soak apart from another. Committing one would mean
# every install shared it, and a chain holding two runs adds up to a number
# about neither.
add_qualification() {
  local plist="$1"
  local session="$2"
  local chain="$STATE_DIR/connectivity-qualification"
  local buddy="/usr/libexec/PlistBuddy"
  [[ -x "$buddy" ]] || die "PlistBuddy is required to enable qualification"
  "$buddy" -c "Add :ProgramArguments: string --connectivity-qualification" \
    -c "Add :ProgramArguments: string $chain" \
    -c "Add :ProgramArguments: string --connectivity-qualification-session" \
    -c "Add :ProgramArguments: string $session" \
    "$plist" >/dev/null
  /usr/bin/install -d -o root -g wheel -m 0700 "$chain"
}

install_observer() {
  require_root
  local binary="${1:-}"
  local config="${2:-}"
  # Optional. Without it the daemon runs exactly the path it ran before the
  # soak observer existed.
  local session="${3:-}"
  [[ -n "$binary" && -n "$config" ]] || die "usage: $0 install BINARY PRIVATE_CONFIG [SESSION_UUID]"
  if [[ -n "$session" ]]; then
    [[ "$session" =~ ^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$ ]] ||
      die "SESSION_UUID must be a lowercase UUID"
  fi
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
  if [[ -n "$session" ]]; then
    add_qualification "$PLIST_DEST" "$session"
  fi

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
    die "usage: $0 {install BINARY PRIVATE_CONFIG [SESSION_UUID]|uninstall|status|logs}"
    ;;
esac
