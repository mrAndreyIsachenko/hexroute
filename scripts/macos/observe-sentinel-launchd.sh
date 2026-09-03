#!/usr/bin/env bash
set -euo pipefail

# The sentinel watches the root daemon from outside it, so it is a resident
# system daemon with KeepAlive: a watcher that stays down after its first exit
# is a watcher that stops watching exactly when something went wrong.
#
# Its own paths are disjoint from the root daemon's. A watcher sharing a
# directory with the thing it watches would lose its own evidence to the same
# failure.

LABEL="com.hexroute.observe.sentinel"
ROOT_DIR="/Library/Application Support/Hexroute/observe-sentinel"
BIN_DIR="$ROOT_DIR/bin"
CONFIG_DIR="$ROOT_DIR/config"
STATE_DIR="$ROOT_DIR/state"
LOG_DIR="/Library/Logs/Hexroute/observe-sentinel"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
PLIST_SOURCE="$REPO_ROOT/deploy/macos/$LABEL.plist"
PLIST_DEST="/Library/LaunchDaemons/$LABEL.plist"

die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

require_root() {
  [[ "$(id -u)" == "0" ]] || die "run as root"
}

require_regular_file() {
  [[ -f "$1" && ! -L "$1" ]] || die "regular non-symlink file required: $1"
}

install_sentinel() {
  require_root
  local binary="${1:-}"
  local config="${2:-}"
  [[ -n "$binary" && -n "$config" ]] ||
    die "usage: $0 install BINARY PRIVATE_CONFIG"
  require_regular_file "$binary"
  require_regular_file "$config"
  require_regular_file "$PLIST_SOURCE"

  # The synthetic example must not become a live configuration. It names a
  # reserved address and an invalid server name, so a sentinel installed with
  # it would probe nothing and report a broken data path forever.
  if [[ "$(cd "$(dirname "$config")" && pwd)/$(basename "$config")" == \
    "$REPO_ROOT/deploy/macos/sentinel-observe.example.json" ]]; then
    die "the committed example is synthetic; supply a private configuration"
  fi

  # A configuration that will not validate is refused before anything is
  # installed, rather than by a daemon that then restarts forever.
  "$binary" --check --config "$config" >/dev/null ||
    die "the configuration did not validate"

  /usr/bin/install -d -o root -g wheel -m 0700 \
    "$ROOT_DIR" "$BIN_DIR" "$CONFIG_DIR" "$STATE_DIR" "$LOG_DIR"
  /usr/bin/install -o root -g wheel -m 0755 "$binary" "$BIN_DIR/hexroute-sentinel"
  # A configuration already at its destination is the ordinary case on
  # reinstall, and `install` refuses to copy a file onto itself. Refusing
  # midway leaves the binary replaced and the plist not.
  if [[ "$config" -ef "$CONFIG_DIR/sentinel-observe.json" ]]; then
    printf 'configuration is already in place; keeping it\n'
    /usr/sbin/chown root:wheel "$CONFIG_DIR/sentinel-observe.json"
    /bin/chmod 0600 "$CONFIG_DIR/sentinel-observe.json"
  else
    /usr/bin/install -o root -g wheel -m 0600 \
      "$config" "$CONFIG_DIR/sentinel-observe.json"
  fi
  /usr/bin/install -o root -g wheel -m 0644 "$PLIST_SOURCE" "$PLIST_DEST"

  /bin/launchctl bootout "system/$LABEL" >/dev/null 2>&1 || true
  /bin/launchctl bootstrap system "$PLIST_DEST"
  printf 'installed %s, observing continuously\n' "$LABEL"
}

uninstall_sentinel() {
  require_root
  /bin/launchctl bootout "system/$LABEL" >/dev/null 2>&1 || true
  rm -f "$PLIST_DEST"
  rm -f "$BIN_DIR/hexroute-sentinel"
  # The private configuration and the logs stay. The configuration is the
  # operator's, and the logs are what the sentinel was installed to produce.
  printf 'removed %s; configuration kept at %s, logs at %s\n' \
    "$LABEL" "$CONFIG_DIR/sentinel-observe.json" "$LOG_DIR"
}

case "${1:-}" in
  install)
    shift
    install_sentinel "$@"
    ;;
  uninstall)
    uninstall_sentinel
    ;;
  status)
    /bin/launchctl print "system/$LABEL" 2>/dev/null |
      grep -E "state|last exit code|runs =" || die "not loaded"
    ;;
  plans)
    # Usually a handful of lines. The plan is written when it changes.
    grep -E "sentinel_recovery_plan|sentinel_recovery_bound|sentinel_planner_unavailable" \
      "$LOG_DIR/sentinel.log" 2>/dev/null | tail -n 20 ||
      printf 'no plan has been recorded\n'
    ;;
  *)
    die "usage: $0 {install BINARY PRIVATE_CONFIG|uninstall|status|plans}"
    ;;
esac
