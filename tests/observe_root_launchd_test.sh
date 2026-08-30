#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PLIST="$ROOT/deploy/macos/com.hexroute.observe.hexrouted.plist"
CONFIG="$ROOT/deploy/macos/root-observe.example.json"
INSTALLER="$ROOT/scripts/macos/observe-root-launchd.sh"

[[ -f "$PLIST" ]]
[[ -f "$CONFIG" ]]
[[ -x "$INSTALLER" ]]

if command -v plutil >/dev/null 2>&1; then
  plutil -lint "$PLIST" >/dev/null
fi

grep -q '<string>com.hexroute.observe.hexrouted</string>' "$PLIST"
grep -q '/Library/Application Support/Hexroute/observe-root' "$PLIST"
grep -q '/var/run/hexroute-observe/hexrouted.sock' "$PLIST"
grep -q '/Library/Logs/Hexroute/observe-root' "$PLIST"
grep -q '<string>--observe</string>' "$PLIST"
grep -q '<string>--heartbeat</string>' "$PLIST"
grep -q 'control-loop.heartbeat.json' "$PLIST"
grep -q '<string>--socket</string>' "$PLIST"
grep -q 'bootstrap_with_retry system "$PLIST_DEST"' "$INSTALLER"
grep -q 'for attempt in 1 2 3' "$INSTALLER"
grep -q 'launchctl bootstrap "$domain" "$plist" 2>"$error_file"' "$INSTALLER"
error_output_line="$(grep -n 'cat "$error_file" >&2' "$INSTALLER" | cut -d: -f1)"
retry_loop_line="$(grep -n 'for attempt in 1 2 3' "$INSTALLER" | cut -d: -f1)"
[[ "$error_output_line" -gt "$retry_loop_line" ]]
source_check_line="$(grep -n '    --config "$config"' "$INSTALLER" | head -n 1 | cut -d: -f1)"
install_binary_line="$(grep -n '"$binary" "$BIN_DIR/hexrouted"' "$INSTALLER" | cut -d: -f1)"
[[ "$source_check_line" -lt "$install_binary_line" ]]

if grep -Eqi 'com\.twilight|/twilight/|pritunl-otp-watchdog|adguard' "$PLIST" "$INSTALLER"; then
  echo "observe-only launchd package overlaps a protected runtime namespace" >&2
  exit 1
fi

if grep -Eqi 'route[[:space:]]+(add|change|delete)|kill(all)?|pkill|launchctl[[:space:]]+unload' "$PLIST"; then
  echo "observe-only plist contains mutation authority" >&2
  exit 1
fi

"$ROOT/bin/hexrouted" --check --config "$CONFIG" >/dev/null

# The soak observer is optional and its session is never committed: a session
# identity in a versioned plist would be shared by every install, and a chain
# holding two runs adds up to a number about neither.
if grep -q 'connectivity-qualification' "$PLIST"; then
  echo "a qualification session is baked into the versioned plist" >&2
  exit 1
fi
grep -q 'connectivity-qualification-session' "$INSTALLER"
grep -q 'SESSION_UUID must be a lowercase UUID' "$INSTALLER"
# Spliced after the plist is installed, or the arguments would be written to a
# file the next line overwrites.
splice_line="$(grep -n 'add_qualification "$PLIST_DEST"' "$INSTALLER" | cut -d: -f1)"
plist_install_line="$(grep -n '"$PLIST_SOURCE" "$PLIST_DEST"' "$INSTALLER" | cut -d: -f1)"
[[ "$splice_line" -gt "$plist_install_line" ]]
