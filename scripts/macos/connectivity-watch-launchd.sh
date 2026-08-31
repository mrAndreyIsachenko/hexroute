#!/usr/bin/env bash
set -euo pipefail

# The watcher runs on a schedule and reads the root-owned store, so it is a
# system daemon rather than a user agent. It has no KeepAlive: a run that ends
# is a run that finished, and restarting it on exit would make its non-zero
# status — which is how it reports a regression — look like a crash loop.

LABEL="com.hexroute.observe.connectivity-watch"
ROOT_DIR="/Library/Application Support/Hexroute/observe-root"
BIN_DIR="$ROOT_DIR/bin"
STATE_DIR="$ROOT_DIR/state"
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

# add_qualification splices the chain and session in, for the same reason the
# observer's own arguments are spliced rather than committed: a session
# identity is what keeps one soak apart from another, and one in a versioned
# plist would be shared by every install.
add_qualification() {
  local plist="$1" session="$2"
  local buddy="/usr/libexec/PlistBuddy"
  [[ -x "$buddy" ]] || die "PlistBuddy is required to watch a qualification"
  "$buddy" -c "Add :ProgramArguments: string --qualification" \
    -c "Add :ProgramArguments: string $STATE_DIR/connectivity-qualification" \
    -c "Add :ProgramArguments: string --session" \
    -c "Add :ProgramArguments: string $session" \
    "$plist" >/dev/null
}

install_watch() {
  require_root
  local binary="${1:-}"
  local session="${2:-}"
  [[ -n "$binary" ]] || die "usage: $0 install BINARY [SESSION_UUID]"
  require_regular_file "$binary"
  require_regular_file "$PLIST_SOURCE"
  if [[ -n "$session" ]]; then
    [[ "$session" =~ ^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$ ]] ||
      die "SESSION_UUID must be a lowercase UUID"
  fi

  /usr/bin/install -d -o root -g wheel -m 0700 "$ROOT_DIR" "$BIN_DIR" "$STATE_DIR" "$LOG_DIR"
  /usr/bin/install -o root -g wheel -m 0755 "$binary" "$BIN_DIR/hexroute-connectivity-watch"
  /usr/bin/install -o root -g wheel -m 0644 "$PLIST_SOURCE" "$PLIST_DEST"
  if [[ -n "$session" ]]; then
    add_qualification "$PLIST_DEST" "$session"
  fi

  /bin/launchctl bootout "system/$LABEL" >/dev/null 2>&1 || true
  /bin/launchctl bootstrap system "$PLIST_DEST"
  printf 'installed %s, every 5 minutes\n' "$LABEL"
}

uninstall_watch() {
  require_root
  /bin/launchctl bootout "system/$LABEL" >/dev/null 2>&1 || true
  rm -f "$PLIST_DEST"
  # The memory of the last look is left behind on purpose. Removing it would
  # make the next install a first look, which reports nothing.
  printf 'removed %s; its memory of the last look is kept at %s\n' \
    "$LABEL" "$STATE_DIR/connectivity-watch.json"
}

case "${1:-}" in
  install)
    shift
    install_watch "$@"
    ;;
  uninstall)
    uninstall_watch
    ;;
  status)
    /bin/launchctl print "system/$LABEL" 2>/dev/null |
      grep -E "state|last exit code|runs =" || die "not loaded"
    ;;
  logs)
    # Usually empty. That is the point: it prints only what moved.
    tail -n 40 "$LOG_DIR/connectivity-watch.log" 2>/dev/null || true
    ;;
  *)
    die "usage: $0 {install BINARY [SESSION_UUID]|uninstall|status|logs}"
    ;;
esac
