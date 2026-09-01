#!/usr/bin/env bash
set -euo pipefail

# The weekly archive review reads the root-owned archive, so it is a system
# daemon rather than a user agent. It has no KeepAlive: a review that ends is a
# review that finished, and there is nothing to restart it for until next week.

LABEL="com.hexroute.observe.archive-review"
ROOT_DIR="/Library/Application Support/Hexroute/observe-root"
BIN_DIR="$ROOT_DIR/bin"
STATE_DIR="$ROOT_DIR/state"
REPORT_DIR="$STATE_DIR/archive-reports"
LOG_DIR="/Library/Logs/Hexroute/observe-root"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
PLIST_SOURCE="$REPO_ROOT/deploy/macos/$LABEL.plist"
WRAPPER_SOURCE="$REPO_ROOT/scripts/macos/archive-review-run.sh"
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

install_review() {
  require_root
  local binary="${1:-}"
  [[ -n "$binary" ]] || die "usage: $0 install BINARY"
  require_regular_file "$binary"
  require_regular_file "$PLIST_SOURCE"
  require_regular_file "$WRAPPER_SOURCE"

  /usr/bin/install -d -o root -g wheel -m 0700 \
    "$ROOT_DIR" "$BIN_DIR" "$STATE_DIR" "$REPORT_DIR" "$LOG_DIR"
  /usr/bin/install -o root -g wheel -m 0755 \
    "$binary" "$BIN_DIR/hexroute-archive-report"
  /usr/bin/install -o root -g wheel -m 0755 \
    "$WRAPPER_SOURCE" "$BIN_DIR/hexroute-archive-review-run.sh"
  /usr/bin/install -o root -g wheel -m 0644 "$PLIST_SOURCE" "$PLIST_DEST"

  /bin/launchctl bootout "system/$LABEL" >/dev/null 2>&1 || true
  /bin/launchctl bootstrap system "$PLIST_DEST"
  printf 'installed %s, every 7 days\n' "$LABEL"
}

uninstall_review() {
  require_root
  /bin/launchctl bootout "system/$LABEL" >/dev/null 2>&1 || true
  rm -f "$PLIST_DEST"
  rm -f "$BIN_DIR/hexroute-archive-report"
  rm -f "$BIN_DIR/hexroute-archive-review-run.sh"
  # The reports and the attempt log stay. They are the record of the reviews
  # that did run, and removing the schedule is not a reason to lose them —
  # the week someone uninstalls this is a week someone is looking at it.
  printf 'removed %s; reports kept at %s, attempts at %s\n' \
    "$LABEL" "$REPORT_DIR" "$STATE_DIR/archive-review-attempts.log"
}

case "${1:-}" in
  install)
    shift
    install_review "$@"
    ;;
  uninstall)
    uninstall_review
    ;;
  status)
    /bin/launchctl print "system/$LABEL" 2>/dev/null |
      grep -E "state|last exit code|runs =" || die "not loaded"
    ;;
  attempts)
    tail -n 20 "$STATE_DIR/archive-review-attempts.log" 2>/dev/null ||
      printf 'no review has been attempted\n'
    ;;
  reports)
    ls -1 "$REPORT_DIR" 2>/dev/null || printf 'no report has been written\n'
    ;;
  *)
    die "usage: $0 {install BINARY|uninstall|status|attempts|reports}"
    ;;
esac
