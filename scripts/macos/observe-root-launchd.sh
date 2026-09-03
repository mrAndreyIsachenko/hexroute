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

# await_bootout waits until the job is actually gone.
#
# bootout is asynchronous: it returns while the job is still unloading, and a
# bootstrap that follows immediately gets `Bootstrap failed: 5: Input/output
# error`. The retry below used to absorb that, and did not — three attempts a
# second apart is two seconds, and a daemon that had been running for three
# days with a read model, an operator socket and two journals open takes
# longer to let go.
#
# So this waits for the condition rather than counting attempts. `launchctl
# print` failing is the condition: the job is no longer in the domain.
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
  # Reinstalling a daemon whose configuration is already in place is an
  # ordinary operation — a new binary, or a plist that gained an argument, with
  # the configuration untouched. `install` refuses to copy a file onto itself
  # and exits, and because that happens midway the binary is already replaced
  # while the plist is not: a half-installed daemon whose loaded job is still
  # the old one.
  #
  # So the copy is skipped when the source already is the destination, and the
  # permissions are still asserted, because the reason to copy was never the
  # bytes.
  if [[ "$config" -ef "$CONFIG_DIR/root-observe.json" ]]; then
    printf 'configuration is already in place; keeping it\n'
    /usr/sbin/chown root:wheel "$CONFIG_DIR/root-observe.json"
    /bin/chmod 0600 "$CONFIG_DIR/root-observe.json"
  else
    /usr/bin/install -o root -g wheel -m 0600 \
      "$config" "$CONFIG_DIR/root-observe.json"
  fi
  /usr/bin/install -o root -g wheel -m 0644 "$PLIST_SOURCE" "$PLIST_DEST"
  if [[ -n "$session" ]]; then
    add_qualification "$PLIST_DEST" "$session"
  fi

  "$BIN_DIR/hexrouted" \
    --check \
    --config "$CONFIG_DIR/root-observe.json" \
    --socket "$SOCKET_DIR/hexrouted.sock"
  /bin/launchctl bootout "system/$LABEL" >/dev/null 2>&1 || true
  await_bootout "system/$LABEL" || true
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
