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
grep -q '__HEXROUTE_USERD_BINARY__' "$PLIST"
grep -q 'observe-user' "$INSTALLER"

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
