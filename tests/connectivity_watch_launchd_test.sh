#!/usr/bin/env bash
set -euo pipefail

# The watcher is a scheduled system daemon, and the two things that would make
# it useless are easy to get wrong: restarting it on exit, which turns its
# regression status into a crash loop, and a session identity committed to the
# repository, which every install would then share.

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
label="com.hexroute.observe.connectivity-watch"
plist="$repo_root/deploy/macos/$label.plist"
installer="$repo_root/scripts/macos/connectivity-watch-launchd.sh"

[[ -f "$plist" ]]
[[ -x "$installer" ]]

if command -v plutil >/dev/null 2>&1; then
  plutil -lint "$plist" >/dev/null
fi

grep -q "<string>$label</string>" "$plist"
grep -q '<key>StartInterval</key>' "$plist"

# A run that ends is a run that finished. Restarting it would make the non-zero
# status it uses to report a regression look like a daemon that cannot start.
if grep -q '<key>KeepAlive</key>' "$plist"; then
  echo "the watcher would be restarted on exit, which makes a regression look like a crash loop" >&2
  exit 1
fi

# It reads; it must never be able to change anything.
if grep -Eqi 'route[[:space:]]+(add|change|delete)|kill(all)?|pkill|launchctl[[:space:]]+(unload|kickstart)' "$plist"; then
  echo "the watcher plist carries mutation authority" >&2
  exit 1
fi

# Disjoint from the runtimes it must not touch.
if grep -Eqi 'com\.twilight|/twilight/|pritunl-otp-watchdog|adguard' "$plist" "$installer"; then
  echo "the watcher overlaps a protected runtime namespace" >&2
  exit 1
fi

# A session identity in the versioned plist would be shared by every install,
# and a chain holding two runs adds up to a number about neither.
if grep -q 'session' "$plist"; then
  echo "a qualification session is baked into the versioned plist" >&2
  exit 1
fi
grep -q 'SESSION_UUID must be a lowercase UUID' "$installer"

# Spliced after the plist is installed, or the arguments would be written to a
# file the next line overwrites.
splice_line="$(grep -n 'add_qualification "$PLIST_DEST"' "$installer" | cut -d: -f1)"
install_line="$(grep -n '"$PLIST_SOURCE" "$PLIST_DEST"' "$installer" | cut -d: -f1)"
[[ "$splice_line" -gt "$install_line" ]]

# Uninstalling keeps the memory of the last look. Removing it would make the
# next install a first look, and a first look reports nothing.
grep -q 'its memory of the last look is kept' "$installer"
if grep -qE 'rm -f .*connectivity-watch\.json|rm -rf .*connectivity-watch\.json' "$installer"; then
  echo "uninstalling deletes the memory of the last look" >&2
  exit 1
fi

printf 'ok: the connectivity watcher is scheduled, read-only and keeps no session in Git\n'
