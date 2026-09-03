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

# await_bootout waits until the job is actually gone.
#
# bootout is asynchronous: it returns while the job is still unloading, and a
# bootstrap that follows immediately gets `Bootstrap failed: 5: Input/output
# error`. It happened on the root daemon after three days of uptime. Waiting
# for the condition rather than a fixed pause is the only version that holds
# for a job that takes its time letting go.
await_bootout() {
  local target="$1"
  local deadline=$((SECONDS + 30))
  while [[ "$SECONDS" -lt "$deadline" ]]; do
    /bin/launchctl print "$target" >/dev/null 2>&1 || return 0
    /bin/sleep 1
  done
  printf 'the previous job did not unload within 30s; bootstrap will likely fail\n' >&2
  return 1
}

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
  await_bootout "system/$LABEL" || true
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
