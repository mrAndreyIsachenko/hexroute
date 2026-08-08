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

if grep -Eqi 'com\.twilight|/twilight/|pritunl-otp-watchdog|adguard' "$PLIST" "$INSTALLER"; then
  echo "observe-only launchd package overlaps a protected runtime namespace" >&2
  exit 1
fi

if grep -Eqi 'route[[:space:]]+(add|change|delete)|kill(all)?|pkill|launchctl[[:space:]]+unload' "$PLIST"; then
  echo "observe-only plist contains mutation authority" >&2
  exit 1
fi

"$ROOT/bin/hexrouted" --check --config "$CONFIG" >/dev/null
