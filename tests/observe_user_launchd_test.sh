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

# The user runtime recovers Pritunl and does nothing else to the host. Routes,
# process killing and any other client are outside what its cutover granted,
# and staying outside is what keeps that grant the smallest one available.
if grep -Eqi 'route[[:space:]]+(add|change|delete)|kill(all)?|pkill' \
  "$PLIST" "$INSTALLER" "$ROOT/internal/userdaemon/"*.go; then
  echo "user package contains authority beyond Pritunl recovery" >&2
  exit 1
fi

# The installer manages this daemon's own label, which is how it installs and
# removes itself. The daemon manages no label at all: restarting a service is
# root's half of the recovery, asked for by a typed request and performed
# there.
if grep -Eqi 'launchctl' "$ROOT/internal/userdaemon/"*.go; then
  echo "the user runtime manages a launchd service itself" >&2
  exit 1
fi

# It holds a credential, and holds it in one place. The daemon reads no
# Keychain itself: it configures the credential package by name and lets that
# package do the reading, so there is one implementation to audit rather than
# one per caller.
if grep -Eqi 'security[[:space:]]+find-generic-password|find-generic-password' \
  "$PLIST" "$INSTALLER" "$ROOT/internal/userdaemon/"*.go; then
  echo "the user runtime reads the Keychain itself instead of through the credential package" >&2
  exit 1
fi

# And the names of the items are configuration, never source. A daemon with a
# service name compiled into it reads whatever that name happens to hold on
# whichever machine it lands on.
if grep -Eqi '"[a-z0-9-]*(pritunl|hexroute)[a-z0-9-]*-(pin|totp|otp)"' \
  "$ROOT/internal/userdaemon/"*.go; then
  echo "a Keychain item name is compiled into the user runtime" >&2
  exit 1
fi

"$ROOT/bin/hexroute-userd" --check --config "$CONFIG" >/dev/null

# The cutover has a written transaction and a written rollback, and the
# rollback is written before it is needed: one discovered during an incident is
# not a rollback. It records no live identity, because it is public.
CUTOVER="$ROOT/docs/macos/pritunl-recovery-cutover.md"
[[ -s "$CUTOVER" ]] || {
  echo "the Pritunl recovery cutover has no written procedure" >&2
  exit 1
}
for phrase in \
  'launchctl disable' \
  'launchctl enable' \
  'Roll the policy generation back' \
  'neither touches the Keychain'
do
  grep -qF "$phrase" "$CUTOVER" || {
    printf 'the cutover procedure does not record: %s\n' "$phrase" >&2
    exit 1
  }
done
if grep -EqiC0 '\b[0-9a-f]{16,}\b|com\.(pritunl|twilight)\.[a-z.-]+' "$CUTOVER"; then
  echo "the cutover procedure names a live profile or service" >&2
  exit 1
fi
