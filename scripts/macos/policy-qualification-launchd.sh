#!/usr/bin/env bash
set -euo pipefail

LABEL="com.hexroute.policy-qualification"
UID_VALUE="$(id -u)"
DOMAIN="gui/$UID_VALUE"
ROOT_DIR="$HOME/Library/Application Support/Hexroute/policy-qualification"
BIN_DIR="$ROOT_DIR/bin"
STATE_DIR="$ROOT_DIR/state"
LOG_DIR="$HOME/Library/Logs/Hexroute/policy-qualification"
ROOT_SOCKET="/var/run/hexroute-observe/hexrouted.sock"
USER_SOCKET="$HOME/Library/Application Support/Hexroute/observe-user/state/userd.sock"
PLIST_SOURCE="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)/deploy/macos/$LABEL.plist"
PLIST_DEST="$HOME/Library/LaunchAgents/$LABEL.plist"
INSTALLED_BINARY="$BIN_DIR/hexroute-policy-qualification"

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
  local attempt
  local error_file
  error_file="$(/usr/bin/mktemp /private/tmp/hexroute-qualification-bootstrap.XXXXXX)"
  for attempt in 1 2 3; do
    if /bin/launchctl bootstrap "$DOMAIN" "$PLIST_DEST" 2>"$error_file"; then
      /bin/rm -f "$error_file"
      return 0
    fi
    [[ "$attempt" == "3" ]] || /bin/sleep 1
  done
  /bin/cat "$error_file" >&2
  /bin/rm -f "$error_file"
  return 1
}

set_plist_string() {
  local key="${1//./:}"
  /usr/libexec/PlistBuddy -c "Set :$key $2" "$PLIST_DEST"
}

agent_args() {
  printf '%s\0' \
    --root "$ROOT_DIR" \
    --root-socket "$ROOT_SOCKET" \
    --user-socket "$USER_SOCKET" \
    --interval 60s \
    --max-gap 180s
}

run_agent() {
  local command="$1"
  shift
  local args=()
  while IFS= read -r -d '' value; do
    args+=("$value")
  done < <(agent_args)
  "$INSTALLED_BINARY" "$command" "${args[@]}" "$@"
}

install_agent() {
  require_user
  local binary="${1:-}"
  [[ -n "$binary" ]] || die "usage: $0 install BINARY"
  require_regular_file "$binary"
  require_regular_file "$PLIST_SOURCE"
  "$binary" --check

  /usr/bin/install -d -m 0700 "$ROOT_DIR" "$BIN_DIR" "$STATE_DIR" "$LOG_DIR"
  if [[ ! -d "$HOME/Library/LaunchAgents" ]]; then
    /usr/bin/install -d -m 0755 "$HOME/Library/LaunchAgents"
  fi
  /usr/bin/install -m 0755 "$binary" "$INSTALLED_BINARY"
  /usr/bin/install -m 0644 "$PLIST_SOURCE" "$PLIST_DEST"

  set_plist_string "ProgramArguments.0" "$INSTALLED_BINARY"
  set_plist_string "ProgramArguments.3" "$ROOT_DIR"
  set_plist_string "ProgramArguments.7" "$USER_SOCKET"
  set_plist_string "WorkingDirectory" "$STATE_DIR"
  set_plist_string "StandardOutPath" "$LOG_DIR/qualification.log"
  set_plist_string "StandardErrorPath" "$LOG_DIR/qualification.err.log"
  /bin/chmod 0644 "$PLIST_DEST"

  run_agent start
  /bin/launchctl bootout "$DOMAIN/$LABEL" >/dev/null 2>&1 || true
  await_bootout "$DOMAIN/$LABEL" || true
  bootstrap_with_retry
  /bin/launchctl kickstart -k "$DOMAIN/$LABEL"
  printf 'installed %s in observe-only mode\n' "$LABEL"
}

uninstall_agent() {
  require_user
  /bin/launchctl bootout "$DOMAIN/$LABEL" >/dev/null 2>&1 || true
  /bin/rm -f "$PLIST_DEST" "$INSTALLED_BINARY"
  printf 'removed %s; private qualification evidence was preserved\n' "$LABEL"
}

status_agent() {
  /bin/launchctl print "$DOMAIN/$LABEL"
  run_agent status
}

logs_agent() {
  /usr/bin/tail -n 120 "$LOG_DIR/qualification.log" "$LOG_DIR/qualification.err.log"
}

case "${1:-}" in
  install)
    shift
    install_agent "$@"
    ;;
  uninstall)
    uninstall_agent
    ;;
  status)
    status_agent
    ;;
  arm-sleep)
    run_agent arm-sleep
    ;;
  restart-session)
    run_agent restart-session
    /bin/launchctl kickstart -k "$DOMAIN/$LABEL"
    ;;
  logs)
    logs_agent
    ;;
  *)
    die "usage: $0 {install BINARY|uninstall|status|arm-sleep|restart-session|logs}"
    ;;
esac
