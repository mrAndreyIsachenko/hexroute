#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PLIST="$ROOT/deploy/macos/com.hexroute.observe.userd.plist"
CONFIG="$ROOT/deploy/macos/user-observe.example.json"
INSTALLER="$ROOT/scripts/macos/observe-user-launchd.sh"

[[ -f "$PLIST" ]]
[[ -f "$CONFIG" ]]
[[ -x "$INSTALLER" ]]

if command -v plutil >/dev/null 2>&1; then
  plutil -lint "$PLIST" >/dev/null
fi

grep -q '<string>com.hexroute.observe.userd</string>' "$PLIST"
grep -q -- '<string>--observe</string>' "$PLIST"
grep -q -- '<string>--state</string>' "$PLIST"
grep -q -- '<string>--socket</string>' "$PLIST"
grep -q '__HEXROUTE_USERD_BINARY__' "$PLIST"
grep -q 'observe-user' "$INSTALLER"
grep -q '/usr/libexec/PlistBuddy' "$INSTALLER"
grep -q 'bootstrap_with_retry "$DOMAIN" "$PLIST_DEST"' "$INSTALLER"
grep -q 'for attempt in 1 2 3' "$INSTALLER"
grep -q 'launchctl bootstrap "$domain" "$plist" 2>"$error_file"' "$INSTALLER"
error_output_line="$(grep -n 'cat "$error_file" >&2' "$INSTALLER" | cut -d: -f1)"
retry_loop_line="$(grep -n 'for attempt in 1 2 3' "$INSTALLER" | cut -d: -f1)"
[[ "$error_output_line" -gt "$retry_loop_line" ]]
source_check_line="$(grep -n '    --config "$config"' "$INSTALLER" | head -n 1 | cut -d: -f1)"
install_binary_line="$(grep -n '"$binary" "$BIN_DIR/hexroute-userd"' "$INSTALLER" | cut -d: -f1)"
[[ "$source_check_line" -lt "$install_binary_line" ]]

if grep -q 'plutil -replace.*ProgramArguments' "$INSTALLER"; then
  echo "plist renderer inserts array elements instead of replacing them" >&2
  exit 1
fi

rendered="$(mktemp "${TMPDIR:-/tmp}/hexroute-userd-plist.XXXXXX")"
trap 'rm -f "$rendered"' EXIT
cp "$PLIST" "$rendered"
/usr/libexec/PlistBuddy -c \
  "Set :ProgramArguments:0 /Users/example user/Hexroute/bin/hexroute-userd" \
  "$rendered"
/usr/libexec/PlistBuddy -c \
  "Set :ProgramArguments:3 /Users/example user/Hexroute/config/user-observe.json" \
  "$rendered"
/usr/libexec/PlistBuddy -c \
  "Set :ProgramArguments:5 /Users/example user/Hexroute/state/pritunl-planner.json" \
  "$rendered"
/usr/libexec/PlistBuddy -c \
  "Set :ProgramArguments:7 /Users/example user/Hexroute/state/userd.sock" \
  "$rendered"
for entry in \
  "WorkingDirectory:/Users/example user/Hexroute/state" \
  "StandardOutPath:/Users/example user/Hexroute/log/userd.log" \
  "StandardErrorPath:/Users/example user/Hexroute/log/userd.err.log"; do
  key="${entry%%:*}"
  value="${entry#*:}"
  /usr/libexec/PlistBuddy -c "Set :$key $value" "$rendered"
done
if grep -q '__HEXROUTE_USERD_' "$rendered"; then
  echo "rendered plist retains a placeholder" >&2
  exit 1
fi
argument_count="$(/usr/libexec/PlistBuddy -c 'Print :ProgramArguments' "$rendered" |
  awk '/^    /{count++} END{print count+0}')"
# Eight for the daemon's own operation, two more to name the root socket it
# publishes what it observed to. The count is pinned so an argument cannot be
# added without someone deciding it belongs.
if [[ "$argument_count" != "10" ]]; then
  echo "rendered plist has $argument_count arguments, expected 10" >&2
  exit 1
fi

if grep -Eqi 'com\.twilight|/twilight/|pritunl-otp-watchdog|adguard' "$PLIST" "$INSTALLER"; then
  echo "observe-only user package overlaps a protected runtime namespace" >&2
  exit 1
fi

if grep -Eqi 'pritunl-client.*(start|stop|restart)|route[[:space:]]+(add|change|delete)|kill(all)?|pkill' \
  "$PLIST" "$INSTALLER" "$ROOT/internal/userdaemon/"*.go; then
  echo "observe-only user package contains mutation authority" >&2
  exit 1
fi

if grep -Eqi 'keychain|totp[_ -]?seed|pin[_ -]?service|otp[_ -]?service' \
  "$PLIST" "$CONFIG" "$INSTALLER" "$ROOT/internal/userdaemon/"*.go; then
  echo "observe-only user runtime contains a credential dependency" >&2
  exit 1
fi

"$ROOT/bin/hexroute-userd" --check --config "$CONFIG" >/dev/null
